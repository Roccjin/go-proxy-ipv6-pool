import { useState } from 'react'
import { Overview, PrefixInfo, api } from '../api'
import Layout from './Layout'

function fmtBytes(b: number): string {
  if (b > 1073741824) return (b / 1073741824).toFixed(2) + ' GB'
  if (b > 1048576) return (b / 1048576).toFixed(1) + ' MB'
  if (b > 1024) return (b / 1024).toFixed(1) + ' KB'
  return b + ' B'
}

function lat(ms: number): string {
  return ms < 0 ? '-' : ms + 'ms'
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

function PrefixCard({ p, onRefresh }: { p: PrefixInfo; onRefresh: () => void }) {
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
    } catch (e: any) {
      setTestResult({ status: 'error', message: e.message })
    } finally { setTesting(false) }
  }

  return (
    <div className={`prefix-card${!p.enabled ? ' disabled' : ''}`}>
      <div className="prefix-header">
        <span className={`prefix-status ${p.enabled ? 'online' : 'offline'}`} />
        <span className="mono" style={{ fontSize: 13 }}>/{p.bits}</span>
        <span className="mono" style={{ color: '#818cf8', fontSize: 12 }}>{p.prefix}</span>
      </div>
      <div className="prefix-meta">
        <span>on-the-fly</span>
        <span style={{ color: '#475569', fontSize: 11 }}>max: {p.max_capacity}</span>
      </div>
      <div className="prefix-actions">
        <button
          className={`btn ${p.enabled ? 'btn-danger' : 'btn-primary'}`}
          onClick={handleToggle}
          disabled={toggling}
          style={{ fontSize: 11, padding: '3px 10px' }}
        >
          {toggling ? '...' : p.enabled ? 'Disable' : 'Enable'}
        </button>
        <button
          className="btn btn-ghost"
          onClick={handleTest}
          disabled={testing}
          style={{ fontSize: 11, padding: '3px 10px' }}
        >
          {testing ? 'Testing...' : 'Test'}
        </button>
      </div>
      {testResult && (
        <div className={`prefix-test-result ${testResult.status === 'ok' ? 'success' : 'fail'}`}>
          {testResult.status === 'ok'
            ? <>
                <div>{testResult.exit_ip} ({testResult.latency_ms}ms)</div>
                {testResult.geo && testResult.geo.status === 'success' && (
                  <div style={{ fontSize: 11, color: '#94a3b8', marginTop: 3 }}>
                    {[testResult.geo.city, testResult.geo.regionName, testResult.geo.country].filter(Boolean).join(', ')}
                    {testResult.geo.isp && <span style={{ color: '#64748b' }}> · {testResult.geo.isp}</span>}
                    {testResult.geo.as && <span style={{ color: '#64748b' }}> · {testResult.geo.as}</span>}
                  </div>
                )}
              </>
            : `FAIL — ${testResult.message}`}
        </div>
      )}
    </div>
  )
}

export default function OverviewPanel({ data, onRefresh }: { data: Overview | null; onRefresh: () => void }) {
  if (!data) return null

  const prefixes = data.prefixes ?? []

  return (
    <Layout title="Overview" actions={<><button className="btn btn-ghost" onClick={onRefresh}>Refresh</button></>}>
      <div className="prefix-grid">
        {prefixes.map((p, i) => (
          <PrefixCard key={i} p={p} onRefresh={onRefresh} />
        ))}
      </div>
      <div className="stat-grid">
        <Stat label="Max IPv6" value={data.max_ipv6} cls="accent" wide />
        <Stat label="Sticky Sessions" value={data.active_sessions} />
        <Stat label="Total Requests" value={data.total_requests} />
        <Stat label="IPv6 Direct" value={data.ipv6_direct} cls="success" />
        <Stat label="IPv4 Fallback" value={data.ipv4_fallback} cls={data.ipv4_fallback > 0 ? 'warning' : ''} />
        <Stat label="HTTP Conns" value={data.active_conns} />
        <Stat label="HTTP Traffic" value={fmtBytes(data.total_bytes)} />
        <Stat label="HTTP Failed" value={data.failed_requests} cls="danger" />
        <Stat label="SOCKS5 Conns" value={data.socks5_active_conns} />
        <Stat label="SOCKS5 Traffic" value={fmtBytes(data.socks5_total_bytes)} />
        <Stat label="SOCKS5 Failed" value={data.socks5_failed_requests} cls="danger" />
        <Stat label="Max Latency" value={lat(data.max_latency_ms)} />
        <Stat label="Min Latency" value={lat(data.min_latency_ms)} />
      </div>
    </Layout>
  )
}
