package session

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"ipv6-proxy/internal/credential"
	"ipv6-proxy/internal/ipgen"
	"ipv6-proxy/internal/store"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type staticPrefixes []ipgen.Prefix

func (s staticPrefixes) EnabledPrefixes() []ipgen.Prefix { return []ipgen.Prefix(s) }

func setupResolver(t *testing.T) (*Resolver, *store.Store, *miniredis.Miniredis) {
	t.Helper()
	store.SetBcryptCost(bcrypt.MinCost)
	mr := miniredis.RunT(t)
	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	st := store.New(cli, "ipv6p:")
	p, err := ipgen.ParsePrefix("2001:db8:abcd::/64")
	if err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Store: st, Prefixes: staticPrefixes{p}}
	return r, st, mr
}

func TestRotateDifferentIPs(t *testing.T) {
	r, _, _ := setupResolver(t)
	ctx := context.Background()
	p := credential.ApplyDefaults(credential.Parsed{Account: "u", Mode: credential.ModeRotate}, credential.ModeRotate, 10, 180)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		ip, err := r.Resolve(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		seen[ip.String()] = true
	}
	if len(seen) < 2 {
		t.Fatalf("rotate produced %d unique ips", len(seen))
	}
}

func TestStickySameSID(t *testing.T) {
	r, _, _ := setupResolver(t)
	ctx := context.Background()
	p := credential.ApplyDefaults(credential.Parsed{
		Account: "u", SID: "11111111", TTLMinutes: 10, Mode: credential.ModeSticky,
	}, credential.ModeRotate, 10, 180)
	ip1, err := r.Resolve(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	ip2, err := r.Resolve(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if !ip1.Equal(ip2) {
		t.Fatalf("%s vs %s", ip1, ip2)
	}
}

func TestRotateModeDoesNotWriteKey(t *testing.T) {
	r, st, mr := setupResolver(t)
	ctx := context.Background()
	parsed, _ := credential.Parse("u_sid_22222222_time_10_mode_rotate")
	p := credential.ApplyDefaults(parsed, credential.ModeSticky, 10, 180)
	if _, err := r.Resolve(ctx, p); err != nil {
		t.Fatal(err)
	}
	keys := mr.Keys()
	for _, k := range keys {
		if len(k) >= 4 && (contains(k, ":sess:")) {
			t.Fatalf("unexpected sess key %s", k)
		}
	}
	n, err := st.StickyCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("count=%d", n)
	}
}

func TestStickyExpires(t *testing.T) {
	r, _, mr := setupResolver(t)
	ctx := context.Background()
	p := credential.ApplyDefaults(credential.Parsed{
		Account: "u", SID: "33333333", TTLMinutes: 1, Mode: credential.ModeSticky,
	}, credential.ModeRotate, 1, 180)
	ip1, err := r.Resolve(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	mr.FastForward(61 * time.Second)
	ip2, err := r.Resolve(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if ip1.Equal(ip2) {
		t.Fatalf("expected new ip after expiry")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
