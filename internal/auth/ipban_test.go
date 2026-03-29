package auth

import (
	"testing"
	"time"
)

func TestIPBanRecordAndBan(t *testing.T) {
	ban := NewIPBan(3, 5*time.Minute)

	ip := "192.168.1.1:12345"
	for i := 0; i < 2; i++ {
		ban.RecordFailure(ip)
	}
	if ban.IsBanned(ip) {
		t.Fatal("should not be banned after 2 failures")
	}

	ban.RecordFailure(ip)
	if !ban.IsBanned("192.168.1.1") {
		t.Fatal("should be banned after 3 failures")
	}
}

func TestIPBanUnban(t *testing.T) {
	ban := NewIPBan(2, 5*time.Minute)

	ban.RecordFailure("10.0.0.1")
	ban.RecordFailure("10.0.0.1")
	if !ban.IsBanned("10.0.0.1") {
		t.Fatal("should be banned")
	}

	ban.Unban("10.0.0.1")
	if ban.IsBanned("10.0.0.1") {
		t.Fatal("should not be banned after unban")
	}
}

func TestIPBanBannedList(t *testing.T) {
	ban := NewIPBan(1, 5*time.Minute)

	ban.RecordFailure("1.1.1.1")
	ban.RecordFailure("2.2.2.2")

	list := ban.BannedList()
	if len(list) != 2 {
		t.Fatalf("expected 2 banned IPs, got %d", len(list))
	}
}

func TestIPBanWindowExpiry(t *testing.T) {
	ban := NewIPBan(3, 100*time.Millisecond)

	ban.RecordFailure("3.3.3.3")
	ban.RecordFailure("3.3.3.3")
	time.Sleep(150 * time.Millisecond)
	// Old failures should be pruned, so this is only failure #1 in the new window
	ban.RecordFailure("3.3.3.3")
	if ban.IsBanned("3.3.3.3") {
		t.Fatal("old failures should have expired, should not be banned")
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"192.168.1.1:8080", "192.168.1.1"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1", "10.0.0.1"},
	}
	for _, tt := range tests {
		got := extractIP(tt.input)
		if got != tt.want {
			t.Errorf("extractIP(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
