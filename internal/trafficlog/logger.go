package trafficlog

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Entry represents a single proxied request log entry.
type Entry struct {
	Timestamp string `json:"timestamp"`
	User      string `json:"user"`
	ClientIP  string `json:"client_ip"`
	Domain    string `json:"domain"`
	ExitIP    string `json:"exit_ip"`
	ActualIP  string `json:"actual_ip,omitempty"` // real local addr used (differs from ExitIP on IPv4 fallback)
	ExitType  string `json:"exit_type"`           // "ipv6" or "ipv4_fallback"
	Protocol  string `json:"protocol"`            // "http" or "socks5"
	LatencyMs int64  `json:"latency_ms"`
	Success   bool   `json:"success"`
	SID       string `json:"sid,omitempty"`
	Mode      string `json:"mode,omitempty"`
	TTLMin    int    `json:"ttl_min,omitempty"`
}

// Logger stores traffic log entries in memory with a max cap, and can persist to file.
type Logger struct {
	mu      sync.RWMutex
	entries []Entry
	maxSize int
}

func New(maxSize int) *Logger {
	return &Logger{
		entries: make([]Entry, 0, 1024),
		maxSize: maxSize,
	}
}

// Record adds a new log entry.
func (l *Logger) Record(e Entry) {
	if e.Timestamp == "" {
		e.Timestamp = time.Now().Format("2006-01-02 15:04:05")
	}
	l.mu.Lock()
	l.entries = append(l.entries, e)
	// Evict oldest if over cap
	if len(l.entries) > l.maxSize {
		l.entries = l.entries[len(l.entries)-l.maxSize:]
	}
	l.mu.Unlock()
}

// Recent returns the last n entries (newest first).
func (l *Logger) Recent(n int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	total := len(l.entries)
	if n > total {
		n = total
	}
	// Return newest first
	result := make([]Entry, n)
	for i := 0; i < n; i++ {
		result[i] = l.entries[total-1-i]
	}
	return result
}

// Count returns total entries stored.
func (l *Logger) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// ExportJSON writes all entries to a file as JSON array.
func (l *Logger) ExportJSON(path string) error {
	l.mu.RLock()
	data, err := json.MarshalIndent(l.entries, "", "  ")
	l.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Clear removes all entries.
func (l *Logger) Clear() {
	l.mu.Lock()
	l.entries = l.entries[:0]
	l.mu.Unlock()
}

// All returns all entries (oldest first).
func (l *Logger) All() []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}
