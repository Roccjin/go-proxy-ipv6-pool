package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"ipv6-proxy/internal/auth"
	"ipv6-proxy/internal/trafficlog"
)

type Stats struct {
	ActiveConns      atomic.Int64
	TotalBytes       atomic.Int64
	FailedRequests   atomic.Int64
	IPv6Direct       atomic.Int64
	IPv4Fallback     atomic.Int64
}

type Server struct {
	rt         *Runtime
	listenAddr string
	stats      Stats
	httpServer *http.Server
}

func NewServer(listenAddr string, rt *Runtime) *Server {
	return &Server{
		rt:         rt,
		listenAddr: listenAddr,
	}
}

func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:         s.listenAddr,
		Handler:      s,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	log.Printf("Proxy listening on %s", s.listenAddr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Stop() error {
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

func (s *Server) Addr() string { return s.listenAddr }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.stats.ActiveConns.Add(1)
	defer s.stats.ActiveConns.Add(-1)

	clientIP := r.RemoteAddr
	if s.rt.IPBan.IsBanned(clientIP) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	user, pass, ok := auth.ParseProxyAuth(r)
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="IPv6 Proxy"`)
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		return
	}

	admitted, err := s.rt.Admit(r.Context(), user, pass, extraSIDFromHeaders(r.Header))
	if errors.Is(err, ErrAuth) {
		s.rt.IPBan.RecordFailure(clientIP)
		w.Header().Set("Proxy-Authenticate", `Basic realm="IPv6 Proxy"`)
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		s.stats.FailedRequests.Add(1)
		return
	}
	if errors.Is(err, ErrRate) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		s.stats.FailedRequests.Add(1)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		s.stats.FailedRequests.Add(1)
		return
	}

	exitIP := net.ParseIP(admitted.ExitIP)
	if exitIP == nil {
		http.Error(w, "invalid exit ip", http.StatusBadGateway)
		s.stats.FailedRequests.Add(1)
		return
	}

	domain := extractHost(r.Host)
	start := time.Now()
	var reqErr error
	var dialRes DialResult

	if r.Method == http.MethodConnect {
		dialRes, reqErr = s.handleConnect(w, r, exitIP)
	} else {
		dialRes, reqErr = s.handleHTTP(w, r, exitIP)
	}

	latency := time.Since(start)
	if reqErr != nil {
		s.stats.FailedRequests.Add(1)
	}

	exitType := "ipv6"
	actualIP := exitIP.String()
	if dialRes.Conn != nil || reqErr == nil {
		if dialRes.IsIPv6 {
			s.stats.IPv6Direct.Add(1)
		} else {
			s.stats.IPv4Fallback.Add(1)
			exitType = "ipv4_fallback"
		}
		if dialRes.LocalIP != "" {
			actualIP = dialRes.LocalIP
		}
	}

	if s.rt.TLog != nil {
		s.rt.TLog.Record(trafficlog.Entry{
			User:      admitted.Parsed.Account,
			ClientIP:  clientIP,
			Domain:    domain,
			ExitIP:    exitIP.String(),
			ActualIP:  actualIP,
			ExitType:  exitType,
			Protocol:  "http",
			LatencyMs: latency.Milliseconds(),
			Success:   reqErr == nil,
			SID:       admitted.Parsed.SID,
			Mode:      string(admitted.Parsed.Mode),
			TTLMin:    admitted.Parsed.TTLMinutes,
		})
	}
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, exitIP net.IP) (DialResult, error) {
	dr, err := dialTarget(r.Context(), r.Host, exitIP, s.rt.ipv4Allowed())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return dr, err
	}
	defer dr.Conn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return dr, fmt.Errorf("hijacking not supported")
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		return dr, err
	}
	defer clientConn.Close()

	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(dr.Conn, clientConn)
		s.stats.TotalBytes.Add(n)
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(clientConn, dr.Conn)
		s.stats.TotalBytes.Add(n)
		done <- struct{}{}
	}()
	<-done
	return dr, nil
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request, exitIP net.IP) (DialResult, error) {
	var lastDR DialResult
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dr, err := dialTarget(ctx, addr, exitIP, s.rt.ipv4Allowed())
			lastDR = dr
			return dr.Conn, err
		},
	}

	r.RequestURI = ""
	r.Header.Del("Proxy-Authorization")
	r.Header.Del("X-Fingerprint")
	r.Header.Del("X-Sticky-Key")

	resp, err := transport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return lastDR, err
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	n, _ := io.Copy(w, resp.Body)
	s.stats.TotalBytes.Add(n)
	return lastDR, nil
}

// DialResult holds the outcome of a dialTarget call.
type DialResult struct {
	Conn   net.Conn
	IsIPv6 bool   // true if connected via IPv6 with pool exit IP
	LocalIP string // actual local address used
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

	// Resolve with system default resolver (supports both A and AAAA)
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return DialResult{}, err
	}

	// Prefer IPv6 targets so we can use our pool address
	var ipv6Addrs, ipv4Addrs []net.IPAddr
	for _, ip := range ips {
		if ip.IP.To4() == nil {
			ipv6Addrs = append(ipv6Addrs, ip)
		} else {
			ipv4Addrs = append(ipv4Addrs, ip)
		}
	}

	// Try IPv6 first (with our exit IP bound)
	if len(ipv6Addrs) > 0 {
		target := net.JoinHostPort(ipv6Addrs[0].IP.String(), port)
		dialer := &net.Dialer{
			LocalAddr: &net.TCPAddr{IP: exitIP},
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   controlFunc,
		}
		conn, err := dialer.DialContext(ctx, "tcp6", target)
		if err == nil {
			localIP := ""
			if la, ok := conn.LocalAddr().(*net.TCPAddr); ok {
				localIP = la.IP.String()
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

	// Fallback to IPv4 (no source bind — uses the server's public IPv4)
	if len(ipv4Addrs) > 0 {
		target := net.JoinHostPort(ipv4Addrs[0].IP.String(), port)
		dialer := &net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		conn, err := dialer.DialContext(ctx, "tcp4", target)
		if err != nil {
			return DialResult{}, err
		}
		localIP := ""
		if la, ok := conn.LocalAddr().(*net.TCPAddr); ok {
			localIP = la.IP.String()
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

func (s *Server) GetStats() map[string]int64 {
	return map[string]int64{
		"active_conns":    s.stats.ActiveConns.Load(),
		"total_bytes":     s.stats.TotalBytes.Load(),
		"failed_requests": s.stats.FailedRequests.Load(),
		"ipv6_direct":     s.stats.IPv6Direct.Load(),
		"ipv4_fallback":   s.stats.IPv4Fallback.Load(),
	}
}

func extractHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	if idx := strings.LastIndex(host, ":"); idx > 0 && !strings.Contains(host[idx:], "]") {
		return host[:idx]
	}
	return host
}
