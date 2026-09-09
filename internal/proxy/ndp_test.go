package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"time"
)

type stubConn struct {
	local net.Addr
}

func (c stubConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c stubConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c stubConn) Close() error                     { return nil }
func (c stubConn) LocalAddr() net.Addr              { return c.local }
func (c stubConn) RemoteAddr() net.Addr             { return c.local }
func (c stubConn) SetDeadline(time.Time) error      { return nil }
func (c stubConn) SetReadDeadline(time.Time) error  { return nil }
func (c stubConn) SetWriteDeadline(time.Time) error { return nil }

type timeoutError struct{}

func (timeoutError) Error() string   { return "dial tcp: i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestNDPWarmCacheSeenAndExpiry(t *testing.T) {
	cache := newNDPWarmCache(40*time.Millisecond, 8)
	ip := net.ParseIP("2001:db8::1")
	if cache.Seen(ip) {
		t.Fatal("new cache must not report a cold IP as warm")
	}
	cache.Mark(ip)
	if !cache.Seen(ip) {
		t.Fatal("expected IP to be warm after Mark")
	}
	time.Sleep(50 * time.Millisecond)
	if cache.Seen(ip) {
		t.Fatal("expected warm entry to expire")
	}
}

func TestNDPWarmCacheEvictsWhenFull(t *testing.T) {
	cache := newNDPWarmCache(time.Hour, 2)
	cache.Mark(net.ParseIP("2001:db8::1"))
	cache.Mark(net.ParseIP("2001:db8::2"))
	cache.Mark(net.ParseIP("2001:db8::3"))
	if cache.Seen(net.ParseIP("2001:db8::1")) && cache.Seen(net.ParseIP("2001:db8::2")) && cache.Seen(net.ParseIP("2001:db8::3")) {
		t.Fatal("cache must drop entries once it hits maxSize")
	}
	if !cache.Seen(net.ParseIP("2001:db8::3")) {
		t.Fatal("most recently marked IP should remain after reset")
	}
}

func TestEncodeNeighborAdvertisement(t *testing.T) {
	target := net.ParseIP("2001:db8::1")
	packet := encodeNeighborAdvertisement(target, nil)
	if len(packet) != 24 {
		t.Fatalf("packet len=%d want 24", len(packet))
	}
	if packet[0] != icmpv6NeighborAdvertisement {
		t.Fatalf("type=%d", packet[0])
	}
	if packet[4] != ndpFlagOverride {
		t.Fatalf("flags=%#x", packet[4])
	}
	if !bytes.Equal(packet[8:24], target.To16()) {
		t.Fatalf("target mismatch %x", packet[8:24])
	}

	mac := net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	withMAC := encodeNeighborAdvertisement(target, mac)
	if len(withMAC) != 32 {
		t.Fatalf("with MAC len=%d want 32", len(withMAC))
	}
	if withMAC[24] != ndpOptionTargetLLA || withMAC[25] != 1 {
		t.Fatalf("option header %x %x", withMAC[24], withMAC[25])
	}
	if !bytes.Equal(withMAC[26:32], mac) {
		t.Fatalf("MAC mismatch %x", withMAC[26:32])
	}
}

func TestParseDefaultIPv6RouteLine(t *testing.T) {
	line := "00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000000 00000000 00000001 00000000 he-ipv6"
	gw, iface, ok := parseDefaultIPv6RouteLine(line)
	if !ok {
		t.Fatal("expected default route")
	}
	if iface != "he-ipv6" {
		t.Fatalf("iface=%s", iface)
	}
	if gw.String() != "fe80::1" {
		t.Fatalf("gw=%s", gw)
	}

	if _, _, ok := parseDefaultIPv6RouteLine("20010db8000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000000 00000000 00000001 00000000 eth0"); ok {
		t.Fatal("non-default route must be ignored")
	}
}

func TestIsRetryableDialError(t *testing.T) {
	if !isRetryableDialError(timeoutError{}) {
		t.Fatal("timeout should retry")
	}
	if !isRetryableDialError(syscall.ECONNRESET) {
		t.Fatal("reset should retry")
	}
	if isRetryableDialError(context.Canceled) {
		t.Fatal("canceled must not retry")
	}
	if isRetryableDialError(errors.New("no such host")) {
		t.Fatal("DNS errors must not retry")
	}
}

func TestIPv6OpenerRetriesColdTimeout(t *testing.T) {
	var calls int
	var primed bool
	exitIP := net.ParseIP("2001:db8::aa")
	opener := &ipv6Opener{
		cache: newNDPWarmCache(time.Hour, 8),
		prime: func(exit, dest net.IP) {
			primed = true
			if !exit.Equal(exitIP) {
				t.Fatalf("prime exit=%s", exit)
			}
			if dest.String() != "2001:db8::1" {
				t.Fatalf("prime dest=%s", dest)
			}
		},
		alreadyDead: func(net.Conn) bool { return false },
		dial: func(ctx context.Context, target string, ip net.IP, timeout time.Duration) (net.Conn, error) {
			calls++
			switch calls {
			case 1:
				if timeout != 3*time.Second {
					t.Fatalf("cold timeout=%s", timeout)
				}
				return nil, timeoutError{}
			case 2, 3:
				if timeout != 15*time.Second {
					t.Fatalf("call %d timeout=%s want 15s", calls, timeout)
				}
				return stubConn{local: &net.TCPAddr{IP: ip}}, nil
			default:
				t.Fatalf("unexpected dial call %d", calls)
				return nil, errors.New("too many dials")
			}
		},
		coldTimeout: 3 * time.Second,
		warmTimeout: 15 * time.Second,
	}

	conn, err := opener.open(context.Background(), "[2001:db8::1]:443", exitIP)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if !primed {
		t.Fatal("expected NDP prime on cold IP")
	}
	if calls != 2 {
		t.Fatalf("calls=%d want 2", calls)
	}

	primed = false
	if _, err := opener.open(context.Background(), "[2001:db8::1]:443", exitIP); err != nil {
		t.Fatal(err)
	}
	if primed {
		t.Fatal("warm IP must not be primed again")
	}
	if calls != 3 {
		t.Fatalf("warm dial total calls=%d want 3", calls)
	}
}

func TestIPv6OpenerRetriesDeadSocket(t *testing.T) {
	var calls int
	opener := &ipv6Opener{
		cache: newNDPWarmCache(time.Hour, 8),
		prime: func(net.IP, net.IP) {},
		alreadyDead: func(net.Conn) bool {
			return calls == 1
		},
		dial: func(ctx context.Context, target string, ip net.IP, timeout time.Duration) (net.Conn, error) {
			calls++
			return stubConn{local: &net.TCPAddr{IP: ip}}, nil
		},
		coldTimeout: 3 * time.Second,
		warmTimeout: 15 * time.Second,
	}
	if _, err := opener.open(context.Background(), "[2001:db8::1]:443", net.ParseIP("2001:db8::bb")); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d want 2", calls)
	}
}

func TestIPv6OpenerSkipsRetryWhenContextCanceled(t *testing.T) {
	var calls int
	ctx, cancel := context.WithCancel(context.Background())
	opener := &ipv6Opener{
		cache:       newNDPWarmCache(time.Hour, 8),
		prime:       func(net.IP, net.IP) {},
		alreadyDead: func(net.Conn) bool { return false },
		dial: func(ctx context.Context, target string, ip net.IP, timeout time.Duration) (net.Conn, error) {
			calls++
			cancel()
			return nil, timeoutError{}
		},
		coldTimeout: 3 * time.Second,
		warmTimeout: 15 * time.Second,
	}
	_, err := opener.open(ctx, "[2001:db8::1]:443", net.ParseIP("2001:db8::cc"))
	if err == nil {
		t.Fatal("expected dial error")
	}
	if calls != 1 {
		t.Fatalf("canceled context retried: calls=%d", calls)
	}
}

func TestIPFromDialAddr(t *testing.T) {
	ip := ipFromDialAddr("[2001:db8::1]:443")
	if ip.String() != "2001:db8::1" {
		t.Fatalf("got %s", ip)
	}
}
