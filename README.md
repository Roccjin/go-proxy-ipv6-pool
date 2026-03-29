# go-proxy-ipv6-pool

A high-performance IPv6 proxy pool built with Go. Routes HTTP and SOCKS5 traffic through randomly selected IPv6 exit addresses from your tunnel prefix — turning a single `/64` subnet into millions of unique exit IPs.

## Features

- **HTTP & SOCKS5 proxy** with IPv6 exit IP binding
- **Multi-prefix support** — comma-separated IPv6 prefixes, each with independent address pools
- **Sticky sessions** — consistent exit IP per client fingerprint (configurable TTL)
- **Domain-based load balancing** — per-domain round-robin or random IP rotation
- **Rate limiting** — sliding-window per-user request throttling
- **IP ban system** — auto-ban after repeated auth failures, with whitelist support
- **Traffic logging** — in-memory ring buffer (last 10,000 requests)
- **Admin panel** — embedded React SPA with real-time monitoring
- **Single binary** — zero Go dependencies, frontend embedded via `go:embed`
- **IPv4 fallback** — graceful degradation when target doesn't support IPv6

## Architecture

```
┌─────────────┐     ┌──────────────────────────────────────┐     ┌───────────────┐
│   Client     │────▶│          go-proxy-ipv6-pool          │────▶│  Target Site  │
│ HTTP/SOCKS5  │     │                                      │     │   (IPv6)      │
└─────────────┘     │  ┌─────────┐ ┌────────┐ ┌─────────┐ │     └───────────────┘
                    │  │  Auth   │ │IP Pool │ │ Traffic │ │
                    │  │& IPBan  │ │& Sticky│ │   Log   │ │
                    │  └─────────┘ └────────┘ └─────────┘ │
                    │  ┌─────────┐ ┌────────┐ ┌─────────┐ │
                    │  │  Rate   │ │ Domain │ │  Admin  │ │
                    │  │ Limiter │ │ Rules  │ │  Panel  │ │
                    │  └─────────┘ └────────┘ └─────────┘ │
                    └──────────────────────────────────────┘
```

```
cmd/server/          → Entry point, CLI flags
internal/ippool/     → IPv6 address pool, sticky sessions, domain rules
internal/proxy/      → HTTP & SOCKS5 proxy servers
internal/auth/       → User auth, IP ban/whitelist
internal/ratelimit/  → Per-user rate limiting
internal/trafficlog/ → Request logging
internal/admin/      → REST API + embedded SPA
web/                 → React 19 + TypeScript + Vite 6
```

## Quick Start

### Prerequisites

- Go 1.21+
- Node.js 18+ (for frontend build)
- Linux server with IPv6 tunnel (e.g., [Hurricane Electric TunnelBroker](https://tunnelbroker.net))

### Build

```bash
# Build frontend
cd web && npm install && npm run build && cd ..

# Build binary
go build -o ipv6-proxy ./cmd/server

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o ipv6-proxy-linux ./cmd/server
```

### Server Setup

Before running the proxy, configure your IPv6 tunnel and kernel:

```bash
# 1. Create SIT tunnel (replace with your TunnelBroker values)
ip tunnel add he-ipv6 mode sit remote <TUNNEL_ENDPOINT> local <YOUR_SERVER_IP>
ip link set he-ipv6 up
ip addr add <TUNNEL_IPV6>::2/64 dev he-ipv6
ip route add ::/0 dev he-ipv6

# 2. Allow binding to any address in the prefix
sysctl -w net.ipv6.ip_nonlocal_bind=1
ip -6 route add local <YOUR_PREFIX>::/64 dev lo

# 3. Start NDP proxy
ndppd -d -c /etc/ndppd.conf
```

### Run

```bash
./ipv6-proxy \
  -prefix "2001:db8:abcd" \
  -count 100 \
  -proxy-addr 0.0.0.0:8080 \
  -socks5-addr 0.0.0.0:8082 \
  -admin-addr 0.0.0.0:8081 \
  -proxy-user myuser \
  -proxy-pass mypass \
  -admin-user admin \
  -admin-pass secret
```

### Test

```bash
# HTTP proxy
curl -x http://myuser:mypass@localhost:8080 https://api64.ipify.org

# SOCKS5 proxy
curl --socks5 myuser:mypass@localhost:8082 https://api64.ipify.org
```

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-prefix` | `2001:db8:abcd` | IPv6 prefix(es), comma-separated |
| `-count` | `100` | Number of addresses per prefix |
| `-sticky-ttl` | `30m` | Sticky session TTL (`0` = forever) |
| `-proxy-addr` | `0.0.0.0:8080` | HTTP proxy listen address |
| `-socks5-addr` | `0.0.0.0:8082` | SOCKS5 proxy listen address |
| `-admin-addr` | `0.0.0.0:8081` | Admin panel listen address |
| `-proxy-user` | `proxy` | Proxy username |
| `-proxy-pass` | `proxy123` | Proxy password |
| `-admin-user` | `admin` | Admin panel username |
| `-admin-pass` | `admin123` | Admin panel password |
| `-data-dir` | `/etc/ipv6-proxy` | Persistent config directory |
| `-rate-limit` | `500` | Max requests/min per user (`0` = disabled) |

Multi-prefix example:

```bash
-prefix "2001:db8:abcd,2001:db8:ef01"
```

## Admin Panel

Access the admin panel at `http://your-server:8081` after starting the service.

**Tabs:**

- **Overview** — real-time stats, prefix cards with enable/disable/test, pool expansion
- **Domain Rules** — per-domain load balancing (round-robin / random)
- **Users** — proxy user management with per-user rate limits
- **Sessions** — sticky session viewer with per-domain latency stats
- **Banned IPs** — auto-ban tracking, manual ban/unban, whitelist
- **Traffic Log** — last 10,000 requests with export
- **Ports** — dynamic port expansion for HTTP & SOCKS5

## How It Works

1. **TunnelBroker** provides a `/64` IPv6 prefix (2^64 addresses) via a 6in4 (SIT) tunnel
2. `ip_nonlocal_bind` + local route allows the process to bind to any address in the prefix
3. Each proxy request exits through a **randomly selected IPv6 address** from the pool
4. `IP_FREEBIND` and `IP_TRANSPARENT` socket options handle the source IP binding at kernel level
5. **Sticky sessions** use SHA256(fingerprint) to map clients to consistent exit IPs
6. **Domain rules** override sticky behavior with round-robin or random rotation per domain

## Production Tips

- Put **Nginx** in front with TLS termination for encrypted proxy connections
- Use **systemd** with `Restart=always` for process management
- Tune kernel parameters for large pools:
  ```bash
  sysctl -w net.ipv6.route.max_size=409600
  sysctl -w net.ipv6.neigh.default.gc_thresh3=102400
  sysctl -w net.core.default_qdisc=cake
  sysctl -w net.ipv4.tcp_congestion_control=bbr
  ```
- Change default credentials before deploying

## License

MIT
