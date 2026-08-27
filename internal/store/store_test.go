package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"ipv6-proxy/internal/credential"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setup(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	SetBcryptCost(bcrypt.MinCost)
	mr := miniredis.RunT(t)
	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	return New(cli, "ipv6p:"), mr
}

func TestAccountAuth(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	err := s.CreateAccount(ctx, Account{
		Username:    "caomao002",
		Enabled:     true,
		DefaultMode: credential.ModeRotate,
		DefaultTTL:  10,
		MaxTTL:      180,
	}, "Aq112211")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, "caomao002", "wrong"); err != ErrBadPass {
		t.Fatalf("got %v", err)
	}
	a, err := s.Authenticate(ctx, "caomao002", "Aq112211")
	if err != nil || a.Username != "caomao002" {
		t.Fatalf("%v %+v", err, a)
	}
}

func TestEnsureDoesNotClobber(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	_ = s.CreateAccount(ctx, Account{Username: "u", Enabled: true}, "first")
	if err := s.EnsureAccount(ctx, "u", "second", Account{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, "u", "first"); err != nil {
		t.Fatal(err)
	}
}

func TestStickyGetOrCreateSameIP(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	ip1, err := s.GetOrCreateSticky(ctx, "u", "sid1", "2001:db8::1", "2001:db8::/64", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	ip2, err := s.GetOrCreateSticky(ctx, "u", "sid1", "2001:db8::2", "2001:db8::/64", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ip1 != "2001:db8::1" || ip2 != ip1 {
		t.Fatalf("%s %s", ip1, ip2)
	}
}

func TestStickyConcurrentSameSID(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	const n = 100
	ips := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			cand := "2001:db8::" + strconvHex(i+1)
			ip, err := s.GetOrCreateSticky(ctx, "u", "shared", cand, "2001:db8::/64", time.Minute, 1)
			if err != nil {
				t.Errorf("%v", err)
				return
			}
			ips[i] = ip
		}()
	}
	wg.Wait()
	first := ""
	for _, ip := range ips {
		if ip == "" {
			t.Fatal("empty ip")
		}
		if first == "" {
			first = ip
		} else if ip != first {
			t.Fatalf("got both %s and %s", first, ip)
		}
	}
}

func TestStickyExpiryNewIP(t *testing.T) {
	s, mr := setup(t)
	ctx := context.Background()
	ip1, err := s.GetOrCreateSticky(ctx, "u", "sid1", "2001:db8::1", "2001:db8::/64", 2*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	mr.FastForward(3 * time.Second)
	ip2, err := s.GetOrCreateSticky(ctx, "u", "sid1", "2001:db8::2", "2001:db8::/64", 2*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ip1 == ip2 {
		t.Fatalf("expected new ip after expiry, both %s", ip1)
	}
	if ip2 != "2001:db8::2" {
		t.Fatalf("got %s", ip2)
	}
}

func TestStickyTTLNotRefreshed(t *testing.T) {
	s, mr := setup(t)
	ctx := context.Background()
	_, err := s.GetOrCreateSticky(ctx, "u", "sid1", "2001:db8::1", "2001:db8::/64", 5*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	mr.FastForward(3 * time.Second)
	_, err = s.GetOrCreateSticky(ctx, "u", "sid1", "2001:db8::9", "2001:db8::/64", 5*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	mr.FastForward(3 * time.Second)
	// original 5s should have expired (3+3), even though we hit at t=3
	ip, err := s.GetOrCreateSticky(ctx, "u", "sid1", "2001:db8::2", "2001:db8::/64", 5*time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "2001:db8::2" {
		t.Fatalf("ttl was refreshed, still %s", ip)
	}
}

func strconvHex(n int) string {
	const hex = "0123456789abcdef"
	if n < 16 {
		return string(hex[n])
	}
	return strconvHex(n/16) + string(hex[n%16])
}
