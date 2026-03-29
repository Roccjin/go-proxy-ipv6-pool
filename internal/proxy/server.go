package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"ipv6-proxy/internal/auth"
	"ipv6-proxy/internal/ippool"
	"ipv6-proxy/internal/ratelimit"
	"ipv6-proxy/internal/trafficlog"
)

var machineIDRe = regexp.MustCompile(`[0-9a-fA-F]{64}`)

type Server struct {
	pool       *ippool.Pool
	auth       *auth.Manager
	ipBan      *auth.IPBan
	limiter    *ratelimit.Limiter
	tlog       *trafficlog.Logger
	listenAddr string
	stats      Stats
	httpServer *http.Server
}

type Stats struct {
	ActiveConns      atomic.Int64
	TotalBytes       atomic.Int64
	FailedRequests   atomic.Int64
	IPv6Direct       atomic.Int64
	IPv4Fallback     atomic.Int64
}

func NewServer(listenAddr string, pool *ippool.Pool, authMgr *auth.Manager, ipBan *auth.IPBan, limiter *ratelimit.Limiter, tlog *trafficlog.Logger) *Server {
	return &Server{
		pool:       pool,
		auth:       authMgr,
		ipBan:      ipBan,
		limiter:    limiter,
		tlog:       tlog,
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
	if s.ipBan.IsBanned(clientIP) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	user, pass, ok := auth.ParseProxyAuth(r)
	if !ok {
		// No auth header — standard 407 challenge, NOT a failure.
		// HTTP clients send unauthenticated first, then retry with creds.
		w.Header().Set("Proxy-Authenticate", `Basic realm="IPv6 Proxy"`)
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		return
	}

	// Extract machine_id from username: "proxy-mid_<64hex>" → realUser="proxy", mid="<64hex>"
	realUser, mid := parseMachineUser(user)

	if !s.auth.Validate(realUser, pass) {
		s.ipBan.RecordFailure(clientIP)
		w.Header().Set("Proxy-Authenticate", `Basic realm="IPv6 Proxy"`)
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		s.stats.FailedRequests.Add(1)
		return
	}

	// Rate limit per user
	if s.limiter != nil && !s.limiter.Allow(realUser) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		s.stats.FailedRequests.Add(1)
		return
	}

	fingerprint := s.extractFingerprint(r, realUser, mid)
	domain := extractHost(r.Host)

	// Resolve sticky IP (latency=0 for now, will record after request)
	exitIP := s.pool.Resolve(fingerprint, domain, 0)

	start := time.Now()
	var reqErr error
	var dialRes DialResult

	if r.Method == http.MethodConnect {
		dialRes, reqErr = s.handleConnect(w, r, exitIP)
	} else {
		dialRes, reqErr = s.handleHTTP(w, r, exitIP)
	}

	latency := time.Since(start)

	// Record latency stats only — don't trigger domain rule selection again
	s.pool.RecordLatency(fingerprint, domain, latency)

	if reqErr != nil {
		s.stats.FailedRequests.Add(1)
	}

	// Track IPv6 vs IPv4 fallback
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

	// Traffic log
	if s.tlog != nil {
		s.tlog.Record(trafficlog.Entry{
			User:      realUser,
			ClientIP:  clientIP,
			Domain:    domain,
			ExitIP:    exitIP.String(),
			ActualIP:  actualIP,
			ExitType:  exitType,
			Protocol:  "http",
			LatencyMs: latency.Milliseconds(),
			Success:   reqErr == nil,
		})
	}
}

func (s *Server) extractFingerprint(r *http.Request, user string, mid string) string {
	if fp := r.Header.Get("X-Fingerprint"); fp != "" {
		return fp
	}
	// machine_id from username takes highest priority
	if mid != "" {
		return "mid:" + mid
	}
	// fallback: check UA headers
	if midUA := extractMachineID(r); midUA != "" {
		return "mid:" + midUA
	}
	if key := r.Header.Get("X-Sticky-Key"); key != "" {
		return "sticky:" + key
	}
	return user + ":" + extractHost(r.Host)
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, exitIP net.IP) (DialResult, error) {
	dr, err := dialTarget(r.Context(), r.Host, exitIP)
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
			dr, err := dialTarget(ctx, addr, exitIP)
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
// If the target is IPv4-only, we fall back to the system default (no bind).
func dialTarget(ctx context.Context, addr string, exitIP net.IP) (DialResult, error) {
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
		log.Printf("IPv6 dial failed for %s (%s), trying IPv4 fallback: %v", host, target, err)
	}

	// Fallback to IPv4 (no source bind — use system default)
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

func (s *Server) GetStats() map[string]int64 {
	return map[string]int64{
		"active_conns":    s.stats.ActiveConns.Load(),
		"total_bytes":     s.stats.TotalBytes.Load(),
		"failed_requests": s.stats.FailedRequests.Load(),
		"ipv6_direct":     s.stats.IPv6Direct.Load(),
		"ipv4_fallback":   s.stats.IPv4Fallback.Load(),
	}
}

// parseMachineUser splits "user-mid_<64hex>" into (user, mid).
// Plain username like "proxy" returns ("proxy", "").
func parseMachineUser(user string) (string, string) {
	const sep = "-mid_"
	idx := strings.Index(user, sep)
	if idx < 0 {
		return user, ""
	}
	mid := user[idx+len(sep):]
	if len(mid) == 64 && machineIDRe.MatchString(mid) {
		return user[:idx], strings.ToLower(mid)
	}
	return user, ""
}

func extractMachineID(r *http.Request) string {
	for _, h := range []string{"User-Agent", "X-Amz-User-Agent"} {
		if v := r.Header.Get(h); v != "" {
			if mid := machineIDRe.FindString(v); mid != "" {
				return strings.ToLower(mid)
			}
		}
	}
	return ""
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
