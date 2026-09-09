package proxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"
)

const (
	ipv6WarmDialTimeout = 15 * time.Second
	ipv6ColdDialTimeout = 3 * time.Second
	ipv6DialKeepAlive   = 30 * time.Second
	ndpWarmTTL          = 60 * time.Second
	ndpWarmCacheMax     = 4096
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

// ipv6Opener dials tcp6 through a FREEBIND exit IP, priming NDP on first use
// so a brand-new random address does not lose the first SYN-ACK / TLS record.
type ipv6Opener struct {
	cache       *ndpWarmCache
	prime       neighborPrimeFunc
	alreadyDead connDeadFunc
	dial        ipv6DialFunc
	coldTimeout time.Duration
	warmTimeout time.Duration
}

func newProductionIPv6Opener() *ipv6Opener {
	return &ipv6Opener{
		cache:       newNDPWarmCache(ndpWarmTTL, ndpWarmCacheMax),
		prime:       primeNeighbor,
		alreadyDead: connAlreadyDead,
		dial:        ipv6Dial,
		coldTimeout: ipv6ColdDialTimeout,
		warmTimeout: ipv6WarmDialTimeout,
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
	cold := !opener.cache.Seen(exitIP)
	if cold && opener.prime != nil {
		opener.prime(exitIP, ipFromDialAddr(target))
	}

	timeout := opener.warmTimeout
	if cold {
		timeout = opener.coldTimeout
	}
	if timeout <= 0 {
		timeout = ipv6WarmDialTimeout
	}

	conn, err := opener.dial(ctx, target, exitIP, timeout)
	if err != nil && cold && ctx.Err() == nil && isRetryableDialError(err) {
		log.Printf("IPv6 dial retry for %s via %s after: %v", target, exitIP, err)
		conn, err = opener.dial(ctx, target, exitIP, opener.warmTimeout)
	}
	if err == nil && cold && opener.alreadyDead != nil && opener.alreadyDead(conn) {
		log.Printf("IPv6 connection died immediately via %s, retrying %s", exitIP, target)
		_ = conn.Close()
		conn, err = opener.dial(ctx, target, exitIP, opener.warmTimeout)
	}
	if err == nil {
		opener.cache.Mark(exitIP)
	}
	return conn, err
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
