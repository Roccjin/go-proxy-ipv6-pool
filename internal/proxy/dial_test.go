package proxy

import (
	"errors"
	"strings"
	"testing"
)

func TestIPv4DisabledErr(t *testing.T) {
	err := ipv4DisabledErr("example.com", nil)
	if err == nil || !strings.Contains(err.Error(), "ipv4 fallback disabled") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "no IPv6 address") {
		t.Fatalf("expected no-AAAA wording, got %v", err)
	}

	inner := errors.New("i/o timeout")
	wrapped := ipv4DisabledErr("dual.example", inner)
	if !strings.Contains(wrapped.Error(), "ipv6 failed") {
		t.Fatalf("got %v", wrapped)
	}
	if !errors.Is(wrapped, inner) {
		t.Fatalf("expected wrap of inner error, got %v", wrapped)
	}
}

func TestRuntimeIPv4DefaultOff(t *testing.T) {
	rt := &Runtime{}
	if rt.ipv4Allowed() {
		t.Fatal("IPv4 fallback must default to off")
	}
	rt.AllowIPv4.Store(true)
	if !rt.ipv4Allowed() {
		t.Fatal("expected on after Store(true)")
	}
	var nilRT *Runtime
	if nilRT.ipv4Allowed() {
		t.Fatal("nil runtime must not allow IPv4")
	}
}
