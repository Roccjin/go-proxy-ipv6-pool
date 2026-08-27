#!/bin/sh
set -e

exec /usr/local/bin/ipv6-proxy \
  -prefix "${PREFIX:-2001:db8:abcd}" \
  -redis "${REDIS_URL:-redis://127.0.0.1:6379/0}" \
  -redis-key-prefix "${REDIS_KEY_PREFIX:-ipv6p:}" \
  -public-host "${PUBLIC_HOST:-}" \
  -proxy-addr "${PROXY_ADDR:-0.0.0.0:8080}" \
  -socks5-addr "${SOCKS5_ADDR:-0.0.0.0:8082}" \
  -admin-addr "${ADMIN_ADDR:-0.0.0.0:8081}" \
  -proxy-user "${PROXY_USER:-proxy}" \
  -proxy-pass "${PROXY_PASS:-proxy123}" \
  -admin-user "${ADMIN_USER:-admin}" \
  -admin-pass "${ADMIN_PASS:-admin123}" \
  -data-dir "${DATA_DIR:-/etc/ipv6-proxy}" \
  -rate-limit "${RATE_LIMIT:-500}" \
  -default-mode "${DEFAULT_MODE:-rotate}" \
  -default-ttl "${DEFAULT_TTL:-10m}" \
  -max-ttl "${MAX_TTL:-180m}" \
  "$@"
