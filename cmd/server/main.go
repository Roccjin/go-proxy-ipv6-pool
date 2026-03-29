package main

import (
	"flag"
	"io/fs"
	"log"
	"time"

	"ipv6-proxy/internal/admin"
	"ipv6-proxy/internal/auth"
	"ipv6-proxy/internal/ippool"
	"ipv6-proxy/internal/proxy"
	"ipv6-proxy/internal/ratelimit"
	"ipv6-proxy/internal/trafficlog"
	"ipv6-proxy/web"
)

func main() {
	prefix := flag.String("prefix", "2001:db8:abcd", "IPv6 prefix(es), comma-separated for multi-prefix")
	count := flag.Int("count", 100, "Number of IPv6 addresses per prefix")
	stickyTTL := flag.Duration("sticky-ttl", 30*time.Minute, "Sticky session TTL (0=forever)")
	proxyAddr := flag.String("proxy-addr", "0.0.0.0:8080", "Proxy listen address")
	socks5Addr := flag.String("socks5-addr", "0.0.0.0:8082", "SOCKS5 listen address")
	adminAddr := flag.String("admin-addr", "0.0.0.0:8081", "Admin panel address")
	proxyUser := flag.String("proxy-user", "proxy", "Default proxy username")
	proxyPass := flag.String("proxy-pass", "proxy123", "Default proxy password")
	adminUser := flag.String("admin-user", "admin", "Admin username")
	adminPass := flag.String("admin-pass", "admin123", "Admin password")
	dataDir := flag.String("data-dir", "/etc/ipv6-proxy", "Data directory for persistent config")
	rateLimit := flag.Int("rate-limit", 500, "Max requests per user per minute (0=disabled)")
	flag.Parse()

	pool := ippool.New(*prefix, *count, *stickyTTL)
	log.Printf("Pool: %d addrs from %d prefix(es), sticky TTL=%v", pool.Count(), len(pool.Prefixes()), *stickyTTL)
	for _, pi := range pool.Prefixes() {
		log.Printf("  Prefix %s /%d — %d addrs, max %s", pi.Prefix, pi.Bits, pi.Count, pi.MaxCapacity)
	}

	// Setup local routes so kernel accepts return packets for all pool addresses
	if err := pool.SetupLocalRoutes(); err != nil {
		log.Printf("Warning: failed to setup local routes (need root): %v", err)
	}

	// Load persisted domain rules
	rulesFile := *dataDir + "/domain-rules.json"
	if err := pool.LoadDomainRules(rulesFile); err != nil {
		log.Printf("No saved domain rules (will create on first add): %v", err)
	} else {
		log.Printf("Loaded %d domain rules from %s", len(pool.DomainRules()), rulesFile)
	}

	authMgr := auth.New()
	authMgr.AddUser(*proxyUser, *proxyPass)

	ipBan := auth.NewIPBan(10, 5*time.Minute)

	// Rate limiter
	var limiter *ratelimit.Limiter
	if *rateLimit > 0 {
		limiter = ratelimit.New(*rateLimit, time.Minute)
		log.Printf("Rate limit: %d req/min per user", *rateLimit)
	}

	// Traffic logger (keep last 10000 entries in memory)
	tlog := trafficlog.New(10000)

	proxyServer := proxy.NewServer(*proxyAddr, pool, authMgr, ipBan, limiter, tlog)
	go func() {
		if err := proxyServer.Start(); err != nil {
			log.Fatalf("Proxy: %v", err)
		}
	}()

	socks5Server := proxy.NewSocks5Server(*socks5Addr, pool, authMgr, ipBan, limiter, tlog)
	go func() {
		if err := socks5Server.Start(); err != nil {
			log.Fatalf("SOCKS5: %v", err)
		}
	}()

	portMgr := proxy.NewPortManager(pool, authMgr, ipBan, limiter, tlog, proxyServer, socks5Server)

	staticFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		log.Printf("Warning: embedded SPA not available, using fallback dashboard: %v", err)
		staticFS = nil
	}

	adminHandler := admin.New(pool, authMgr, ipBan, limiter, tlog, portMgr, *adminUser, *adminPass, proxyServer.GetStats, socks5Server.GetStats, staticFS, rulesFile)
	if err := adminHandler.Start(*adminAddr); err != nil {
		log.Fatalf("Admin: %v", err)
	}
}
