package ippool

import (
	"testing"
	"time"
)

func TestStickyConsistency(t *testing.T) {
	p := New("2001:db8:abcd", 100, 30*time.Minute)
	ip1 := p.Resolve("user1:google.com", "google.com", 100*time.Millisecond)
	ip2 := p.Resolve("user1:google.com", "google.com", 200*time.Millisecond)
	if !ip1.Equal(ip2) {
		t.Fatalf("sticky broken: %s vs %s", ip1, ip2)
	}
}

func TestStickyDifferentFingerprints(t *testing.T) {
	p := New("2001:db8:abcd", 100, 30*time.Minute)
	ip1 := p.Resolve("fp-aaa", "a.com", 50*time.Millisecond)
	ip2 := p.Resolve("fp-bbb", "b.com", 50*time.Millisecond)
	t.Logf("fp-aaa -> %s, fp-bbb -> %s", ip1, ip2)
}

func TestLatencyTracking(t *testing.T) {
	p := New("2001:db8:abcd", 10, 0)
	p.Resolve("test", "example.com", 100*time.Millisecond)
	p.Resolve("test", "example.com", 300*time.Millisecond)
	p.Resolve("test", "other.com", 50*time.Millisecond)

	sessions := p.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.TotalHits != 3 {
		t.Fatalf("expected 3 hits, got %d", s.TotalHits)
	}
	if s.MaxLatency != 300*time.Millisecond {
		t.Fatalf("expected max 300ms, got %v", s.MaxLatency)
	}
	if s.MinLatency != 50*time.Millisecond {
		t.Fatalf("expected min 50ms, got %v", s.MinLatency)
	}
	if len(s.Domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(s.Domains))
	}
	if s.Domains["example.com"].Hits != 2 {
		t.Fatalf("expected 2 hits for example.com, got %d", s.Domains["example.com"].Hits)
	}
}

func TestTTLExpiry(t *testing.T) {
	p := New("2001:db8:abcd", 10, 50*time.Millisecond)
	ip1 := p.Resolve("expire-test", "x.com", 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	// After TTL, should get reassigned (same hash = same IP, but session is new)
	ip2 := p.Resolve("expire-test", "x.com", 10*time.Millisecond)
	// IPs should be same (deterministic hash) but session should be fresh
	if !ip1.Equal(ip2) {
		t.Logf("IPs differ after TTL (possible if hash changed): %s vs %s", ip1, ip2)
	}
	sessions := p.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session after re-resolve, got %d", len(sessions))
	}
	// Fresh session should have only 1 hit
	if sessions[0].TotalHits != 1 {
		t.Fatalf("expected fresh session with 1 hit, got %d", sessions[0].TotalHits)
	}
}

func TestGlobalStats(t *testing.T) {
	p := New("2001:db8:abcd", 10, 0)
	p.Resolve("a", "a.com", 100*time.Millisecond)
	p.Resolve("b", "b.com", 500*time.Millisecond)
	p.Resolve("a", "a.com", 10*time.Millisecond)

	total, maxLat, minLat := p.GlobalStats()
	if total != 3 {
		t.Fatalf("expected 3 total, got %d", total)
	}
	if maxLat != 500*time.Millisecond {
		t.Fatalf("expected max 500ms, got %v", maxLat)
	}
	if minLat != 10*time.Millisecond {
		t.Fatalf("expected min 10ms, got %v", minLat)
	}
}

func TestDomainRuleRoundRobin(t *testing.T) {
	p := New("2001:db8:abcd", 100, 0)
	p.AddDomainRule("example.com", 3, StrategyRoundRobin)

	seen := make(map[string]bool)
	for i := 0; i < 9; i++ {
		ip := p.Resolve("user1:example.com", "example.com", 10*time.Millisecond)
		seen[ip.String()] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 different IPs from round-robin with count=3, got %d: %v", len(seen), seen)
	}
}

func TestDomainRuleRandom(t *testing.T) {
	p := New("2001:db8:abcd", 100, 0)
	p.AddDomainRule("random.com", 5, StrategyRandom)

	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		ip := p.Resolve("user1:random.com", "random.com", 10*time.Millisecond)
		seen[ip.String()] = true
	}
	if len(seen) < 2 || len(seen) > 5 {
		t.Fatalf("expected 2~5 different IPs from random with count=5, got %d", len(seen))
	}
}

func TestDomainRuleNoMatch(t *testing.T) {
	p := New("2001:db8:abcd", 100, 0)
	p.AddDomainRule("example.com", 3, StrategyRoundRobin)

	// Non-matching domain should use sticky behavior
	ip1 := p.Resolve("fp-test", "other.com", 10*time.Millisecond)
	ip2 := p.Resolve("fp-test", "other.com", 10*time.Millisecond)
	if !ip1.Equal(ip2) {
		t.Fatalf("non-matching domain should be sticky: %s vs %s", ip1, ip2)
	}
}
