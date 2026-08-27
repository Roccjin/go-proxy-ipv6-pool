import { useState } from 'react'
import { FlaskConical, Power, PowerOff, RefreshCw, Shield, ShieldOff } from 'lucide-react'
import { Overview, PrefixInfo, api } from '../api'
import { useI18n } from '../i18n'
import Layout from './Layout'
import { Btn } from './ui'

function fmtBytes(b: number): string {
  if (b > 1073741824) return (b / 1073741824).toFixed(2) + ' GB'
  if (b > 1048576) return (b / 1048576).toFixed(1) + ' MB'
  if (b > 1024) return (b / 1024).toFixed(1) + ' KB'
  return b + ' B'
}

function lat(ms: number): string {
  return ms < 0 ? '—' : ms + 'ms'
}

function Stat({ label, value, cls, wide }: { label: string; value: string | number; cls?: string; wide?: boolean }) {
  return (
    <div className={`stat-card${wide ? ' wide' : ''}`}>
      <div className="stat-label">{label}</div>
      <div className={`stat-value ${cls ?? ''}`}>{value}</div>
    </div>
  )
}

interface TestResult {
  status: string
  exit_ip?: string
  latency_ms?: number
  message?: string
  geo?: {
    status?: string
    country?: string
    regionName?: string
    city?: string
    isp?: string
    org?: string
    as?: string
    query?: string
    error?: string
  }
}

function PrefixBody({ p, onRefresh, hero }: { p: PrefixInfo; onRefresh: () => void; hero?: boolean }) {
  const { m, t } = useI18n()
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<TestResult | null>(null)
  const [toggling, setToggling] = useState(false)

  const handleToggle = async () => {
    setToggling(true)
    try {
      await api.togglePrefix(p.prefix, !p.enabled)
      onRefresh()
    } finally { setToggling(false) }
  }

  const handleTest = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const res = await api.testPrefix(p.prefix)
      setTestResult(res)
    } catch (e: unknown) {
      setTestResult({ status: 'error', message: e instanceof Error ? e.message : String(e) })
    } finally { setTesting(false) }
  }

  const actions = (
    <div className="prefix-actions">
      <Btn
        variant={p.enabled ? 'danger' : 'primary'}
        icon={p.enabled ? PowerOff : Power}
        onClick={handleToggle}
        disabled={toggling}
      >
        {toggling ? '...' : p.enabled ? m.common.disable : m.common.enable}
      </Btn>
      <Btn icon={FlaskConical} onClick={handleTest} disabled={testing}>
        {testing ? m.overview.testing : m.overview.test}
      </Btn>
    </div>
  )

  const result = testResult && (
    <div className={`prefix-test-result ${testResult.status === 'ok' ? 'success' : 'fail'}`}>
      {testResult.status === 'ok'
        ? <>
            <div>{testResult.exit_ip} ({testResult.latency_ms}ms)</div>
            {testResult.geo && testResult.geo.status === 'success' && (
              <div style={{ marginTop: 6, color: 'var(--mute)' }}>
                {[testResult.geo.city, testResult.geo.regionName, testResult.geo.country].filter(Boolean).join(', ')}
                {testResult.geo.isp && <span> · {testResult.geo.isp}</span>}
                {testResult.geo.as && <span> · {testResult.geo.as}</span>}
              </div>
            )}
          </>
        : t('overview.fail', { msg: testResult.message ?? '' })}
    </div>
  )

  if (hero) {
    return (
      <div className="prefix-hero">
        <div>
          <div className="mast-kicker">/{p.bits} · {p.enabled ? m.status.online : m.status.offline}</div>
          <div className="prefix-addr">
            {p.prefix}<span>/{p.bits}</span>
          </div>
        </div>
        <div className="prefix-plate">
          <div className="prefix-meta">
            <span>{m.overview.onTheFly}</span>
            <span>{m.overview.max} {p.max_capacity}</span>
          </div>
          {actions}
          {result}
        </div>
      </div>
    )
  }

  return (
    <div className={`prefix-card${!p.enabled ? ' disabled' : ''}`}>
      <div className="prefix-header">
        <span className={`pulse ${p.enabled ? 'on' : ''}`} />
        <span className="mono">/{p.bits}</span>
        <span className="mono" style={{ color: 'var(--ion)' }}>{p.prefix}</span>
      </div>
      <div className="prefix-meta">
        <span>{m.overview.onTheFly}</span>
        <span>{m.overview.max} {p.max_capacity}</span>
      </div>
      {actions}
      {result}
    </div>
  )
}

export default function OverviewPanel({ data, onRefresh }: { data: Overview | null; onRefresh: () => void }) {
  const { m } = useI18n()
  const [togglingV4, setTogglingV4] = useState(false)
  if (!data) return null

  const prefixes = data.prefixes ?? []
  const [first, ...rest] = prefixes
  const ipv4On = !!data.ipv4_fallback_enabled

  const toggleIPv4 = async () => {
    setTogglingV4(true)
    try {
      await api.setIPv4Fallback(!ipv4On)
      onRefresh()
    } finally {
      setTogglingV4(false)
    }
  }

  return (
    <Layout title={m.overview.title} actions={<Btn icon={RefreshCw} onClick={onRefresh}>{m.common.refresh}</Btn>}>
      <div className="prefix-plate" style={{ marginBottom: 22 }}>
        <div className="prefix-meta">
          <span className={`badge ${ipv4On ? 'warn' : 'ion'}`}>
            {ipv4On ? m.overview.ipv4On : m.overview.ipv4Off}
          </span>
        </div>
        <p className="lede" style={{ marginBottom: 0 }}>{m.overview.ipv4Hint}</p>
        <div className="prefix-actions">
          <Btn
            variant={ipv4On ? 'danger' : 'ion'}
            icon={ipv4On ? ShieldOff : Shield}
            onClick={toggleIPv4}
            disabled={togglingV4}
          >
            {ipv4On ? m.overview.ipv4Disable : m.overview.ipv4Enable}
          </Btn>
        </div>
      </div>
      {first && <PrefixBody p={first} onRefresh={onRefresh} hero />}
      {rest.length > 0 && (
        <div className="prefix-grid">
          {rest.map((p, i) => (
            <PrefixBody key={i} p={p} onRefresh={onRefresh} />
          ))}
        </div>
      )}
      <div className="stat-strip">
        <Stat label={m.overview.maxIpv6} value={data.max_ipv6} cls="accent" wide />
        <Stat label={m.overview.stickySessions} value={data.active_sessions} />
        <Stat label={m.overview.totalRequests} value={data.total_requests} />
        <Stat label={m.overview.ipv6Direct} value={data.ipv6_direct} cls="success" />
        <Stat label={m.overview.ipv4Fallback} value={data.ipv4_fallback} cls={data.ipv4_fallback > 0 ? 'warning' : ''} />
        <Stat label={m.overview.httpConns} value={data.active_conns} />
        <Stat label={m.overview.httpTraffic} value={fmtBytes(data.total_bytes)} />
        <Stat label={m.overview.httpFailed} value={data.failed_requests} cls="danger" />
        <Stat label={m.overview.socksConns} value={data.socks5_active_conns} />
        <Stat label={m.overview.socksTraffic} value={fmtBytes(data.socks5_total_bytes)} />
        <Stat label={m.overview.socksFailed} value={data.socks5_failed_requests} cls="danger" />
        <Stat label={m.overview.maxLatency} value={lat(data.max_latency_ms)} />
        <Stat label={m.overview.minLatency} value={lat(data.min_latency_ms)} />
      </div>
    </Layout>
  )
}
