package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"strings"
	"time"

	"ipv6-proxy/internal/admin"
	"ipv6-proxy/internal/auth"
	"ipv6-proxy/internal/credential"
	"ipv6-proxy/internal/ippool"
	"ipv6-proxy/internal/proxy"
	"ipv6-proxy/internal/ratelimit"
	"ipv6-proxy/internal/session"
	"ipv6-proxy/internal/store"
	"ipv6-proxy/internal/trafficlog"
	"ipv6-proxy/web"
)

func main() {
	prefix := flag.String("prefix", "2001:db8:abcd", "IPv6 prefix(es), comma-separated for multi-prefix")
	count := flag.Int("count", 100, "Deprecated: sequential pool size (exit IPs are generated on the fly)")
	stickyTTL := flag.Duration("sticky-ttl", 30*time.Minute, "Deprecated: use username time= or -default-ttl")
	proxyAddr := flag.String("proxy-addr", "0.0.0.0:8080", "Proxy listen address")
	socks5Addr := flag.String("socks5-addr", "0.0.0.0:8082", "SOCKS5 listen address")
	adminAddr := flag.String("admin-addr", "0.0.0.0:8081", "Admin panel address")
	proxyUser := flag.String("proxy-user", "proxy", "Default proxy username")
	proxyPass := flag.String("proxy-pass", "proxy123", "Default proxy password")
	adminUser := flag.String("admin-user", "admin", "Admin username")
	adminPass := flag.String("admin-pass", "admin123", "Admin password")
	dataDir := flag.String("data-dir", "/etc/ipv6-proxy", "Data directory for persistent config")
	rateLimit := flag.Int("rate-limit", 500, "Max requests per user per minute (0=disabled)")
	redisURL := flag.String("redis", "redis://127.0.0.1:6379/0", "Redis URL (required)")
	redisPrefix := flag.String("redis-key-prefix", "ipv6p:", "Redis key prefix")
	publicHost := flag.String("public-host", "", "Host shown in generated credentials")
	defaultMode := flag.String("default-mode", "rotate", "Default account mode: rotate or sticky")
	defaultTTL := flag.Duration("default-ttl", 10*time.Minute, "Default sticky TTL")
	maxTTL := flag.Duration("max-ttl", 180*time.Minute, "Max sticky TTL")
	flag.Parse()

	_ = count
	_ = stickyTTL
	log.Printf("Exit IPs are generated on the fly from prefix host bits; -count and -sticky-ttl are ignored")

	pool := ippool.New(*prefix, 1, 0)
	log.Printf("Prefixes: %d enabled for on-the-fly IPv6", len(pool.EnabledPrefixes()))
	for _, pi := range pool.Prefixes() {
		log.Printf("  Prefix %s /%d — max %s", pi.Prefix, pi.Bits, pi.MaxCapacity)
	}

	if err := pool.SetupLocalRoutes(); err != nil {
		log.Printf("Warning: failed to setup local routes (need root): %v", err)
	}

	rulesFile := *dataDir + "/domain-rules.json"
	if err := pool.LoadDomainRules(rulesFile); err != nil {
		log.Printf("No saved domain rules (legacy, unused on proxy hot path): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	st, rdb, err := store.Open(ctx, *redisURL, *redisPrefix)
	cancel()
	if err != nil {
		log.Fatalf("Redis: %v", err)
	}
	defer rdb.Close()
	log.Printf("Redis connected (%s)", *redisURL)

	mode := credential.Mode(strings.ToLower(*defaultMode))
	if mode != credential.ModeSticky {
		mode = credential.ModeRotate
	}
	defTTLMin := int(defaultTTL.Minutes())
	maxTTLMin := int(maxTTL.Minutes())
	if err := st.EnsureAccount(context.Background(), *proxyUser, *proxyPass, store.Account{
		Enabled:     true,
		DefaultMode: mode,
		DefaultTTL:  defTTLMin,
		MaxTTL:      maxTTLMin,
		RateLimit:   *rateLimit,
	}); err != nil {
		log.Fatalf("default account: %v", err)
	}
	log.Printf("Proxy account %q ready (mode=%s default_ttl=%dm)", *proxyUser, mode, defTTLMin)

	ipBan := auth.NewIPBan(10, 5*time.Minute)

	var limiter *ratelimit.Limiter
	if *rateLimit > 0 {
		limiter = ratelimit.New(*rateLimit, time.Minute)
		log.Printf("Rate limit: %d req/min per user (0 on account = unlimited)", *rateLimit)
		if *rateLimit > 0 {
			limiter.SetUserLimit(*proxyUser, *rateLimit)
		}
	}

	tlog := trafficlog.New(10000)
	resolver := &session.Resolver{Store: st, Prefixes: pool}
	rt := &proxy.Runtime{
		Store:    st,
		Resolver: resolver,
		IPBan:    ipBan,
		Limiter:  limiter,
		TLog:     tlog,
	}

	proxyServer := proxy.NewServer(*proxyAddr, rt)
	go func() {
		if err := proxyServer.Start(); err != nil {
			log.Fatalf("Proxy: %v", err)
		}
	}()

	socks5Server := proxy.NewSocks5Server(*socks5Addr, rt)
	go func() {
		if err := socks5Server.Start(); err != nil {
			log.Fatalf("SOCKS5: %v", err)
		}
	}()

	portMgr := proxy.NewPortManager(rt, proxyServer, socks5Server)

	staticFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		log.Printf("Warning: embedded SPA not available, using fallback dashboard: %v", err)
		staticFS = nil
	}

	host := strings.TrimSpace(*publicHost)
	if host == "" {
		host = guessPublicHost()
	}

	adminHandler := admin.New(admin.Deps{
		Pool:           pool,
		Store:          st,
		IPBan:          ipBan,
		Limiter:        limiter,
		TLog:           tlog,
		PortMgr:        portMgr,
		AdminUser:      *adminUser,
		AdminPass:      *adminPass,
		GetStats:       proxyServer.GetStats,
		GetSocks5Stats: socks5Server.GetStats,
		StaticFS:       staticFS,
		RulesFile:      rulesFile,
		PublicHost:     host,
		HTTPAddr:       *proxyAddr,
		SOCKS5Addr:     *socks5Addr,
	})
	if err := adminHandler.Start(*adminAddr); err != nil {
		log.Fatalf("Admin: %v", err)
	}
}

func guessPublicHost() string {
	ifaces, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range ifaces {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			return v4.String()
		}
	}
	return "127.0.0.1"
}
