# go-proxy-ipv6-pool

**中文** | [English](README.en.md)

用 Go 实现的高性能 IPv6 代理池。把 HTTP / SOCKS5 流量从隧道前缀里的 IPv6 地址送出，一条 `/64` 就能变成海量独立出口 IP。

## 功能

- **HTTP 与 SOCKS5 代理**，出口绑定 IPv6
- **即时生成 `/64` 地址** — 随机主机位，不是顺序的 `::1` / `::2`
- **用户名编码会话** — `账号_sid_XXXXXXXX_time_10:密码@主机:端口`
- **轮转或粘性** — 没有 `sid` 则每次连接换 IP；同一 `sid` 在 N 分钟内固定（Redis）
- **凭证生成器** — 批量生成无限用户名，不必预先入库
- **限流** — 按主账号滑动窗口节流
- **IP 封禁** — 认证失败多次自动封禁，支持白名单
- **流量日志** — 内存环形缓冲（最近 10,000 条）
- **管理面板** — 账号、生成器、粘性会话、端口
- **IPv4 回退** — 目标不支持 IPv6 时自动降级

## 架构

```
┌─────────────┐     ┌──────────────────────────────────────┐     ┌───────────────┐
│   客户端      │────▶│          go-proxy-ipv6-pool          │────▶│  目标站点      │
│ HTTP/SOCKS5  │     │                                      │     │   (IPv6)      │
└─────────────┘     │  ┌─────────┐ ┌────────┐ ┌─────────┐ │     └───────────────┘
                    │  │  认证    │ │IP 池   │ │ 流量    │ │
                    │  │& IP封禁 │ │& 粘性  │ │  日志   │ │
                    │  └─────────┘ └────────┘ └─────────┘ │
                    │  ┌─────────┐ ┌────────┐ ┌─────────┐ │
                    │  │  限流    │ │ 域名   │ │ 管理    │ │
                    │  │         │ │ 规则   │ │  面板   │ │
                    │  └─────────┘ └────────┘ └─────────┘ │
                    └──────────────────────────────────────┘
```

```
cmd/server/          → 入口、命令行参数
internal/credential/ → 用户名协议（sid/time/mode）
internal/ipgen/      → 在前缀内随机生成 IPv6
internal/store/      → Redis 账号与粘性会话
internal/session/    → 轮转 / 粘性解析
internal/ippool/     → 前缀元数据、本机路由、前缀探测
internal/proxy/      → HTTP 与 SOCKS5 代理
internal/auth/       → IP 封禁 / 白名单
internal/ratelimit/  → 按用户限流
internal/trafficlog/ → 请求日志
internal/admin/      → REST API + 内嵌 SPA
web/                 → React 19 + TypeScript + Vite 6
```

## 快速开始

### 环境要求

- Go 1.21+
- Redis
- Node.js 18+（仅编译前端时需要）
- 带 IPv6 隧道的 Linux 服务器（例如 [Hurricane Electric TunnelBroker](https://tunnelbroker.net)）

### 编译

```bash
# 编译前端
cd web && npm install && npm run build && cd ..

# 编译二进制
go build -o ipv6-proxy ./cmd/server

# 交叉编译 Linux
GOOS=linux GOARCH=amd64 go build -o ipv6-proxy-linux ./cmd/server
```

### 服务器准备

启动代理前，先配好 IPv6 隧道和内核：

```bash
# 1. 创建 SIT 隧道（换成你的 TunnelBroker 参数）
ip tunnel add he-ipv6 mode sit remote <TUNNEL_ENDPOINT> local <YOUR_SERVER_IP>
ip link set he-ipv6 up
ip addr add <TUNNEL_IPV6>::2/64 dev he-ipv6
ip route add ::/0 dev he-ipv6

# 2. 允许绑定前缀内任意地址
sysctl -w net.ipv6.ip_nonlocal_bind=1
ip -6 route add local <YOUR_PREFIX>::/64 dev lo

# 3. 启动 NDP 代理
ndppd -d -c /etc/ndppd.conf
```

### 运行

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

不要和 x-ui / xray 抢同一个入站端口。停掉那边的入站，或给本进程换端口。

### Docker（linux/amd64 + linux/arm64）

镜像是多架构的：**x86_64**（`linux/amd64`）和 **ARM64**（`linux/arm64`，Graviton / Apple Silicon / 树莓派 4+）。

代理必须使用宿主机网络命名空间。HE 隧道和 `ndppd` 留在宿主机；容器只跑 Redis 和本程序，并授予 `NET_ADMIN` 以便添加 `local` IPv6 路由。

在 **Linux 宿主机**上执行一次：

```bash
sysctl -w net.ipv6.ip_nonlocal_bind=1
sysctl -w net.ipv6.conf.all.forwarding=1
# 持久化到 /etc/sysctl.d/99-ipv6-proxy.conf
```

把 `docker/env.example` 复制为 `.env`，填好 `PREFIX`、`PUBLIC_HOST` 和密码。

**在 VPS 上按本机架构构建（推荐）：**

```bash
docker compose build
docker compose up -d
```

x86 机器编出来是 amd64，ARM 机器编出来是 arm64，不需要 QEMU。

**同时交叉编译两种架构并推送：**

```bash
docker buildx create --name ipv6-proxy-builder --driver docker-container --use
docker buildx inspect --bootstrap

# 仓库需要支持多架构 manifest
IMAGE=ghcr.io/you/ipv6-proxy:latest PUSH=1 ./scripts/docker-build.sh both
```

Windows（Docker Desktop）：

```powershell
.\scripts\docker-build.ps1 amd64
# 登录镜像仓库之后：
$env:IMAGE="ghcr.io/you/ipv6-proxy:latest"; $env:PUSH="1"; .\scripts\docker-build.ps1 both
```

Bake 目标：`local-amd64`、`local-arm64`、`image`（双架构，需要 `--push`）。

然后在服务器上：

```bash
IMAGE=ghcr.io/you/ipv6-proxy:latest docker compose pull
IMAGE=ghcr.io/you/ipv6-proxy:latest docker compose up -d
```

Windows / macOS 的 Docker Desktop 绑不了宿主机 HE `/64`，请在 Linux VPS 上跑 compose。

### 测试

```bash
# 轮转 — 每次连接不同 IPv6
curl -x http://caomao002:Aq112211@127.0.0.1:8080 https://ipv6.ip.sb

# 粘性 10 分钟 — 同一 sid 保持同一 IPv6
curl -x http://caomao002_sid_46916889_time_10:Aq112211@127.0.0.1:8080 https://ipv6.ip.sb

# SOCKS5 粘性
curl --socks5 caomao002_sid_46916889_time_10:Aq112211@127.0.0.1:8082 https://ipv6.ip.sb
```

用户名协议：

```text
{account}[_sid_{id}][_time_{minutes}][_mode_{rotate|sticky}]
```

- 没有 `sid` → 轮转（每次连接新出口 IP）
- 有 `sid` → 粘性；`time` 单位是分钟（1–180，默认 10）
- 密码始终是 **主账号** 密码
- 生成出来的 `sid` 用户名不会入库；第一次使用时 Redis 才写入粘性映射

## 配置

| 参数 | 默认值 | 说明 |
|------|---------|------|
| `-prefix` | `2001:db8:abcd` | IPv6 前缀，逗号分隔，建议写 CIDR |
| `-redis` | `redis://127.0.0.1:6379/0` | Redis 地址（必填） |
| `-redis-key-prefix` | `ipv6p:` | Redis key 前缀 |
| `-public-host` | （自动） | 生成凭证时显示的主机名 |
| `-default-mode` | `rotate` | 主账号默认模式 |
| `-default-ttl` | `10m` | 默认粘性时长 |
| `-max-ttl` | `180m` | 最大粘性时长 |
| `-count` | `100` | 已废弃（忽略） |
| `-sticky-ttl` | `30m` | 已废弃（忽略） |
| `-proxy-addr` | `0.0.0.0:8080` | HTTP 代理监听地址 |
| `-socks5-addr` | `0.0.0.0:8082` | SOCKS5 监听地址 |
| `-admin-addr` | `0.0.0.0:8081` | 管理面板地址 |
| `-proxy-user` | `proxy` | 默认代理用户名 |
| `-proxy-pass` | `proxy123` | 默认代理密码 |
| `-admin-user` | `admin` | 管理面板用户名 |
| `-admin-pass` | `admin123` | 管理面板密码 |
| `-data-dir` | `/etc/ipv6-proxy` | 持久化配置目录 |
| `-rate-limit` | `500` | 每用户每分钟最大请求数（`0` = 关闭） |

多前缀示例：

```bash
-prefix "2001:db8:abcd,2001:db8:ef01"
```

## 管理面板

服务启动后访问 `http://你的服务器:8081`。

**标签页：**

- **Overview** — 实时统计、前缀卡片（启用/停用/探测）
- **Accounts** — 主账号（模式、TTL、限流、启用）
- **Generator** — 批量生成 `user:pass@host:port`
- **Sessions** — Redis 里的粘性会话，可踢线换 IP
- **Banned IPs** — 自动封禁、手动封禁/解封、白名单
- **Traffic Log** — 最近 10,000 条请求
- **Ports** — 动态扩展 HTTP / SOCKS5 端口
- **Domain Rules** — 遗留功能，不走代理热路径

## 工作原理

1. **TunnelBroker** 通过 6in4（SIT）隧道提供一条 `/64` IPv6 前缀（2^64 个地址）
2. `ip_nonlocal_bind` 加上本机 local 路由，进程才能绑定前缀内任意地址
3. 代理解析用户名、校验 **主账号**，然后：
   - **轮转**：在前缀主机位上 `crypto/rand`
   - **粘性**：Redis 对 `账号+sid → 出口 IP` 做 GET-or-SET，TTL 从首次使用起算
4. `IP_FREEBIND` / `IPV6_FREEBIND` 把该地址绑成 TCP 源地址
5. 粘性 TTL 到期后，同一用户名会拿到 **新的** IPv6，并开启下一窗口

## 生产建议

- 前面加 **Nginx** 做 TLS 终结，加密代理连接
- 用 **systemd** 的 `Restart=always` 管进程
- 大池子时调内核参数：
  ```bash
  sysctl -w net.ipv6.route.max_size=409600
  sysctl -w net.ipv6.neigh.default.gc_thresh3=102400
  sysctl -w net.core.default_qdisc=cake
  sysctl -w net.ipv4.tcp_congestion_control=bbr
  ```
- 上线前改掉默认账号密码

## 许可证

MIT
