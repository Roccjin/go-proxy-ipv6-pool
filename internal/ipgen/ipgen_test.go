package ipgen

import (
	"net"
	"testing"
)

func TestParsePrefixCIDR(t *testing.T) {
	p, err := ParsePrefix("2001:470:24:692::/64")
	if err != nil {
		t.Fatal(err)
	}
	if p.Bits != 64 {
		t.Fatalf("bits=%d", p.Bits)
	}
	want := net.ParseIP("2001:470:24:692::")
	if !p.IP.Equal(want) {
		t.Fatalf("ip=%s want %s", p.IP, want)
	}
}

func TestParsePrefixLegacy(t *testing.T) {
	p, err := ParsePrefix("2001:db8:abcd")
	if err != nil {
		t.Fatal(err)
	}
	if p.Bits != 48 {
		t.Fatalf("bits=%d", p.Bits)
	}
}

func TestRandomKeepsPrefixBits(t *testing.T) {
	cases := []struct {
		spec string
		bits int
	}{
		{"2001:db8:abcd::/48", 48},
		{"2001:db8:abcd:1234::/64", 64},
		{"2001:db8:abcd:1234:5678::/80", 80},
		{"2001:db8::1/128", 128},
	}
	for _, tc := range cases {
		p, err := ParsePrefix(tc.spec)
		if err != nil {
			t.Fatal(err)
		}
		ip, err := Random(p.IP, p.Bits)
		if err != nil {
			t.Fatal(err)
		}
		if !prefixEqual(p.IP, ip, p.Bits) {
			t.Fatalf("%s: prefix bits changed: prefix=%s ip=%s", tc.spec, p.IP, ip)
		}
		if p.Bits < 128 && ip.Equal(p.IP) {
			// /64+ should almost never return the network address; retry a few times
			ok := false
			for i := 0; i < 8; i++ {
				ip, _ = Random(p.IP, p.Bits)
				if !ip.Equal(p.IP) {
					ok = true
					break
				}
			}
			if !ok && p.Bits <= 64 {
				t.Fatalf("random host still equals network address")
			}
		}
	}
}

func TestRandom128IsFixed(t *testing.T) {
	p, _ := ParsePrefix("2001:db8::1/128")
	ip1, err := Random(p.IP, 128)
	if err != nil {
		t.Fatal(err)
	}
	ip2, _ := Random(p.IP, 128)
	if !ip1.Equal(p.IP) || !ip2.Equal(p.IP) {
		t.Fatalf("got %s %s", ip1, ip2)
	}
}

func TestRandomFromEmpty(t *testing.T) {
	if _, _, err := RandomFrom(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestRandomNotSequential(t *testing.T) {
	p, _ := ParsePrefix("2001:470:24:692::/64")
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		ip, err := Random(p.IP, 64)
		if err != nil {
			t.Fatal(err)
		}
		s := ip.String()
		if s == "2001:470:24:692::1" || s == "2001:470:24:692::2" {
			t.Fatalf("sequential-looking address %s", s)
		}
		seen[s] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected multiple addresses, got %d", len(seen))
	}
}

func prefixEqual(a, b net.IP, bits int) bool {
	a16, b16 := a.To16(), b.To16()
	for i := 0; i < bits; i++ {
		byteIdx := i / 8
		bitIdx := uint(7 - (i % 8))
		if (a16[byteIdx]>>bitIdx)&1 != (b16[byteIdx]>>bitIdx)&1 {
			return false
		}
	}
	return true
}
