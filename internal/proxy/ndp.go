package proxy

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	icmpv6NeighborAdvertisement = 136
	ndpFlagOverride             = 0x20
	ndpOptionTargetLLA          = 2
)

type ndpWarmCache struct {
	mu       sync.Mutex
	entries  map[string]time.Time
	inFlight map[string]*ndpConfirmWait
	ttl      time.Duration
	maxSize  int
}

type ndpConfirmWait struct {
	done chan struct{}
}

func newNDPWarmCache(ttl time.Duration, maxSize int) *ndpWarmCache {
	if ttl <= 0 {
		ttl = ndpWarmTTL
	}
	if maxSize <= 0 {
		maxSize = ndpWarmCacheMax
	}
	return &ndpWarmCache{
		entries: make(map[string]time.Time),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

func (cache *ndpWarmCache) Seen(ip net.IP) bool {
	if cache == nil || ip == nil {
		return false
	}
	key := ip.String()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.warmLocked(key)
}

func (cache *ndpWarmCache) Mark(ip net.IP) {
	if cache == nil || ip == nil {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.markLocked(ip)
}

func (cache *ndpWarmCache) warmLocked(key string) bool {
	seenAt, ok := cache.entries[key]
	if !ok {
		return false
	}
	if time.Since(seenAt) > cache.ttl {
		delete(cache.entries, key)
		return false
	}
	return true
}

func (cache *ndpWarmCache) markLocked(ip net.IP) {
	if len(cache.entries) >= cache.maxSize {
		cache.evictExpiredLocked()
		if len(cache.entries) >= cache.maxSize {
			cache.entries = make(map[string]time.Time, cache.maxSize/2)
		}
	}
	cache.entries[ip.String()] = time.Now()
}

// beginConfirm serializes first-use warmup for one exit IP.
// warm: already confirmed. leader: this caller should run warmup.
func (cache *ndpWarmCache) beginConfirm(ip net.IP) (warm bool, wait <-chan struct{}, leader bool) {
	if cache == nil || ip == nil {
		return false, nil, true
	}
	key := ip.String()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.warmLocked(key) {
		return true, nil, false
	}
	if cache.inFlight == nil {
		cache.inFlight = make(map[string]*ndpConfirmWait)
	}
	if waiter, ok := cache.inFlight[key]; ok {
		return false, waiter.done, false
	}
	waiter := &ndpConfirmWait{done: make(chan struct{})}
	cache.inFlight[key] = waiter
	return false, waiter.done, true
}

func (cache *ndpWarmCache) finishConfirm(ip net.IP, succeeded bool) {
	if cache == nil || ip == nil {
		return
	}
	key := ip.String()
	cache.mu.Lock()
	waiter := cache.inFlight[key]
	delete(cache.inFlight, key)
	if succeeded {
		cache.markLocked(ip)
	}
	cache.mu.Unlock()
	if waiter != nil {
		close(waiter.done)
	}
}

func (cache *ndpWarmCache) evictExpiredLocked() {
	now := time.Now()
	for key, seenAt := range cache.entries {
		if now.Sub(seenAt) > cache.ttl {
			delete(cache.entries, key)
		}
	}
}

func isRetryableDialError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ETIMEDOUT,
			syscall.ENETUNREACH, syscall.EHOSTUNREACH:
			return true
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host")
}

// encodeNeighborAdvertisement builds an ICMPv6 Neighbor Advertisement
// (RFC 4861 §4.4). Checksum is left zero for the kernel to fill.
func encodeNeighborAdvertisement(target net.IP, hardwareAddr net.HardwareAddr) []byte {
	ip16 := target.To16()
	if ip16 == nil {
		return nil
	}
	optionLength := 0
	if len(hardwareAddr) > 0 {
		optionLength = ((2 + len(hardwareAddr) + 7) / 8) * 8
	}
	packet := make([]byte, 8+16+optionLength)
	packet[0] = icmpv6NeighborAdvertisement
	packet[4] = ndpFlagOverride
	copy(packet[8:24], ip16)
	if optionLength > 0 {
		packet[24] = ndpOptionTargetLLA
		packet[25] = byte(optionLength / 8)
		copy(packet[26:], hardwareAddr)
	}
	return packet
}

func parseDefaultIPv6RouteLine(line string) (gateway net.IP, ifaceName string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return nil, "", false
	}
	if fields[0] != "00000000000000000000000000000000" {
		return nil, "", false
	}
	if fields[1] != "00" && fields[1] != "0" {
		return nil, "", false
	}
	ifaceName = fields[len(fields)-1]
	if ifaceName == "" || ifaceName == "lo" {
		return nil, "", false
	}
	gateway = parseProcIPv6(fields[4])
	return gateway, ifaceName, true
}

func parseProcIPv6(hexAddr string) net.IP {
	if len(hexAddr) != 32 {
		return nil
	}
	raw, err := hex.DecodeString(hexAddr)
	if err != nil || len(raw) != 16 {
		return nil
	}
	ip := net.IP(raw)
	if ip.IsUnspecified() {
		return nil
	}
	return ip
}
