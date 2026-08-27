package proxy

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"ipv6-proxy/internal/auth"
	"ipv6-proxy/internal/credential"
	"ipv6-proxy/internal/ratelimit"
	"ipv6-proxy/internal/session"
	"ipv6-proxy/internal/store"
	"ipv6-proxy/internal/trafficlog"
)

var (
	ErrAuth = errors.New("proxy authentication failed")
	ErrRate = errors.New("rate limit exceeded")
)

type Runtime struct {
	Store     *store.Store
	Resolver  *session.Resolver
	IPBan     *auth.IPBan
	Limiter   *ratelimit.Limiter
	TLog      *trafficlog.Logger
	AllowIPv4 atomic.Bool
}

func (rt *Runtime) ipv4Allowed() bool {
	if rt == nil {
		return false
	}
	return rt.AllowIPv4.Load()
}

type AdmitResult struct {
	Parsed  credential.Parsed
	Account *store.Account
	ExitIP  string
}

func (rt *Runtime) Admit(ctx context.Context, rawUser, pass, extraSID string) (AdmitResult, error) {
	parsed, err := credential.Parse(rawUser)
	if err != nil {
		return AdmitResult{}, ErrAuth
	}
	acct, err := rt.Store.Authenticate(ctx, parsed.Account, pass)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrDisabled) || errors.Is(err, store.ErrBadPass) {
			return AdmitResult{}, ErrAuth
		}
		return AdmitResult{}, err
	}
	parsed = credential.ApplyDefaults(parsed, acct.DefaultMode, acct.DefaultTTL, acct.MaxTTL)
	if parsed.Mode == credential.ModeSticky && parsed.SID == "" && extraSID != "" && credential.ValidSID(extraSID) {
		parsed.SID = extraSID
	}
	if parsed.Mode == credential.ModeSticky && parsed.SID == "" {
		parsed.Mode = credential.ModeRotate
	}
	if rt.Limiter != nil && acct.RateLimit != 0 {
		if !rt.Limiter.Allow(acct.Username) {
			return AdmitResult{Parsed: parsed, Account: acct}, ErrRate
		}
	}
	ip, err := rt.Resolver.Resolve(ctx, parsed)
	if err != nil {
		return AdmitResult{Parsed: parsed, Account: acct}, err
	}
	return AdmitResult{Parsed: parsed, Account: acct, ExitIP: ip.String()}, nil
}

func extraSIDFromHeaders(h http.Header) string {
	if h == nil {
		return ""
	}
	if fp := h.Get("X-Fingerprint"); fp != "" {
		return fp
	}
	return h.Get("X-Sticky-Key")
}
