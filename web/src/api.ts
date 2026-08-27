const BASE = '';

async function request<T>(url: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(BASE + url, { credentials: 'same-origin', ...opts });
  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent('auth-expired'));
    throw new Error('401');
  }
  if (!res.ok) throw new Error(`${res.status}`);
  return res.json();
}

export interface PrefixInfo {
  prefix: string;
  bits: number;
  count: number;
  max_capacity: string;
  enabled: boolean;
}

export interface Overview {
  uptime_seconds: number;
  uptime_str: string;
  pool_size: number;
  prefixes: PrefixInfo[];
  prefix: string;
  prefix_bits: number;
  max_ipv6: string;
  active_sessions: number;
  total_requests: number;
  active_conns: number;
  total_bytes: number;
  failed_requests: number;
  ipv6_direct: number;
  ipv4_fallback: number;
  max_latency_ms: number;
  min_latency_ms: number;
  socks5_active_conns: number;
  socks5_total_bytes: number;
  socks5_failed_requests: number;
}

export interface DomainDetail {
  domain: string;
  hits: number;
  max_lat_ms: number;
  min_lat_ms: number;
  last_seen: string;
}

export interface Session {
  account: string;
  sid: string;
  exit_ip: string;
  prefix: string;
  ttl_min: number;
  hits: number;
  created_at: string;
  last_seen: string;
  ttl_remain_sec: number;
}

export interface DomainRule {
  domain: string;
  count: number;
  strategy: number;
}

export interface BannedIP {
  ip: string;
  banned_at: string;
  failures: number;
}

export interface UserInfo {
  user: string;
  enabled: boolean;
  default_mode: string;
  default_ttl: number;
  max_ttl: number;
  rate_limit: number;
  created_at: number;
}

export interface TrafficEntry {
  timestamp: string;
  user: string;
  client_ip: string;
  domain: string;
  exit_ip: string;
  actual_ip?: string;
  exit_type: string;
  protocol: string;
  latency_ms: number;
  success: boolean;
  sid?: string;
  mode?: string;
  ttl_min?: number;
}

export interface RateLimitInfo {
  enabled: boolean;
  limit?: number;
  window_sec?: number;
}

export interface PortInfo {
  addr: string;
  type: string;
  is_main: boolean;
  running: boolean;
}

export const api = {
  login: (user: string, pass: string) => request<{ status: string }>('/api/login', { method: 'POST', body: JSON.stringify({ user, pass }) }),
  logout: () => request<{ status: string }>('/api/logout', { method: 'POST' }),
  overview: () => request<Overview>('/api/overview'),
  sessions: () => request<Session[]>('/api/sticky-sessions'),
  clearSessions: () => request<any>('/api/sessions/clear', { method: 'POST' }),
  removeSession: (account: string, sid: string) => request<any>('/api/sticky-sessions/delete', { method: 'POST', body: JSON.stringify({ account, sid }) }),
  users: () => request<{ users: UserInfo[]; accounts: UserInfo[] }>('/api/accounts'),
  addUser: (body: { user: string; pass: string; default_mode?: string; default_ttl?: number; max_ttl?: number; rate_limit?: number }) =>
    request<any>('/api/accounts/add', { method: 'POST', body: JSON.stringify(body) }),
  removeUser: (user: string) => request<any>('/api/accounts/remove', { method: 'POST', body: JSON.stringify({ user }) }),
  updateAccount: (body: Record<string, unknown>) => request<any>('/api/accounts/update', { method: 'POST', body: JSON.stringify(body) }),
  generateCredentials: (body: {
    account: string; mode: string; ttl_minutes: number; count: number;
    protocol: string; host: string; port: number; password: string;
  }) => request<{ status: string; lines: string[]; usernames: string[]; curl: string; message?: string }>(
    '/api/credentials/generate', { method: 'POST', body: JSON.stringify(body) }),
  banned: () => request<{ banned: BannedIP[] }>('/api/banned'),
  unban: (ip: string) => request<any>('/api/banned/unban', { method: 'POST', body: JSON.stringify({ ip }) }),
  ban: (ip: string) => request<any>('/api/banned/ban', { method: 'POST', body: JSON.stringify({ ip }) }),
  whitelist: () => request<{ whitelist: string[] }>('/api/whitelist'),
  whitelistAdd: (ip: string) => request<any>('/api/whitelist/add', { method: 'POST', body: JSON.stringify({ ip }) }),
  whitelistRemove: (ip: string) => request<any>('/api/whitelist/remove', { method: 'POST', body: JSON.stringify({ ip }) }),
  domainRules: () => request<{ rules: DomainRule[] }>('/api/domain-rules'),
  addDomainRule: (domain: string, count: number, strategy: number) => request<any>('/api/domain-rules/add', { method: 'POST', body: JSON.stringify({ domain, count, strategy }) }),
  removeDomainRule: (domain: string) => request<any>('/api/domain-rules/remove', { method: 'POST', body: JSON.stringify({ domain }) }),
  expandPool: () => request<{ status: string; new_size: string }>('/api/pool/expand', { method: 'POST' }),
  togglePrefix: (prefix: string, enabled: boolean) => request<{ status: string; pool_size: number }>('/api/prefix/toggle', { method: 'POST', body: JSON.stringify({ prefix, enabled }) }),
  testPrefix: (prefix: string) => request<{ status: string; exit_ip?: string; latency_ms?: number; message?: string }>('/api/prefix/test', { method: 'POST', body: JSON.stringify({ prefix }) }),
  trafficLog: () => request<{ entries: TrafficEntry[]; total: number }>('/api/traffic-log'),
  clearTrafficLog: () => request<any>('/api/traffic-log/clear', { method: 'POST' }),
  rateLimit: () => request<RateLimitInfo>('/api/rate-limit'),
  ports: () => request<{ ports: PortInfo[] }>('/api/ports'),
  expandPorts: () => request<{ status: string; added: PortInfo[] }>('/api/ports/expand', { method: 'POST' }),
  stopPort: (addr: string) => request<{ status: string }>('/api/ports/stop', { method: 'POST', body: JSON.stringify({ addr }) }),
};
