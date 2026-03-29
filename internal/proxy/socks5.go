package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"ipv6-proxy/internal/auth"
	"ipv6-proxy/internal/ippool"
	"ipv6-proxy/internal/ratelimit"
	"ipv6-proxy/internal/trafficlog"
)

// SOCKS5 constants
const (
	socks5Version = 0x05
	authNone      = 0x00
	authUserPass  = 0x02
	authNoAccept  = 0xFF

	cmdConnect      = 0x01
	addrIPv4        = 0x01
	addrDomain      = 0x03
	addrIPv6        = 0x04
	repSuccess      = 0x00
	repGeneralFail  = 0x01
	repNotAllowed   = 0x02
	repCmdNotSupp   = 0x07
	repAddrNotSupp  = 0x08
)

type Socks5Server struct {
	listenAddr string
	pool       *ippool.Pool
	auth       *auth.Manager
	ipBan      *auth.IPBan
	limiter    *ratelimit.Limiter
	tlog       *trafficlog.Logger
	stats      Stats
	listener   net.Listener
}

func NewSocks5Server(listenAddr string, pool *ippool.Pool, authMgr *auth.Manager, ipBan *auth.IPBan, limiter *ratelimit.Limiter, tlog *trafficlog.Logger) *Socks5Server {
	return &Socks5Server{
		listenAddr: listenAddr,
		pool:       pool,
		auth:       authMgr,
		ipBan:      ipBan,
		limiter:    limiter,
		tlog:       tlog,
	}
}

func (s *Socks5Server) Start() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return err
	}
	s.listener = ln
	log.Printf("SOCKS5 listening on %s", s.listenAddr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Check if listener was closed intentionally
			select {
			default:
				log.Printf("SOCKS5 accept: %v", err)
			}
			if s.listener == nil {
				return nil
			}
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Socks5Server) Stop() error {
	if s.listener != nil {
		ln := s.listener
		s.listener = nil
		return ln.Close()
	}
	return nil
}

func (s *Socks5Server) Addr() string { return s.listenAddr }

func (s *Socks5Server) GetStats() map[string]int64 {
	return map[string]int64{
		"active_conns":    s.stats.ActiveConns.Load(),
		"total_bytes":     s.stats.TotalBytes.Load(),
		"failed_requests": s.stats.FailedRequests.Load(),
		"ipv6_direct":     s.stats.IPv6Direct.Load(),
		"ipv4_fallback":   s.stats.IPv4Fallback.Load(),
	}
}

func (s *Socks5Server) handleConn(conn net.Conn) {
	defer conn.Close()
	s.stats.ActiveConns.Add(1)
	defer s.stats.ActiveConns.Add(-1)

	clientIP := conn.RemoteAddr().String()
	if s.ipBan.IsBanned(clientIP) {
		return
	}

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	user, mid, err := s.negotiate(conn, clientIP)
	if err != nil {
		s.stats.FailedRequests.Add(1)
		return
	}

	// Rate limit per user
	if s.limiter != nil && !s.limiter.Allow(user) {
		s.stats.FailedRequests.Add(1)
		return
	}

	conn.SetDeadline(time.Time{}) // clear deadline for relay

	s.handleRequest(conn, user, mid, clientIP)
}

// SOCKS5_NEGOTIATE_PLACEHOLDER

// negotiate handles SOCKS5 version negotiation and authentication.
// Returns (realUser, machineID, error).
func (s *Socks5Server) negotiate(conn net.Conn, clientIP string) (string, string, error) {
	// Read version + number of methods
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", "", err
	}
	if buf[0] != socks5Version {
		return "", "", fmt.Errorf("unsupported SOCKS version: %d", buf[0])
	}
	nMethods := int(buf[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", "", err
	}

	// We require username/password auth
	hasUserPass := false
	for _, m := range methods {
		if m == authUserPass {
			hasUserPass = true
			break
		}
	}
	if !hasUserPass {
		conn.Write([]byte{socks5Version, authNoAccept})
		return "", "", fmt.Errorf("client does not support username/password auth")
	}
	conn.Write([]byte{socks5Version, authUserPass})

	return s.authenticate(conn, clientIP)
}

// authenticate performs RFC 1929 username/password authentication.
func (s *Socks5Server) authenticate(conn net.Conn, clientIP string) (string, string, error) {
	// RFC 1929: version(1) + ulen(1) + user(ulen) + plen(1) + pass(plen)
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", "", err
	}
	// buf[0] is sub-negotiation version (0x01)
	uLen := int(buf[1])
	userBuf := make([]byte, uLen)
	if _, err := io.ReadFull(conn, userBuf); err != nil {
		return "", "", err
	}

	pLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, pLenBuf); err != nil {
		return "", "", err
	}
	pLen := int(pLenBuf[0])
	passBuf := make([]byte, pLen)
	if _, err := io.ReadFull(conn, passBuf); err != nil {
		return "", "", err
	}

	user := string(userBuf)
	pass := string(passBuf)

	realUser, mid := parseMachineUser(user)
	if !s.auth.Validate(realUser, pass) {
		conn.Write([]byte{0x01, 0x01}) // auth failure
		s.ipBan.RecordFailure(clientIP)
		return "", "", fmt.Errorf("auth failed for %s", realUser)
	}
	conn.Write([]byte{0x01, 0x00}) // auth success
	return realUser, mid, nil
}

// SOCKS5_REQUEST_PLACEHOLDER

func (s *Socks5Server) handleRequest(conn net.Conn, user, mid, clientIP string) {
	// Read request: ver(1) + cmd(1) + rsv(1) + atyp(1)
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	if header[0] != socks5Version {
		return
	}

	cmd := header[1]
	if cmd != cmdConnect {
		s.sendReply(conn, repCmdNotSupp, nil, 0)
		s.stats.FailedRequests.Add(1)
		return
	}

	// Parse target address
	targetHost, targetPort, err := s.readAddress(conn, header[3])
	if err != nil {
		s.sendReply(conn, repAddrNotSupp, nil, 0)
		s.stats.FailedRequests.Add(1)
		return
	}

	// Build fingerprint: mid takes priority, fallback to user:host
	var fingerprint string
	if mid != "" {
		fingerprint = "mid:" + mid
	} else {
		fingerprint = user + ":" + targetHost
	}

	domain := targetHost
	exitIP := s.pool.Resolve(fingerprint, domain, 0)

	addr := net.JoinHostPort(targetHost, fmt.Sprintf("%d", targetPort))
	start := time.Now()

	dr, err := dialTarget(context.Background(), addr, exitIP)
	if err != nil {
		s.sendReply(conn, repGeneralFail, nil, 0)
		s.stats.FailedRequests.Add(1)
		s.pool.RecordLatency(fingerprint, domain, time.Since(start))
		if s.tlog != nil {
			s.tlog.Record(trafficlog.Entry{
				User:      user,
				ClientIP:  clientIP,
				Domain:    domain,
				ExitIP:    exitIP.String(),
				ExitType:  "ipv6",
				Protocol:  "socks5",
				LatencyMs: time.Since(start).Milliseconds(),
				Success:   false,
			})
		}
		return
	}
	defer dr.Conn.Close()

	// Track IPv6 vs IPv4 fallback
	exitType := "ipv6"
	actualIP := exitIP.String()
	if dr.IsIPv6 {
		s.stats.IPv6Direct.Add(1)
	} else {
		s.stats.IPv4Fallback.Add(1)
		exitType = "ipv4_fallback"
	}
	if dr.LocalIP != "" {
		actualIP = dr.LocalIP
	}

	// Send success reply with bound address
	localAddr := dr.Conn.LocalAddr().(*net.TCPAddr)
	s.sendReply(conn, repSuccess, localAddr.IP, uint16(localAddr.Port))

	latency := time.Since(start)
	s.pool.RecordLatency(fingerprint, domain, latency)

	if s.tlog != nil {
		s.tlog.Record(trafficlog.Entry{
			User:      user,
			ClientIP:  clientIP,
			Domain:    domain,
			ExitIP:    exitIP.String(),
			ActualIP:  actualIP,
			ExitType:  exitType,
			Protocol:  "socks5",
			LatencyMs: latency.Milliseconds(),
			Success:   true,
		})
	}

	// Relay data
	s.relay(conn, dr.Conn)
}

func (s *Socks5Server) readAddress(conn net.Conn, atyp byte) (string, uint16, error) {
	var host string
	switch atyp {
	case addrIPv4:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", 0, err
		}
		host = net.IP(buf).String()
	case addrDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", 0, err
		}
		domBuf := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domBuf); err != nil {
			return "", 0, err
		}
		host = string(domBuf)
	case addrIPv6:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", 0, err
		}
		host = net.IP(buf).String()
	default:
		return "", 0, fmt.Errorf("unsupported address type: %d", atyp)
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", 0, err
	}
	port := binary.BigEndian.Uint16(portBuf)
	return host, port, nil
}

func (s *Socks5Server) sendReply(conn net.Conn, rep byte, bindIP net.IP, bindPort uint16) {
	// ver(1) + rep(1) + rsv(1) + atyp(1) + addr + port(2)
	reply := []byte{socks5Version, rep, 0x00}
	if bindIP != nil && bindIP.To4() == nil {
		// IPv6
		reply = append(reply, addrIPv6)
		reply = append(reply, bindIP.To16()...)
	} else {
		// IPv4 (or zero)
		reply = append(reply, addrIPv4)
		if bindIP != nil {
			reply = append(reply, bindIP.To4()...)
		} else {
			reply = append(reply, 0, 0, 0, 0)
		}
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, bindPort)
	reply = append(reply, portBytes...)
	conn.Write(reply)
}

func (s *Socks5Server) relay(client, target net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(target, client)
		s.stats.TotalBytes.Add(n)
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(client, target)
		s.stats.TotalBytes.Add(n)
		done <- struct{}{}
	}()
	<-done
}
