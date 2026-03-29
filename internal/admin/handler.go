package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"ipv6-proxy/internal/auth"
	"ipv6-proxy/internal/ippool"
	"ipv6-proxy/internal/proxy"
	"ipv6-proxy/internal/ratelimit"
	"ipv6-proxy/internal/trafficlog"
)

type Handler struct {
	pool           *ippool.Pool
	authMgr        *auth.Manager
	ipBan          *auth.IPBan
	limiter        *ratelimit.Limiter
	tlog           *trafficlog.Logger
	portMgr        *proxy.PortManager
	adminUser      string
	adminPass      string
	getStats       func() map[string]int64
	getSocks5Stats func() map[string]int64
	startTime      time.Time
	staticFS       fs.FS
	rulesFile      string
	// session tokens
	mu       sync.RWMutex
	sessions map[string]time.Time // token -> expiry
}

const sessionTTL = 24 * time.Hour
const cookieName = "admin_session"

func New(pool *ippool.Pool, authMgr *auth.Manager, ipBan *auth.IPBan, limiter *ratelimit.Limiter, tlog *trafficlog.Logger, portMgr *proxy.PortManager, adminUser, adminPass string, statsFn func() map[string]int64, socks5StatsFn func() map[string]int64, staticFS fs.FS, rulesFile string) *Handler {
	return &Handler{
		pool:           pool,
		authMgr:        authMgr,
		ipBan:          ipBan,
		limiter:        limiter,
		tlog:           tlog,
		portMgr:        portMgr,
		adminUser:      adminUser,
		adminPass:      adminPass,
		getStats:       statsFn,
		getSocks5Stats: socks5StatsFn,
		startTime:      time.Now(),
		staticFS:       staticFS,
		rulesFile:      rulesFile,
		sessions:       make(map[string]time.Time),
	}
}

func (h *Handler) Start(addr string) error {
	mux := http.NewServeMux()

	// Public: login endpoint + SPA static files
	mux.HandleFunc("/api/login", h.apiLogin)

	// Protected API routes
	mux.HandleFunc("/api/logout", h.requireAuth(h.apiLogout))
	mux.HandleFunc("/api/overview", h.requireAuth(h.apiOverview))
	mux.HandleFunc("/api/sessions", h.requireAuth(h.apiSessions))
	mux.HandleFunc("/api/sessions/clear", h.requireAuth(h.apiClearSessions))
	mux.HandleFunc("/api/sessions/remove", h.requireAuth(h.apiRemoveSession))
	mux.HandleFunc("/api/users", h.requireAuth(h.apiUsers))
	mux.HandleFunc("/api/users/add", h.requireAuth(h.apiAddUser))
	mux.HandleFunc("/api/users/remove", h.requireAuth(h.apiRemoveUser))
	mux.HandleFunc("/api/users/rate-limit", h.requireAuth(h.apiSetUserRateLimit))
	mux.HandleFunc("/api/banned", h.requireAuth(h.apiBanned))
	mux.HandleFunc("/api/banned/unban", h.requireAuth(h.apiUnban))
	mux.HandleFunc("/api/banned/ban", h.requireAuth(h.apiBan))
	mux.HandleFunc("/api/whitelist", h.requireAuth(h.apiWhitelist))
	mux.HandleFunc("/api/whitelist/add", h.requireAuth(h.apiWhitelistAdd))
	mux.HandleFunc("/api/whitelist/remove", h.requireAuth(h.apiWhitelistRemove))
	mux.HandleFunc("/api/domain-rules", h.requireAuth(h.apiDomainRules))
	mux.HandleFunc("/api/domain-rules/add", h.requireAuth(h.apiAddDomainRule))
	mux.HandleFunc("/api/domain-rules/remove", h.requireAuth(h.apiRemoveDomainRule))
	mux.HandleFunc("/api/pool/expand", h.requireAuth(h.apiExpandPool))
	mux.HandleFunc("/api/prefix/toggle", h.requireAuth(h.apiPrefixToggle))
	mux.HandleFunc("/api/prefix/test", h.requireAuth(h.apiPrefixTest))
	mux.HandleFunc("/api/traffic-log", h.requireAuth(h.apiTrafficLog))
	mux.HandleFunc("/api/traffic-log/clear", h.requireAuth(h.apiTrafficLogClear))
	mux.HandleFunc("/api/rate-limit", h.requireAuth(h.apiRateLimit))
	mux.HandleFunc("/api/ports", h.requireAuth(h.apiPorts))
	mux.HandleFunc("/api/ports/expand", h.requireAuth(h.apiExpandPorts))
	mux.HandleFunc("/api/ports/stop", h.requireAuth(h.apiStopPort))

	// SPA: serve without auth (login page needs to load)
	if h.staticFS != nil {
		mux.HandleFunc("/", h.serveSPA)
	} else {
		mux.HandleFunc("/", h.requireAuth(h.dashboard))
	}

	log.Printf("Admin panel listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (h *Handler) generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handler) validateSession(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	h.mu.RLock()
	expiry, ok := h.sessions[cookie.Value]
	h.mu.RUnlock()
	if !ok || time.Now().After(expiry) {
		if ok {
			h.mu.Lock()
			delete(h.sessions, cookie.Value)
			h.mu.Unlock()
		}
		return false
	}
	return true
}

func (h *Handler) serveSPA(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	filePath := path[1:]
	f, err := h.staticFS.Open(filePath)
	if err != nil {
		f, err = h.staticFS.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	f.Close()
	http.FileServer(http.FS(h.staticFS)).ServeHTTP(w, r)
}

func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		if h.ipBan.IsBanned(clientIP) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		if !h.validateSession(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next(w, r)
	}
}

func (h *Handler) apiLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	clientIP := r.RemoteAddr
	if h.ipBan.IsBanned(clientIP) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	var req struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.User != h.adminUser || req.Pass != h.adminPass {
		h.ipBan.RecordFailure(clientIP)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid credentials"}`))
		return
	}
	token := h.generateToken()
	expiry := time.Now().Add(sessionTTL)
	h.mu.Lock()
	h.sessions[token] = expiry
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		h.mu.Lock()
		delete(h.sessions, cookie.Value)
		h.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	writeJSON(w, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fmtDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}

type sessionResp struct {
	Fingerprint string       `json:"fingerprint"`
	ExitIP      string       `json:"exit_ip"`
	ExitIPs     []string     `json:"exit_ips"`
	TotalHits   int64        `json:"total_hits"`
	MaxLatMs    int64        `json:"max_lat_ms"`
	MinLatMs    int64        `json:"min_lat_ms"`
	AvgLatMs    int64        `json:"avg_lat_ms"`
	CreatedAt   string       `json:"created_at"`
	LastSeen    string       `json:"last_seen"`
	Domains     []domainResp `json:"domains"`
}

type domainResp struct {
	Domain   string `json:"domain"`
	Hits     int64  `json:"hits"`
	MaxLatMs int64  `json:"max_lat_ms"`
	MinLatMs int64  `json:"min_lat_ms"`
	LastSeen string `json:"last_seen"`
}

func (h *Handler) apiOverview(w http.ResponseWriter, r *http.Request) {
	totalReqs, maxLat, minLat := h.pool.GlobalStats()
	stats := h.getStats()
	s5stats := h.getSocks5Stats()
	writeJSON(w, map[string]interface{}{
		"uptime_seconds":       int(time.Since(h.startTime).Seconds()),
		"uptime_str":           fmtDuration(time.Since(h.startTime)),
		"pool_size":            h.pool.Count(),
		"prefixes":             h.pool.Prefixes(),
		"prefix":               h.pool.Prefix(),
		"prefix_bits":          h.pool.PrefixBits(),
		"max_ipv6":             h.pool.MaxCapacity(),
		"active_sessions":      h.pool.SessionCount(),
		"total_requests":       totalReqs,
		"active_conns":         stats["active_conns"],
		"total_bytes":          stats["total_bytes"],
		"failed_requests":      stats["failed_requests"],
		"ipv6_direct":          stats["ipv6_direct"] + s5stats["ipv6_direct"],
		"ipv4_fallback":        stats["ipv4_fallback"] + s5stats["ipv4_fallback"],
		"max_latency_ms":       maxLat.Milliseconds(),
		"min_latency_ms":       minLat.Milliseconds(),
		"socks5_active_conns":  s5stats["active_conns"],
		"socks5_total_bytes":   s5stats["total_bytes"],
		"socks5_failed_requests": s5stats["failed_requests"],
	})
}

func (h *Handler) apiSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.pool.Sessions()
	out := make([]sessionResp, 0, len(sessions))
	for _, s := range sessions {
		domains := make([]domainResp, 0, len(s.Domains))
		for _, d := range s.Domains {
			domains = append(domains, domainResp{
				Domain:   d.Domain,
				Hits:     d.Hits,
				MaxLatMs: d.MaxLatency.Milliseconds(),
				MinLatMs: d.MinLatency.Milliseconds(),
				LastSeen: d.LastSeen.Format("15:04:05"),
			})
		}
		exitIPs := make([]string, 0, len(s.ExitIPs))
		for ip := range s.ExitIPs {
			exitIPs = append(exitIPs, ip)
		}
		out = append(out, sessionResp{
			Fingerprint: s.Fingerprint,
			ExitIP:      s.ExitIP.String(),
			ExitIPs:     exitIPs,
			TotalHits:   s.TotalHits,
			MaxLatMs:    s.MaxLatency.Milliseconds(),
			MinLatMs:    s.MinLatency.Milliseconds(),
			AvgLatMs:    s.AvgLatency.Milliseconds(),
			CreatedAt:   s.CreatedAt.Format("15:04:05"),
			LastSeen:    s.LastSeen.Format("15:04:05"),
			Domains:     domains,
		})
	}
	writeJSON(w, out)
}

func (h *Handler) apiClearSessions(w http.ResponseWriter, r *http.Request) {
	h.pool.ClearSessions()
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiRemoveSession(w http.ResponseWriter, r *http.Request) {
	var req struct{ Fingerprint string `json:"fingerprint"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.pool.RemoveSession(req.Fingerprint)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiUsers(w http.ResponseWriter, r *http.Request) {
	users := h.authMgr.ListUsers()
	type userInfo struct {
		User      string `json:"user"`
		RateLimit int    `json:"rate_limit"`
	}
	out := make([]userInfo, 0, len(users))
	for _, u := range users {
		rl := 0
		if h.limiter != nil {
			rl = h.limiter.UserLimit(u)
		}
		out = append(out, userInfo{User: u, RateLimit: rl})
	}
	writeJSON(w, map[string]interface{}{"users": out})
}

func (h *Handler) apiAddUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	h.authMgr.AddUser(req.User, req.Pass)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiRemoveUser(w http.ResponseWriter, r *http.Request) {
	var req struct{ User string `json:"user"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.authMgr.RemoveUser(req.User)
	// Also remove per-user rate limit
	if h.limiter != nil {
		h.limiter.SetUserLimit(req.User, 0)
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiSetUserRateLimit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		User  string `json:"user"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if h.limiter == nil {
		writeJSON(w, map[string]string{"status": "error", "message": "rate limiting disabled"})
		return
	}
	h.limiter.SetUserLimit(req.User, req.Limit)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiBanned(w http.ResponseWriter, r *http.Request) {
	banned := h.ipBan.BannedList()
	type entry struct {
		IP       string `json:"ip"`
		BannedAt string `json:"banned_at"`
		Failures int    `json:"failures"`
	}
	entries := make([]entry, 0, len(banned))
	for _, b := range banned {
		entries = append(entries, entry{
			IP:       b.IP,
			BannedAt: b.BannedAt.Format("15:04:05"),
			Failures: b.Failures,
		})
	}
	writeJSON(w, map[string]interface{}{"banned": entries})
}

func (h *Handler) apiUnban(w http.ResponseWriter, r *http.Request) {
	var req struct{ IP string `json:"ip"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.ipBan.Unban(req.IP)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiBan(w http.ResponseWriter, r *http.Request) {
	var req struct{ IP string `json:"ip"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.ipBan.Ban(req.IP)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiWhitelist(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"whitelist": h.ipBan.WhitelistedList()})
}

func (h *Handler) apiWhitelistAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ IP string `json:"ip"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.ipBan.AddWhitelist(req.IP)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiWhitelistRemove(w http.ResponseWriter, r *http.Request) {
	var req struct{ IP string `json:"ip"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.ipBan.RemoveWhitelist(req.IP)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiDomainRules(w http.ResponseWriter, r *http.Request) {
	rules := h.pool.DomainRules()
	writeJSON(w, map[string]interface{}{"rules": rules})
}

func (h *Handler) apiAddDomainRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain   string `json:"domain"`
		Count    int    `json:"count"`
		Strategy int    `json:"strategy"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	h.pool.AddDomainRule(req.Domain, req.Count, ippool.Strategy(req.Strategy))
	if h.rulesFile != "" {
		if err := h.pool.SaveDomainRules(h.rulesFile); err != nil {
			log.Printf("Failed to save domain rules: %v", err)
		}
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiRemoveDomainRule(w http.ResponseWriter, r *http.Request) {
	var req struct{ Domain string `json:"domain"` }
	json.NewDecoder(r.Body).Decode(&req)
	h.pool.RemoveDomainRule(req.Domain)
	if h.rulesFile != "" {
		if err := h.pool.SaveDomainRules(h.rulesFile); err != nil {
			log.Printf("Failed to save domain rules: %v", err)
		}
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiExpandPool(w http.ResponseWriter, r *http.Request) {
	h.pool.Expand(100)
	writeJSON(w, map[string]interface{}{
		"status":   "ok",
		"new_size": fmt.Sprintf("%d", h.pool.Count()),
	})
}

func (h *Handler) apiPrefixToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Prefix  string `json:"prefix"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h.pool.SetPrefixEnabled(req.Prefix, req.Enabled)
	writeJSON(w, map[string]interface{}{
		"status":    "ok",
		"pool_size": h.pool.Count(),
	})
}

func (h *Handler) apiPrefixTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Prefix string `json:"prefix"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	exitIP, latency, err := h.pool.TestPrefix(req.Prefix)
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	// Lookup geolocation for the exit IP
	geo := lookupIPGeo(exitIP)

	writeJSON(w, map[string]interface{}{
		"status":     "ok",
		"exit_ip":    exitIP,
		"latency_ms": latency.Milliseconds(),
		"geo":        geo,
	})
}

func (h *Handler) apiTrafficLog(w http.ResponseWriter, r *http.Request) {
	if h.tlog == nil {
		writeJSON(w, map[string]interface{}{"entries": []struct{}{}, "total": 0})
		return
	}
	entries := h.tlog.Recent(400)
	writeJSON(w, map[string]interface{}{
		"entries": entries,
		"total":   h.tlog.Count(),
	})
}

func (h *Handler) apiTrafficLogClear(w http.ResponseWriter, r *http.Request) {
	if h.tlog != nil {
		h.tlog.Clear()
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiPorts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"ports": h.portMgr.Ports()})
}

func (h *Handler) apiExpandPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	added := h.portMgr.Expand(5)
	writeJSON(w, map[string]interface{}{"status": "ok", "added": added})
}

func (h *Handler) apiStopPort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct{ Addr string `json:"addr"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.portMgr.StopPort(req.Addr); err != nil {
		writeJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiRateLimit(w http.ResponseWriter, r *http.Request) {
	if h.limiter == nil {
		writeJSON(w, map[string]interface{}{"enabled": false})
		return
	}
	writeJSON(w, map[string]interface{}{
		"enabled":    true,
		"limit":      h.limiter.Limit(),
		"window_sec": int(h.limiter.Window().Seconds()),
	})
}

// lookupIPGeo queries ip-api.com for geolocation of an IP address.
// Returns a map with country, city, isp, org etc. On failure returns a minimal map.
func lookupIPGeo(rawIP string) map[string]interface{} {
	// Strip port and brackets from addresses like "[::1]:12345"
	ip := rawIP
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	ip = strings.Trim(ip, "[]")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://ip-api.com/json/" + ip + "?fields=status,country,regionName,city,isp,org,as,query")
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return result
}
