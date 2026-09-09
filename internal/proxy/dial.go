package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

const (
	ipv6WarmDialTimeout      = 15 * time.Second
	ipv6ColdDialTimeout      = 3 * time.Second
	ipv6WarmupRetryTimeout   = 5 * time.Second
	ipv6WarmupConfirmTimeout = 2 * time.Second
	ipv6DialKeepAlive        = 30 * time.Second
	ndpWarmTTL               = 60 * time.Second
	ndpWarmCacheMax          = 4096
	ipv6NDPProbeTarget       = "[2001:4860:4860::8888]:53"
)

// DialResult holds the outcome of a dialTarget call.
type DialResult struct {
	Conn    net.Conn
	IsIPv6  bool   // true if connected via IPv6 with pool exit IP
	LocalIP string // actual local address used
}

type ipv6DialFunc func(ctx context.Context, target string, exitIP net.IP, timeout time.Duration) (net.Conn, error)
type neighborPrimeFunc func(exitIP, destIP net.IP)
type connDeadFunc func(conn net.Conn) bool
type warmupConfirmFunc func(conn net.Conn) error

// ipv6Opener dials tcp6 through a FREEBIND exit IP. A brand-new random address
// is confirmed internally (probe TCP + data) before the client-facing dial.
type ipv6Opener struct {
	cache       *ndpWarmCache
	prime       neighborPrimeFunc
	alreadyDead connDeadFunc
	dial        ipv6DialFunc
	confirm     warmupConfirmFunc
	probeTarget string
	coldTimeout time.Duration
	warmTimeout time.Duration
	warmupRetry time.Duration
}

func newProductionIPv6Opener() *ipv6Opener {
	return &ipv6Opener{
		cache:       newNDPWarmCache(ndpWarmTTL, ndpWarmCacheMax),
		prime:       primeNeighbor,
		alreadyDead: connAlreadyDead,
		dial:        ipv6Dial,
		confirm:     confirmDNSTCP,
		probeTarget: ipv6NDPProbeTarget,
		coldTimeout: ipv6ColdDialTimeout,
		warmTimeout: ipv6WarmDialTimeout,
		warmupRetry: ipv6WarmupRetryTimeout,
	}
}

var productionIPv6Opener = newProductionIPv6Opener()

func ipv6Dial(ctx context.Context, target string, exitIP net.IP, timeout time.Duration) (net.Conn, error) {
	dialer := &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: exitIP},
		Timeout:   timeout,
		KeepAlive: ipv6DialKeepAlive,
		Control:   controlFunc,
	}
	return dialer.DialContext(ctx, "tcp6", target)
}

func (opener *ipv6Opener) open(ctx context.Context, target string, exitIP net.IP) (net.Conn, error) {
	if err := opener.ensureWarm(ctx, target, exitIP); err != nil && ctx.Err() != nil {
		return nil, err
	}
	return opener.dialClient(ctx, target, exitIP)
}

func (opener *ipv6Opener) ensureWarm(ctx context.Context, target string, exitIP net.IP) error {
	if opener.cache.Seen(exitIP) {
		return nil
	}
	alreadyWarm, wait, leader := opener.cache.beginConfirm(exitIP)
	if alreadyWarm {
		return nil
	}
	if !leader {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
			return nil
		}
	}
	if opener.prime != nil {
		opener.prime(exitIP, ipFromDialAddr(target))
	}
	err := opener.warmupExit(ctx, target, exitIP)
	opener.cache.finishConfirm(exitIP, err == nil)
	return err
}

func (opener *ipv6Opener) warmupExit(ctx context.Context, target string, exitIP net.IP) error {
	start := time.Now()
	probe := opener.probeAddr()
	if err := opener.confirmDial(ctx, probe, exitIP, true); err == nil {
		log.Printf("IPv6 NDP warmup ok via probe %s source %s in %s", probe, exitIP, time.Since(start).Round(time.Millisecond))
		return nil
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	if probe != target {
		if err := opener.confirmDial(ctx, target, exitIP, false); err == nil {
			log.Printf("IPv6 NDP warmup ok via target %s source %s in %s", target, exitIP, time.Since(start).Round(time.Millisecond))
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	log.Printf("IPv6 NDP warmup failed for %s via %s after %s", target, exitIP, time.Since(start).Round(time.Millisecond))
	return fmt.Errorf("ndp warmup failed for %s", exitIP)
}

func (opener *ipv6Opener) confirmDial(ctx context.Context, target string, exitIP net.IP, withData bool) error {
	conn, err := opener.dialWithColdRetry(ctx, target, exitIP)
	if err != nil {
		return err
	}
	defer closeWakeup(conn)
	if opener.alreadyDead != nil && opener.alreadyDead(conn) {
		return fmt.Errorf("wakeup connection already dead")
	}
	if withData {
		confirm := opener.confirm
		if confirm == nil {
			confirm = confirmDNSTCP
		}
		if err := confirm(conn); err != nil {
			return err
		}
	}
	return nil
}

func (opener *ipv6Opener) dialWithColdRetry(ctx context.Context, target string, exitIP net.IP) (net.Conn, error) {
	timeout := opener.coldTimeout
	if timeout <= 0 {
		timeout = ipv6ColdDialTimeout
	}
	conn, err := opener.dial(ctx, target, exitIP, timeout)
	if err != nil && ctx.Err() == nil && isRetryableDialError(err) {
		log.Printf("IPv6 dial retry for %s via %s after: %v", target, exitIP, err)
		retry := opener.warmupRetry
		if retry <= 0 {
			retry = ipv6WarmupRetryTimeout
		}
		conn, err = opener.dial(ctx, target, exitIP, retry)
	}
	if err == nil && opener.alreadyDead != nil && opener.alreadyDead(conn) {
		log.Printf("IPv6 connection died immediately via %s, retrying %s", exitIP, target)
		closeWakeup(conn)
		retry := opener.warmTimeout
		if retry <= 0 {
			retry = ipv6WarmDialTimeout
		}
		conn, err = opener.dial(ctx, target, exitIP, retry)
	}
	return conn, err
}

func (opener *ipv6Opener) dialClient(ctx context.Context, target string, exitIP net.IP) (net.Conn, error) {
	cold := !opener.cache.Seen(exitIP)
	if cold {
		return opener.dialWithColdRetry(ctx, target, exitIP)
	}
	timeout := opener.warmTimeout
	if timeout <= 0 {
		timeout = ipv6WarmDialTimeout
	}
	return opener.dial(ctx, target, exitIP, timeout)
}

func (opener *ipv6Opener) probeAddr() string {
	if opener.probeTarget != "" {
		return opener.probeTarget
	}
	return ipv6NDPProbeTarget
}

func closeWakeup(conn net.Conn) {
	if conn == nil {
		return
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetLinger(0)
	}
	_ = conn.Close()
}

// confirmDNSTCP exchanges a DNS query over an already-open TCP connection
// so NDP is proven for payload, not just the SYN handshake.
func confirmDNSTCP(conn net.Conn) error {
	if err := conn.SetDeadline(time.Now().Add(ipv6WarmupConfirmTimeout)); err != nil {
		return err
	}
	defer conn.SetDeadline(time.Time{})
	if _, err := conn.Write(dnsTCPRootNSQuery); err != nil {
		return err
	}
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	length := int(header[0])<<8 | int(header[1])
	if length <= 0 || length > 4096 {
		return fmt.Errorf("invalid dns tcp length %d", length)
	}
	body := make([]byte, length)
	_, err := io.ReadFull(conn, body)
	return err
}

// dialTarget resolves the target address and picks the right source IP.
// If the target resolves to IPv6, we bind our pool IPv6 as source.
// IPv4 fallback uses the host default address (no bind) and is off unless allowIPv4.
func dialTarget(ctx context.Context, addr string, exitIP net.IP, allowIPv4 bool) (DialResult, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = "80"
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return DialResult{}, err
	}

	var ipv6Addrs, ipv4Addrs []net.IPAddr
	for _, ip := range ips {
		if ip.IP.To4() == nil {
			ipv6Addrs = append(ipv6Addrs, ip)
		} else {
			ipv4Addrs = append(ipv4Addrs, ip)
		}
	}

	if len(ipv6Addrs) > 0 {
		target := net.JoinHostPort(ipv6Addrs[0].IP.String(), port)
		conn, err := productionIPv6Opener.open(ctx, target, exitIP)
		if err == nil {
			localIP := ""
			if localAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
				localIP = localAddr.IP.String()
			}
			return DialResult{Conn: conn, IsIPv6: true, LocalIP: localIP}, nil
		}
		log.Printf("IPv6 dial failed for %s (%s): %v", host, target, err)
		if !allowIPv4 {
			return DialResult{}, ipv4DisabledErr(host, err)
		}
	}

	if !allowIPv4 {
		return DialResult{}, ipv4DisabledErr(host, nil)
	}

	if len(ipv4Addrs) > 0 {
		target := net.JoinHostPort(ipv4Addrs[0].IP.String(), port)
		dialer := &net.Dialer{
			Timeout:   ipv6WarmDialTimeout,
			KeepAlive: ipv6DialKeepAlive,
		}
		conn, err := dialer.DialContext(ctx, "tcp4", target)
		if err != nil {
			return DialResult{}, err
		}
		localIP := ""
		if localAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
			localIP = localAddr.IP.String()
		}
		return DialResult{Conn: conn, IsIPv6: false, LocalIP: localIP}, nil
	}

	return DialResult{}, &net.OpError{Op: "dial", Net: "tcp", Err: &net.AddrError{Err: "no reachable addresses", Addr: host}}
}

func ipv4DisabledErr(host string, ipv6Err error) error {
	if ipv6Err != nil {
		return fmt.Errorf("ipv6 failed for %s and ipv4 fallback disabled: %w", host, ipv6Err)
	}
	return fmt.Errorf("no IPv6 address for %s (ipv4 fallback disabled)", host)
}

func ipFromDialAddr(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return net.ParseIP(addr)
	}
	return net.ParseIP(host)
}

// dnsTCPRootNSQuery is a TCP-framed DNS query for `. IN NS`.
var dnsTCPRootNSQuery = []byte{
	0x00, 0x11,
	0x12, 0x34,
	0x01, 0x00,
	0x00, 0x01,
	0x00, 0x00,
	0x00, 0x00,
	0x00, 0x00,
	0x00,
	0x00, 0x02,
	0x00, 0x01,
}
