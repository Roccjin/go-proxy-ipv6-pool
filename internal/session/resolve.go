package session

import (
	"context"
	"fmt"
	"net"
	"time"

	"ipv6-proxy/internal/credential"
	"ipv6-proxy/internal/ipgen"
	"ipv6-proxy/internal/store"
)

type PrefixSource interface {
	EnabledPrefixes() []ipgen.Prefix
}

type Resolver struct {
	Store    *store.Store
	Prefixes PrefixSource
}

func (r *Resolver) Resolve(ctx context.Context, p credential.Parsed) (net.IP, error) {
	prefixes := r.Prefixes.EnabledPrefixes()
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("no enabled IPv6 prefixes")
	}
	if p.Mode != credential.ModeSticky || p.SID == "" {
		ip, _, err := ipgen.RandomFrom(prefixes)
		return ip, err
	}
	cand, pfx, err := ipgen.RandomFrom(prefixes)
	if err != nil {
		return nil, err
	}
	ttl := time.Duration(p.TTLMinutes) * time.Minute
	got, err := r.Store.GetOrCreateSticky(ctx, p.Account, p.SID, cand.String(), pfx.String(), ttl, p.TTLMinutes)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(got)
	if ip == nil {
		return nil, fmt.Errorf("invalid sticky ip %q", got)
	}
	return ip, nil
}
