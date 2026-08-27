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
	"ipv6-proxy/internal/store"
	"ipv6-proxy/internal/trafficlog"
)

type Deps struct {
	Pool           *ippool.Pool
	Store          *store.Store
	IPBan          *auth.IPBan
	Limiter        *ratelimit.Limiter
	TLog           *trafficlog.Logger
	PortMgr        *proxy.PortManager
	AdminUser      string
	AdminPass      string
	GetStats       func() map[string]int64
	GetSocks5Stats func() map[string]int64
	StaticFS       fs.FS
	RulesFile      string
	PublicHost     string
	HTTPAddr       string
	SOCKS5Addr     string
	Runtime        *proxy.Runtime
}

type Handler struct {
	pool           *ippool.Pool
	store          *store.Store
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
	publicHost     string
	httpAddr       string
	socks5Addr     string
	rt             *proxy.Runtime
	mu             sync.RWMutex
	sessions       map[string]time.Time
}

const sessionTTL = 24 * time.Hour
const cookieName = "admin_session"

func New(d Deps) *Handler {
	return &Handler{
		pool:           d.Pool,
		store:          d.Store,
		ipBan:          d.IPBan,
		limiter:        d.Limiter,
		tlog:           d.TLog,
		portMgr:        d.PortMgr,
		adminUser:      d.AdminUser,
		adminPass:      d.AdminPass,
		getStats:       d.GetStats,
		getSocks5Stats: d.GetSocks5Stats,
		startTime:      time.Now(),
		staticFS:       d.StaticFS,
		rulesFile:      d.RulesFile,
		publicHost:     d.PublicHost,
		httpAddr:       d.HTTPAddr,
		socks5Addr:     d.SOCKS5Addr,
		rt:             d.Runtime,
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
	mux.HandleFunc("/api/sessions", h.requireAuth(h.apiStickySessions))
	mux.HandleFunc("/api/sessions/clear", h.requireAuth(h.apiClearStickySessions))
	mux.HandleFunc("/api/sessions/remove", h.requireAuth(h.apiRemoveStickySession))
	mux.HandleFunc("/api/sticky-sessions", h.requireAuth(h.apiStickySessions))
	mux.HandleFunc("/api/sticky-sessions/delete", h.requireAuth(h.apiRemoveStickySession))
	mux.HandleFunc("/api/users", h.requireAuth(h.apiAccounts))
	mux.HandleFunc("/api/users/add", h.requireAuth(h.apiAddAccount))
	mux.HandleFunc("/api/users/remove", h.requireAuth(h.apiRemoveAccount))
	mux.HandleFunc("/api/users/rate-limit", h.requireAuth(h.apiPatchAccount))
	mux.HandleFunc("/api/accounts", h.requireAuth(h.apiAccounts))
	mux.HandleFunc("/api/accounts/add", h.requireAuth(h.apiAddAccount))
	mux.HandleFunc("/api/accounts/update", h.requireAuth(h.apiPatchAccount))
	mux.HandleFunc("/api/accounts/remove", h.requireAuth(h.apiRemoveAccount))
	mux.HandleFunc("/api/credentials/generate", h.requireAuth(h.apiGenerateCredentials))
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
	mux.HandleFunc("/api/ipv4-fallback", h.requireAuth(h.apiIPv4Fallback))
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

func (h *Handler) apiOverview(w http.ResponseWriter, r *http.Request) {
	totalReqs, maxLat, minLat := h.pool.GlobalStats()
	if h.tlog != nil {
		totalReqs = int64(h.tlog.Count())
	}
	stats := h.getStats()
	s5stats := h.getSocks5Stats()
	activeSess := 0
	if n, err := h.store.StickyCount(r.Context()); err == nil {
		activeSess = n
	}
	writeJSON(w, map[string]interface{}{
		"uptime_seconds":       int(time.Since(h.startTime).Seconds()),
		"uptime_str":           fmtDuration(time.Since(h.startTime)),
		"pool_size":            h.pool.Count(),
		"prefixes":             h.pool.Prefixes(),
		"prefix":               h.pool.Prefix(),
		"prefix_bits":          h.pool.PrefixBits(),
		"max_ipv6":             h.pool.MaxCapacity(),
		"active_sessions":      activeSess,
		"total_requests":       totalReqs,
		"active_conns":         stats["active_conns"],
		"total_bytes":          stats["total_bytes"],
		"failed_requests":      stats["failed_requests"],
		"ipv6_direct":          stats["ipv6_direct"] + s5stats["ipv6_direct"],
		"ipv4_fallback":          stats["ipv4_fallback"] + s5stats["ipv4_fallback"],
		"ipv4_fallback_enabled":  h.rt != nil && h.rt.AllowIPv4.Load(),
		"max_latency_ms":       maxLat.Milliseconds(),
		"min_latency_ms":       minLat.Milliseconds(),
		"socks5_active_conns":  s5stats["active_conns"],
		"socks5_total_bytes":   s5stats["total_bytes"],
		"socks5_failed_requests": s5stats["failed_requests"],
	})
}



func (h *Handler) apiIPv4Fallback(w http.ResponseWriter, r *http.Request) {
	if h.rt == nil {
		http.Error(w, "runtime unavailable", http.StatusBadGateway)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		h.rt.AllowIPv4.Store(req.Enabled)
		log.Printf("IPv4 fallback %s", map[bool]string{true: "enabled", false: "disabled"}[req.Enabled])
	}
	writeJSON(w, map[string]interface{}{
		"status":  "ok",
		"enabled": h.rt.AllowIPv4.Load(),
	})
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
