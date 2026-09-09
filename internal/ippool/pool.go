package ippool

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ipv6-proxy/internal/ipgen"
)

type Strategy int

const (
	StrategyRoundRobin Strategy = iota
	StrategyRandom
)

type DomainRule struct {
	Domain   string   `json:"domain"`
	Count    int      `json:"count"`
	Strategy Strategy `json:"strategy"`
	counter  atomic.Uint64
	startIdx int
}

type StickySession struct {
	Fingerprint string
	ExitIP      net.IP
	ExitIPs     map[string]bool // all exit IPs used (for domain-rule sessions)
	Domains     map[string]*DomainStat
	TotalHits   int64
	MaxLatency  time.Duration
	MinLatency  time.Duration
	AvgLatency  time.Duration
	totalNs     int64
	CreatedAt   time.Time
	LastSeen    time.Time
}

type DomainStat struct {
	Domain     string
	Hits       int64
	MaxLatency time.Duration
	MinLatency time.Duration
	LastSeen   time.Time
}

// PrefixInfo holds metadata about a single IPv6 prefix in the pool.
type PrefixInfo struct {
	Prefix      string `json:"prefix"`
	Bits        int    `json:"bits"`
	Count       int    `json:"count"`
	MaxCapacity string `json:"max_capacity"`
	Enabled     bool   `json:"enabled"`
}

type Pool struct {
	mu          sync.RWMutex
	prefixes    []PrefixInfo
	addrs       []net.IP                  // active addrs (only from enabled prefixes)
	allAddrs    map[string][]net.IP       // prefix -> all addrs for that prefix
	sessions    map[string]*StickySession // fingerprint -> session
	domainRules map[string]*DomainRule    // domain -> rule
	ttl         time.Duration
}

// parsePrefixSpec parses a prefix string that may include "/bits" notation.
// Returns the clean prefix (without /bits) and the bit length.
// Examples: "2001:db8:abcd" → ("2001:db8:abcd", 48)
//           "2001:db8:abcd:1::2/128" → ("2001:db8:abcd:1::2", 128)
func parsePrefixSpec(spec string) (prefix string, bits int) {
	if idx := strings.LastIndex(spec, "/"); idx != -1 {
		prefix = spec[:idx]
		fmt.Sscanf(spec[idx+1:], "%d", &bits)
		return
	}
	// Legacy: infer from colon count (works for clean prefixes without ::)
	prefix = spec
	groups := 1
	for _, c := range prefix {
		if c == ':' {
			groups++
		}
	}
	bits = groups * 16
	return
}

// maxCapacity returns a human-readable string of the theoretical max IPv6 count for a prefix.
func maxCapacity(bits int) string {
	hostBits := 128 - bits
	switch {
	case hostBits >= 64:
		return fmt.Sprintf("2^%d (~%.1e)", hostBits, math.Pow(2, float64(hostBits)))
	default:
		val := uint64(1) << hostBits
		return fmt.Sprintf("%d", val)
	}
}

// New creates a pool from one or more comma-separated prefixes.
// Each prefix gets `countPer` addresses.
func New(prefixStr string, countPer int, ttl time.Duration) *Pool {
	parts := strings.Split(prefixStr, ",")
	p := &Pool{
		allAddrs:    make(map[string][]net.IP),
		sessions:    make(map[string]*StickySession),
		domainRules: make(map[string]*DomainRule),
		ttl:         ttl,
	}
	for _, raw := range parts {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		pfx, bits := parsePrefixSpec(spec)
		var pfxAddrs []net.IP
		count := countPer
		if bits == 128 {
			// Single IP — parse directly, no suffix generation
			if ip := net.ParseIP(pfx); ip != nil {
				pfxAddrs = append(pfxAddrs, ip)
			}
			count = 1
		} else {
			for i := 1; i <= countPer; i++ {
				if ip := net.ParseIP(fmt.Sprintf("%s::%x", pfx, i)); ip != nil {
					pfxAddrs = append(pfxAddrs, ip)
				}
			}
		}
		info := PrefixInfo{
			Prefix:      pfx,
			Bits:        bits,
			Count:       count,
			MaxCapacity: maxCapacity(bits),
			Enabled:     true,
		}
		p.prefixes = append(p.prefixes, info)
		p.allAddrs[pfx] = pfxAddrs
	}
	p.rebuildAddrs()
	go p.cleanupLoop()
	return p
}

// Resolve returns a sticky IPv6 for the fingerprint and records the hit.
// If domain matches a DomainRule, uses load-balanced selection instead of sticky.
func (p *Pool) Resolve(fingerprint, domain string, latency time.Duration) net.IP {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	// Check domain rules first — override sticky behavior
	if domain != "" {
		if rule, ok := p.domainRules[domain]; ok {
			ip := p.resolveDomainRule(rule)
			// Still record stats on the sticky session
			p.recordHit(fingerprint, domain, latency, ip, now)
			return ip
		}
	}

	sess, ok := p.sessions[fingerprint]

	if ok && p.ttl > 0 && now.Sub(sess.LastSeen) > p.ttl {
		delete(p.sessions, fingerprint)
		ok = false
	}

	if !ok {
		h := sha256.Sum256([]byte(fingerprint))
		idx := int(binary.BigEndian.Uint32(h[:4])) % len(p.addrs)
		sess = &StickySession{
			Fingerprint: fingerprint,
			ExitIP:      p.addrs[idx],
			Domains:     make(map[string]*DomainStat),
			MinLatency:  -1,
			CreatedAt:   now,
		}
		p.sessions[fingerprint] = sess
	}

	sess.TotalHits++
	sess.LastSeen = now
	if latency > 0 {
		sess.totalNs += int64(latency)
		sess.AvgLatency = time.Duration(sess.totalNs / sess.TotalHits)
		if latency > sess.MaxLatency {
			sess.MaxLatency = latency
		}
		if sess.MinLatency < 0 || latency < sess.MinLatency {
			sess.MinLatency = latency
		}
	}

	if domain != "" {
		ds, exists := sess.Domains[domain]
		if !exists {
			ds = &DomainStat{Domain: domain, MinLatency: -1}
			sess.Domains[domain] = ds
		}
		ds.Hits++
		ds.LastSeen = now
		if latency > 0 {
			if latency > ds.MaxLatency {
				ds.MaxLatency = latency
			}
			if ds.MinLatency < 0 || latency < ds.MinLatency {
				ds.MinLatency = latency
			}
		}
	}

	return sess.ExitIP
}

// Lookup returns the sticky IP without recording a hit
func (p *Pool) Lookup(fingerprint string) (net.IP, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if sess, ok := p.sessions[fingerprint]; ok {
		if p.ttl > 0 && time.Since(sess.LastSeen) > p.ttl {
			return nil, false
		}
		return sess.ExitIP, true
	}
	return nil, false
}

func (p *Pool) Count() int { return len(p.addrs) }

// EnabledPrefixes returns CIDR prefixes that may be used for on-the-fly exit IPs.
func (p *Pool) EnabledPrefixes() []ipgen.Prefix {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ipgen.Prefix, 0, len(p.prefixes))
	for _, pi := range p.prefixes {
		if !pi.Enabled {
			continue
		}
		spec := pi.Prefix
		if !strings.Contains(spec, "/") {
			spec = fmt.Sprintf("%s/%d", spec, pi.Bits)
		}
		px, err := ipgen.ParsePrefix(spec)
		if err != nil {
			continue
		}
		out = append(out, px)
	}
	return out
}

// Prefixes returns metadata about all prefixes in the pool.
func (p *Pool) Prefixes() []PrefixInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]PrefixInfo, len(p.prefixes))
	copy(out, p.prefixes)
	return out
}

// rebuildAddrs rebuilds the active addrs slice from enabled prefixes. Must be called with mu held.
func (p *Pool) rebuildAddrs() {
	p.addrs = nil
	for _, pfx := range p.prefixes {
		if pfx.Enabled {
			p.addrs = append(p.addrs, p.allAddrs[pfx.Prefix]...)
		}
	}
}

// SetPrefixEnabled enables or disables a prefix. Disabled prefixes are excluded from IP selection.
// When disabling, any sticky sessions bound to IPs from that prefix are removed so they get reassigned.
func (p *Pool) SetPrefixEnabled(prefix string, enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.prefixes {
		if p.prefixes[i].Prefix == prefix {
			p.prefixes[i].Enabled = enabled
			break
		}
	}
	p.rebuildAddrs()

	// When disabling, purge sessions that are stuck on IPs from this prefix
	if !enabled {
		disabledIPs := make(map[string]bool)
		for _, ip := range p.allAddrs[prefix] {
			disabledIPs[ip.String()] = true
		}
		for fp, sess := range p.sessions {
			if disabledIPs[sess.ExitIP.String()] {
				delete(p.sessions, fp)
			}
		}
	}
	p.rebuildAddrs()
}

// TestPrefix tests connectivity through a prefix by dialing an external IPv6 endpoint.
// The source address is a random host in the prefix (same as live proxy exits).
// Host ids 0 and 1 are skipped: ::1 is the HE tunnel gateway on the tunnel /64
// and is not a usable local source.
func (p *Pool) TestPrefix(prefix string) (string, time.Duration, error) {
	p.mu.RLock()
	var bits int
	found := false
	for _, info := range p.prefixes {
		if info.Prefix == prefix {
			bits = info.Bits
			found = true
			break
		}
	}
	p.mu.RUnlock()
	if !found {
		return "", 0, fmt.Errorf("unknown prefix %s", prefix)
	}

	testIP, err := pickTestExitIP(prefix, bits)
	if err != nil {
		return "", 0, err
	}
	dialer := &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: testIP},
		Timeout:   10 * time.Second,
		Control:   controlFunc,
	}
	start := time.Now()
	var conn net.Conn
	for attempt := 0; attempt < 2; attempt++ {
		conn, err = dialer.Dial("tcp6", "[2001:4860:4860::8888]:53")
		if err == nil {
			break
		}
	}
	elapsed := time.Since(start)
	if err != nil {
		return "", elapsed, fmt.Errorf("dial failed via %s: %v", testIP, err)
	}
	localUsed := conn.LocalAddr().String()
	conn.Close()
	return localUsed, elapsed, nil
}

// pickTestExitIP returns a source address that matches live proxy selection:
// random host bits, never the network address (::) or ::1.
func pickTestExitIP(prefix string, bits int) (net.IP, error) {
	spec := prefix
	if bits > 0 && !strings.Contains(prefix, "/") {
		spec = fmt.Sprintf("%s/%d", prefix, bits)
	}
	p, err := ipgen.ParsePrefix(spec)
	if err != nil {
		return nil, err
	}
	if p.Bits >= 128 {
		return append(net.IP(nil), p.IP...), nil
	}
	for i := 0; i < 16; i++ {
		ip, err := ipgen.Random(p.IP, p.Bits)
		if err != nil {
			return nil, err
		}
		if !reservedTestHost(ip) {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("could not pick a usable test address in %s", p)
}

func reservedTestHost(ip net.IP) bool {
	b := ip.To16()
	if b == nil {
		return true
	}
	return low64(b) <= 1
}

func low64(b net.IP) uint64 {
	var v uint64
	for _, x := range b[8:] {
		v = (v << 8) | uint64(x)
	}
	return v
}

// Prefix returns the first prefix string (backward compat).
func (p *Pool) Prefix() string {
	if len(p.prefixes) > 0 {
		return p.prefixes[0].Prefix
	}
	return ""
}

// PrefixBits returns the first prefix's bit length (backward compat).
func (p *Pool) PrefixBits() int {
	if len(p.prefixes) > 0 {
		return p.prefixes[0].Bits
	}
	return 0
}

// MaxCapacity returns a human-readable string of the first prefix's max (backward compat).
func (p *Pool) MaxCapacity() string {
	if len(p.prefixes) > 0 {
		return p.prefixes[0].MaxCapacity
	}
	return "0"
}

func (p *Pool) SessionCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.sessions)
}

// Sessions returns a snapshot sorted by last seen desc
func (p *Pool) Sessions() []StickySession {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]StickySession, 0, len(p.sessions))
	for _, s := range p.sessions {
		cp := *s
		cp.Domains = make(map[string]*DomainStat, len(s.Domains))
		for k, v := range s.Domains {
			dcopy := *v
			cp.Domains[k] = &dcopy
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// GlobalStats returns aggregate stats across all sessions
func (p *Pool) GlobalStats() (totalReqs int64, maxLat, minLat time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	minLat = -1
	for _, s := range p.sessions {
		totalReqs += s.TotalHits
		if s.MaxLatency > maxLat {
			maxLat = s.MaxLatency
		}
		if s.MinLatency >= 0 && (minLat < 0 || s.MinLatency < minLat) {
			minLat = s.MinLatency
		}
	}
	return
}

func (p *Pool) ClearSessions() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessions = make(map[string]*StickySession)
}

func (p *Pool) RemoveSession(fingerprint string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessions, fingerprint)
}

func (p *Pool) cleanupLoop() {
	if p.ttl <= 0 {
		return
	}
	ticker := time.NewTicker(p.ttl / 2)
	defer ticker.Stop()
	for range ticker.C {
		p.mu.Lock()
		now := time.Now()
		for fp, s := range p.sessions {
			if now.Sub(s.LastSeen) > p.ttl {
				delete(p.sessions, fp)
			}
		}
		p.mu.Unlock()
	}
}

// RecordLatency records latency stats on an existing session without triggering
// domain rule resolution or advancing the round-robin counter.
func (p *Pool) RecordLatency(fingerprint, domain string, latency time.Duration) {
	if latency <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	sess, ok := p.sessions[fingerprint]
	if !ok {
		return // no session to update
	}

	sess.totalNs += int64(latency)
	sess.AvgLatency = time.Duration(sess.totalNs / sess.TotalHits)
	if latency > sess.MaxLatency {
		sess.MaxLatency = latency
	}
	if sess.MinLatency < 0 || latency < sess.MinLatency {
		sess.MinLatency = latency
	}

	if domain != "" {
		if ds, exists := sess.Domains[domain]; exists {
			if latency > ds.MaxLatency {
				ds.MaxLatency = latency
			}
			if ds.MinLatency < 0 || latency < ds.MinLatency {
				ds.MinLatency = latency
			}
		}
	}
}

// recordHit updates session stats for domain-rule resolved IPs.
func (p *Pool) recordHit(fingerprint, domain string, latency time.Duration, ip net.IP, now time.Time) {
	sess, ok := p.sessions[fingerprint]
	if !ok {
		sess = &StickySession{
			Fingerprint: fingerprint,
			ExitIP:      ip,
			ExitIPs:     map[string]bool{ip.String(): true},
			Domains:     make(map[string]*DomainStat),
			MinLatency:  -1,
			CreatedAt:   now,
		}
		p.sessions[fingerprint] = sess
	}
	sess.ExitIP = ip
	if sess.ExitIPs == nil {
		sess.ExitIPs = make(map[string]bool)
	}
	sess.ExitIPs[ip.String()] = true
	sess.TotalHits++
	sess.LastSeen = now
	if latency > 0 {
		sess.totalNs += int64(latency)
		sess.AvgLatency = time.Duration(sess.totalNs / sess.TotalHits)
		if latency > sess.MaxLatency {
			sess.MaxLatency = latency
		}
		if sess.MinLatency < 0 || latency < sess.MinLatency {
			sess.MinLatency = latency
		}
	}
	if domain != "" {
		ds, exists := sess.Domains[domain]
		if !exists {
			ds = &DomainStat{Domain: domain, MinLatency: -1}
			sess.Domains[domain] = ds
		}
		ds.Hits++
		ds.LastSeen = now
		if latency > 0 {
			if latency > ds.MaxLatency {
				ds.MaxLatency = latency
			}
			if ds.MinLatency < 0 || latency < ds.MinLatency {
				ds.MinLatency = latency
			}
		}
	}
}

// resolveDomainRule picks an IP from the pool based on the rule's strategy.
func (p *Pool) resolveDomainRule(rule *DomainRule) net.IP {
	count := rule.Count
	if count > len(p.addrs) {
		count = len(p.addrs)
	}
	var idx int
	switch rule.Strategy {
	case StrategyRoundRobin:
		seq := rule.counter.Add(1) - 1
		idx = (rule.startIdx + int(seq)%count) % len(p.addrs)
	case StrategyRandom:
		offset := rand.Intn(count)
		idx = (rule.startIdx + offset) % len(p.addrs)
	default:
		idx = rule.startIdx % len(p.addrs)
	}
	return p.addrs[idx]
}

// AddDomainRule adds a load-balancing rule for a domain.
func (p *Pool) AddDomainRule(domain string, count int, strategy Strategy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h := sha256.Sum256([]byte(domain))
	startIdx := int(binary.BigEndian.Uint32(h[:4])) % len(p.addrs)
	p.domainRules[domain] = &DomainRule{
		Domain:   domain,
		Count:    count,
		Strategy: strategy,
		startIdx: startIdx,
	}
}

// RemoveDomainRule removes a domain load-balancing rule.
func (p *Pool) RemoveDomainRule(domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.domainRules, domain)
}

// DomainRules returns a snapshot of all domain rules.
func (p *Pool) DomainRules() []DomainRule {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]DomainRule, 0, len(p.domainRules))
	for _, r := range p.domainRules {
		out = append(out, DomainRule{
			Domain:   r.Domain,
			Count:    r.Count,
			Strategy: r.Strategy,
		})
	}
	return out
}

// SaveDomainRules persists domain rules to a JSON file.
func (p *Pool) SaveDomainRules(path string) error {
	rules := p.DomainRules()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadDomainRules loads domain rules from a JSON file.
func (p *Pool) LoadDomainRules(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var rules []DomainRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return err
	}
	for i := range rules {
		p.AddDomainRule(rules[i].Domain, rules[i].Count, rules[i].Strategy)
	}
	return nil
}

// Expand adds more IPv6 addresses distributed across all prefixes.
func (p *Pool) Expand(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Count expandable prefixes (skip /128 single-IP)
	expandable := 0
	for _, pfx := range p.prefixes {
		if pfx.Bits < 128 {
			expandable++
		}
	}
	if expandable == 0 {
		return
	}
	perPrefix := count / expandable
	remainder := count % expandable
	ei := 0
	for i := range p.prefixes {
		if p.prefixes[i].Bits == 128 {
			continue
		}
		n := perPrefix
		if ei < remainder {
			n++
		}
		ei++
		pfx := p.prefixes[i].Prefix
		start := p.prefixes[i].Count + 1
		for j := start; j < start+n; j++ {
			if ip := net.ParseIP(fmt.Sprintf("%s::%x", pfx, j)); ip != nil {
				p.allAddrs[pfx] = append(p.allAddrs[pfx], ip)
			}
		}
		p.prefixes[i].Count += n
	}
	p.rebuildAddrs()
}
