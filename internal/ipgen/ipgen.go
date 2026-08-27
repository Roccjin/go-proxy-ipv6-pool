package ipgen

import (
	"crypto/rand"
	"fmt"
	"net"
	"strings"
)

// Prefix is an IPv6 network used to generate exit addresses.
type Prefix struct {
	IP   net.IP
	Bits int
}

func (p Prefix) String() string {
	if p.IP == nil {
		return ""
	}
	return fmt.Sprintf("%s/%d", p.IP.String(), p.Bits)
}

// ParsePrefix accepts CIDR ("2001:db8::/64") or a truncated prefix
// ("2001:db8:abcd") matching the historical pool flag format.
func ParsePrefix(spec string) (Prefix, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Prefix{}, fmt.Errorf("empty prefix")
	}

	ipPart := spec
	bits := -1
	if i := strings.LastIndex(spec, "/"); i >= 0 {
		ipPart = spec[:i]
		n, err := parseBits(spec[i+1:])
		if err != nil {
			return Prefix{}, fmt.Errorf("invalid prefix bits in %q", spec)
		}
		bits = n
	}

	ip := parseIPv6(ipPart)
	if ip == nil {
		return Prefix{}, fmt.Errorf("invalid IPv6 prefix %q", spec)
	}
	if bits < 0 {
		bits = inferBits(ipPart)
	}
	if bits < 0 || bits > 128 {
		return Prefix{}, fmt.Errorf("invalid prefix bits %d", bits)
	}
	return Prefix{IP: maskPrefix(ip, bits), Bits: bits}, nil
}

// Random returns an address in prefix with a cryptographically random host.
// A /128 returns the prefix address itself.
func Random(prefix net.IP, bits int) (net.IP, error) {
	p16 := prefix.To16()
	if p16 == nil {
		return nil, fmt.Errorf("not an IPv6 address")
	}
	if bits < 0 || bits > 128 {
		return nil, fmt.Errorf("invalid prefix bits %d", bits)
	}
	out := make(net.IP, 16)
	copy(out, p16)
	if bits == 128 {
		return out, nil
	}

	hostBits := 128 - bits
	hostBytes := (hostBits + 7) / 8
	rnd := make([]byte, hostBytes)
	if _, err := rand.Read(rnd); err != nil {
		return nil, err
	}
	start := 16 - hostBytes
	copy(out[start:], rnd)
	if rem := hostBits % 8; rem != 0 {
		prefixMask := byte(0xFF << rem)
		out[start] = (p16[start] & prefixMask) | (out[start] & ^prefixMask)
	}
	return out, nil
}

// RandomFrom picks a prefix uniformly, then a random host inside it.
func RandomFrom(prefixes []Prefix) (net.IP, Prefix, error) {
	if len(prefixes) == 0 {
		return nil, Prefix{}, fmt.Errorf("no enabled IPv6 prefixes")
	}
	idx, err := randInt(len(prefixes))
	if err != nil {
		return nil, Prefix{}, err
	}
	p := prefixes[idx]
	ip, err := Random(p.IP, p.Bits)
	if err != nil {
		return nil, Prefix{}, err
	}
	return ip, p, nil
}

func parseBits(s string) (int, error) {
	n := 0
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	if n < 0 || n > 128 {
		return 0, fmt.Errorf("out of range")
	}
	return n, nil
}

func parseIPv6(s string) net.IP {
	if ip := net.ParseIP(s); ip != nil {
		return ip.To16()
	}
	if ip := net.ParseIP(s + "::"); ip != nil {
		return ip.To16()
	}
	return nil
}

func inferBits(s string) int {
	s = strings.TrimSuffix(s, "::")
	groups := 1
	for _, c := range s {
		if c == ':' {
			groups++
		}
	}
	bits := groups * 16
	if bits > 128 {
		return 128
	}
	if bits < 0 {
		return 0
	}
	return bits
}

func maskPrefix(ip net.IP, bits int) net.IP {
	out := make(net.IP, 16)
	copy(out, ip.To16())
	if bits >= 128 {
		return out
	}
	for i := bits; i < 128; i++ {
		byteIdx := i / 8
		bitIdx := uint(7 - (i % 8))
		out[byteIdx] &^= 1 << bitIdx
	}
	return out
}

func randInt(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("empty set")
	}
	if n == 1 {
		return 0, nil
	}
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return 0, err
	}
	var v uint64
	for _, x := range b {
		v = (v << 8) | uint64(x)
	}
	return int(v % uint64(n)), nil
}
