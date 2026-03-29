package auth

import (
	"net"
	"sync"
	"time"
)

type BannedIP struct {
	IP       string    `json:"ip"`
	BannedAt time.Time `json:"banned_at"`
	Failures int       `json:"failures"`
}

type IPBan struct {
	mu        sync.RWMutex
	failures  map[string][]time.Time
	banned    map[string]time.Time
	whitelist map[string]bool
	threshold int
	window    time.Duration
}

func NewIPBan(threshold int, window time.Duration) *IPBan {
	return &IPBan{
		failures:  make(map[string][]time.Time),
		banned:    make(map[string]time.Time),
		whitelist: make(map[string]bool),
		threshold: threshold,
		window:    window,
	}
}

func (b *IPBan) AddWhitelist(ip string) {
	ip = extractIP(ip)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.whitelist[ip] = true
	// Auto-unban if currently banned
	delete(b.banned, ip)
	delete(b.failures, ip)
}

func (b *IPBan) RemoveWhitelist(ip string) {
	ip = extractIP(ip)
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.whitelist, ip)
}

func (b *IPBan) IsWhitelisted(ip string) bool {
	ip = extractIP(ip)
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.whitelist[ip]
}

func (b *IPBan) WhitelistedList() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.whitelist))
	for ip := range b.whitelist {
		out = append(out, ip)
	}
	return out
}

func (b *IPBan) RecordFailure(ip string) {
	ip = extractIP(ip)
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.whitelist[ip] {
		return
	}

	// Prune old entries outside the window
	cutoff := now.Add(-b.window)
	times := b.failures[ip]
	valid := make([]time.Time, 0, len(times))
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	valid = append(valid, now)
	b.failures[ip] = valid

	if len(valid) >= b.threshold {
		b.banned[ip] = now
	}
}

func (b *IPBan) IsBanned(ip string) bool {
	ip = extractIP(ip)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.whitelist[ip] {
		return false
	}
	t, ok := b.banned[ip]
	if !ok {
		return false
	}
	// Auto-expire bans after the window duration
	if time.Since(t) > b.window {
		delete(b.banned, ip)
		delete(b.failures, ip)
		return false
	}
	return true
}

func (b *IPBan) Unban(ip string) {
	ip = extractIP(ip)
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.banned, ip)
	delete(b.failures, ip)
}

// Ban manually adds an IP to the ban list.
func (b *IPBan) Ban(ip string) {
	ip = extractIP(ip)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.banned[ip] = time.Now()
}

func (b *IPBan) BannedList() []BannedIP {
	b.mu.RLock()
	defer b.mu.RUnlock()
	list := make([]BannedIP, 0, len(b.banned))
	for ip, t := range b.banned {
		count := len(b.failures[ip])
		list = append(list, BannedIP{IP: ip, BannedAt: t, Failures: count})
	}
	return list
}

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
