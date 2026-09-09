# go-proxy-ipv6-pool

[中文](README.md) | **English**

A high-performance IPv6 proxy pool built with Go. Routes HTTP and SOCKS5 traffic through randomly selected IPv6 exit addresses from your tunnel prefix — turning a single `/64` subnet into millions of unique exit IPs.

## Features

- **HTTP & SOCKS5 proxy** with IPv6 exit IP binding
- **On-the-fly `/64` exits** — random host bits, not sequential `::1`/`::2`
- **Username-encoded sessions** — `account_sid_XXXXXXXX_time_10:pass@host:port`
- **Rotate or sticky** — no `sid` rotates every connection; same `sid` keeps one IP for N minutes (Redis)
- **Credential generator** — batch-create unlimited usernames without storing them
- **Rate limiting** — sliding-window per master account
- **IP ban system** — auto-ban after repeated auth failures, with whitelist support
- **Traffic logging** — in-memory ring buffer (last 10,000 requests)
- **Admin panel** — accounts, generator, sticky sessions, ports
- **IPv6-only by default** — IPv4 fallback leaks the server public IPv4, so it is off; enable with `-ipv4-fallback` or the admin toggle

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
internal/credential/ → Username protocol (sid/time/mode)
internal/ipgen/      → Random IPv6 inside a prefix
internal/store/      → Redis accounts + sticky sessions
internal/session/    → Rotate vs sticky resolver
internal/ippool/     → Prefix metadata, local routes, prefix test
internal/proxy/      → HTTP & SOCKS5 proxy servers
internal/auth/       → IP ban/whitelist
internal/ratelimit/  → Per-user rate limiting
internal/trafficlog/ → Request logging
internal/admin/      → REST API + embedded SPA
web/                 → React 19 + TypeScript + Vite 6
```

## Quick Start

### Prerequisites

- Go 1.21+
- Redis
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
  -prefix "2001:470:24:692::/64" \
  -redis redis://127.0.0.1:6379/0 \
  -public-host 45.129.9.171 \
  -proxy-addr 0.0.0.0:8080 \
  -socks5-addr 0.0.0.0:8082 \
  -admin-addr 0.0.0.0:8081 \
  -proxy-user caomao002 \
  -proxy-pass Aq112211 \
  -admin-user admin \
  -admin-pass secret
```

Do not bind the same inbound port as x-ui/xray. Stop that inbound or pick another port.

### Docker (linux/amd64 + linux/arm64)

Images are multi-arch: **x86_64** (`linux/amd64`) and **ARM64** (`linux/arm64`, Graviton / Apple Silicon / Pi 4+).

The proxy must share the host network namespace. The HE tunnel and `ndppd` stay on the host; the container only runs Redis + this binary, with `NET_ADMIN` so it can add `local` IPv6 routes.

On the **Linux host** (once):

```bash
sysctl -w net.ipv6.ip_nonlocal_bind=1
sysctl -w net.ipv6.conf.all.forwarding=1
# persist in /etc/sysctl.d/99-ipv6-proxy.conf
```

Copy `docker/env.example` to `.env` and set `PREFIX`, `PUBLIC_HOST`, passwords.

**On the VPS (native arch, recommended):**

```bash
docker compose build
docker compose up -d
```

`docker compose build` on an x86 machine produces amd64; on ARM it produces arm64. No QEMU needed.

**Cross-build both architectures and push:**

```bash
docker buildx create --name ipv6-proxy-builder --driver docker-container --use
docker buildx inspect --bootstrap

# registry must allow multi-arch manifests
IMAGE=ghcr.io/you/ipv6-proxy:latest PUSH=1 ./scripts/docker-build.sh both
```

Windows (Docker Desktop):

```powershell
.\scripts\docker-build.ps1 amd64
# or, after login to a registry:
$env:IMAGE="ghcr.io/you/ipv6-proxy:latest"; $env:PUSH="1"; .\scripts\docker-build.ps1 both
```

Bake targets: `local-amd64`, `local-arm64`, `image` (both, needs `--push`).

Then on the server:

```bash
IMAGE=ghcr.io/you/ipv6-proxy:latest docker compose pull
IMAGE=ghcr.io/you/ipv6-proxy:latest docker compose up -d
```

Docker Desktop on Windows/macOS cannot bind the host HE `/64`; run the compose stack on the Linux VPS.

### Test

```bash
# Rotate — each connection a new IPv6
curl -x http://caomao002:Aq112211@127.0.0.1:8080 https://ipv6.ip.sb

# Sticky 10 minutes — same sid keeps the same IPv6
curl -x http://caomao002_sid_46916889_time_10:Aq112211@127.0.0.1:8080 https://ipv6.ip.sb

# SOCKS5 sticky
curl --socks5 caomao002_sid_46916889_time_10:Aq112211@127.0.0.1:8082 https://ipv6.ip.sb
```

Username protocol:

```text
{account}[_sid_{id}][_time_{minutes}][_mode_{rotate|sticky}]
```

- No `sid` → rotate (new exit IP per connection)
- `sid` present → sticky; `time` is minutes (1–180, default 10)
- Password is always the **master account** password
- Generated `sid` usernames are not stored; Redis only records a sticky mapping on first use

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-prefix` | `2001:db8:abcd` | IPv6 prefix(es), comma-separated CIDR preferred |
| `-redis` | `redis://127.0.0.1:6379/0` | Redis URL (required) |
| `-redis-key-prefix` | `ipv6p:` | Redis key prefix |
| `-public-host` | (auto) | Host shown in generated credentials |
| `-default-mode` | `rotate` | Default master-account mode |
| `-default-ttl` | `10m` | Default sticky duration |
| `-max-ttl` | `180m` | Max sticky duration |
| `-count` | `100` | Deprecated (ignored) |
| `-sticky-ttl` | `30m` | Deprecated (ignored) |
| `-proxy-addr` | `0.0.0.0:8080` | HTTP proxy listen address |
| `-socks5-addr` | `0.0.0.0:8082` | SOCKS5 proxy listen address |
| `-admin-addr` | `0.0.0.0:8081` | Admin panel listen address |
| `-proxy-user` | `proxy` | Proxy username |
| `-proxy-pass` | `proxy123` | Proxy password |
| `-admin-user` | `admin` | Admin panel username |
| `-admin-pass` | `admin123` | Admin panel password |
| `-data-dir` | `/etc/ipv6-proxy` | Persistent config directory |
| `-rate-limit` | `500` | Max requests/min per user (`0` = disabled) |
| `-ipv4-fallback` | `false` | Allow IPv4 fallback (leaks the server public IPv4) |

Multi-prefix example:

```bash
-prefix "2001:db8:abcd,2001:db8:ef01"
```

## Admin Panel

Access the admin panel at `http://your-server:8081` after starting the service.

**Tabs:**

- **Overview** — real-time stats, prefix cards with enable/disable/test
- **Accounts** — master accounts (mode, TTL, rate limit, enable)
- **Generator** — batch `user:pass@host:port` lines
- **Sessions** — live Redis sticky sessions, kick to force a new IP
- **Banned IPs** — auto-ban tracking, manual ban/unban, whitelist
- **Traffic Log** — last 10,000 requests
- **Ports** — dynamic port expansion for HTTP & SOCKS5
- **Domain Rules** — legacy; not used on the proxy hot path

## How It Works

1. **TunnelBroker** provides a `/64` IPv6 prefix (2^64 addresses) via a 6in4 (SIT) tunnel
2. `ip_nonlocal_bind` + local route allows the process to bind to any address in the prefix
3. The proxy parses the username, authenticates the **master account**, then:
   - **rotate**: `crypto/rand` host bits inside the prefix
   - **sticky**: Redis `GET-or-SET` of `account+sid → exit IP` with a fixed TTL from first use
4. `IP_FREEBIND` / `IPV6_FREEBIND` bind that address as the TCP source
5. A new exit IP is primed with an unsolicited ICMPv6 Neighbor Advertisement; a failed first TCP dial is retried once so HTTPS does not see an NDP race
6. After sticky TTL expires, the same username gets a **new** IPv6 and a new window

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
