package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"ipv6-proxy/internal/credential"
	"ipv6-proxy/internal/store"
)

type accountJSON struct {
	User        string `json:"user"`
	Enabled     bool   `json:"enabled"`
	DefaultMode string `json:"default_mode"`
	DefaultTTL  int    `json:"default_ttl"`
	MaxTTL      int    `json:"max_ttl"`
	RateLimit   int    `json:"rate_limit"`
	CreatedAt   int64  `json:"created_at"`
}

func accountToJSON(a store.Account) accountJSON {
	return accountJSON{
		User:        a.Username,
		Enabled:     a.Enabled,
		DefaultMode: string(a.DefaultMode),
		DefaultTTL:  a.DefaultTTL,
		MaxTTL:      a.MaxTTL,
		RateLimit:   a.RateLimit,
		CreatedAt:   a.CreatedAt,
	}
}

func (h *Handler) apiAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListAccounts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	out := make([]accountJSON, 0, len(list))
	for _, a := range list {
		out = append(out, accountToJSON(a))
	}
	writeJSON(w, map[string]interface{}{"users": out, "accounts": out})
}

func (h *Handler) apiAddAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		User        string `json:"user"`
		Pass        string `json:"pass"`
		Enabled     *bool  `json:"enabled"`
		DefaultMode string `json:"default_mode"`
		DefaultTTL  int    `json:"default_ttl"`
		MaxTTL      int    `json:"max_ttl"`
		RateLimit   int    `json:"rate_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	mode := credential.Mode(req.DefaultMode)
	if mode != credential.ModeSticky {
		mode = credential.ModeRotate
	}
	err := h.store.CreateAccount(r.Context(), store.Account{
		Username:    req.User,
		Enabled:     enabled,
		DefaultMode: mode,
		DefaultTTL:  req.DefaultTTL,
		MaxTTL:      req.MaxTTL,
		RateLimit:   req.RateLimit,
	}, req.Pass)
	if err != nil {
		writeJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	h.syncLimiter(req.User, req.RateLimit)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiPatchAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		User        string `json:"user"`
		Pass        string `json:"pass"`
		Enabled     *bool  `json:"enabled"`
		DefaultMode string `json:"default_mode"`
		DefaultTTL  *int   `json:"default_ttl"`
		MaxTTL      *int   `json:"max_ttl"`
		RateLimit   *int   `json:"rate_limit"`
		Limit       *int   `json:"limit"` // alias used by old rate-limit UI
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.RateLimit == nil && req.Limit != nil {
		req.RateLimit = req.Limit
	}
	err := h.store.UpdateAccount(r.Context(), req.User, func(a *store.Account) error {
		if req.Enabled != nil {
			a.Enabled = *req.Enabled
		}
		if req.DefaultMode != "" {
			if req.DefaultMode == string(credential.ModeSticky) {
				a.DefaultMode = credential.ModeSticky
			} else {
				a.DefaultMode = credential.ModeRotate
			}
		}
		if req.DefaultTTL != nil {
			a.DefaultTTL = *req.DefaultTTL
		}
		if req.MaxTTL != nil {
			a.MaxTTL = *req.MaxTTL
		}
		if req.RateLimit != nil {
			a.RateLimit = *req.RateLimit
		}
		return nil
	}, req.Pass)
	if err != nil {
		writeJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	if req.RateLimit != nil {
		h.syncLimiter(req.User, *req.RateLimit)
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiRemoveAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteAccount(r.Context(), req.User); err != nil {
		writeJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	if h.limiter != nil {
		h.limiter.SetUserLimit(req.User, 0)
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiGenerateCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Account    string `json:"account"`
		Mode       string `json:"mode"`
		TTLMinutes int    `json:"ttl_minutes"`
		Count      int    `json:"count"`
		Protocol   string `json:"protocol"`
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Password   string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 100 {
		req.Count = 100
	}
	if _, err := h.store.Authenticate(r.Context(), req.Account, req.Password); err != nil {
		writeJSON(w, map[string]string{"status": "error", "message": "account/password mismatch"})
		return
	}
	acct, err := h.store.GetAccount(r.Context(), req.Account)
	if err != nil {
		writeJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	mode := credential.Mode(req.Mode)
	if mode != credential.ModeSticky {
		mode = credential.ModeRotate
	}
	ttl := req.TTLMinutes
	if ttl <= 0 {
		ttl = acct.DefaultTTL
	}
	if ttl < credential.MinTTLMinutes {
		ttl = credential.MinTTLMinutes
	}
	if ttl > acct.MaxTTL {
		ttl = acct.MaxTTL
	}

	host := strings.TrimSpace(req.Host)
	if host == "" {
		host = h.publicHost
	}
	port := req.Port
	proto := strings.ToLower(req.Protocol)
	if port <= 0 {
		if proto == "socks5" || proto == "socks" {
			port = listenPort(h.socks5Addr)
		} else {
			port = listenPort(h.httpAddr)
		}
	}

	lines := make([]string, 0, req.Count)
	usernames := make([]string, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		sid := ""
		if mode == credential.ModeSticky {
			sid, err = credential.RandomSID()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		user := credential.FormatUsername(req.Account, sid, ttl, mode)
		usernames = append(usernames, user)
		lines = append(lines, fmt.Sprintf("%s:%s@%s:%d", user, req.Password, host, port))
	}

	curl := ""
	if len(lines) > 0 {
		if proto == "socks5" || proto == "socks" {
			curl = fmt.Sprintf("curl --socks5 %q https://ipv6.ip.sb", lines[0])
		} else {
			curl = fmt.Sprintf("curl -x http://%s https://ipv6.ip.sb", lines[0])
		}
	}
	writeJSON(w, map[string]interface{}{
		"status":    "ok",
		"lines":     lines,
		"usernames": usernames,
		"curl":      curl,
	})
}

type stickyJSON struct {
	Account   string `json:"account"`
	SID       string `json:"sid"`
	ExitIP    string `json:"exit_ip"`
	Prefix    string `json:"prefix"`
	TTLMin    int    `json:"ttl_min"`
	Hits      int64  `json:"hits"`
	CreatedAt string `json:"created_at"`
	LastSeen  string `json:"last_seen"`
	TTLRemain int64  `json:"ttl_remain_sec"`
}

func (h *Handler) apiStickySessions(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListStickySessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	out := make([]stickyJSON, 0, len(list))
	for _, s := range list {
		out = append(out, stickyJSON{
			Account:   s.Account,
			SID:       s.SID,
			ExitIP:    s.ExitIP,
			Prefix:    s.Prefix,
			TTLMin:    s.TTLMin,
			Hits:      s.Hits,
			CreatedAt: time.Unix(s.CreatedAt, 0).Format("15:04:05"),
			LastSeen:  time.Unix(s.LastSeen, 0).Format("15:04:05"),
			TTLRemain: int64(s.TTLRemain.Seconds()),
		})
	}
	writeJSON(w, out)
}

func (h *Handler) apiClearStickySessions(w http.ResponseWriter, r *http.Request) {
	if err := h.store.ClearSticky(r.Context()); err != nil {
		writeJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) apiRemoveStickySession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account     string `json:"account"`
		SID         string `json:"sid"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	account, sid := req.Account, req.SID
	if account == "" || sid == "" {
		account, sid = splitFingerprint(req.Fingerprint)
	}
	if account == "" || sid == "" {
		http.Error(w, "account and sid required", http.StatusBadRequest)
		return
	}
	if err := h.store.DeleteSticky(r.Context(), account, sid); err != nil && !errors.Is(err, store.ErrNotFound) {
		writeJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) syncLimiter(user string, limit int) {
	if h.limiter == nil {
		return
	}
	h.limiter.SetUserLimit(user, limit)
}

func listenPort(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	var n int
	fmt.Sscanf(port, "%d", &n)
	return n
}

func splitFingerprint(fp string) (string, string) {
	if fp == "" {
		return "", ""
	}
	parts := strings.SplitN(fp, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", fp
}
