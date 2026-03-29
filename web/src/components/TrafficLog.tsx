import { useState, useEffect } from 'react'
import { TrafficEntry, RateLimitInfo, api } from '../api'
import Layout from './Layout'

export default function TrafficLog() {
  const [entries, setEntries] = useState<TrafficEntry[]>([])
  const [total, setTotal] = useState(0)
  const [rateInfo, setRateInfo] = useState<RateLimitInfo | null>(null)

  const load = () => {
    api.trafficLog().then(d => { setEntries(d.entries); setTotal(d.total) })
    api.rateLimit().then(setRateInfo)
  }

  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t) }, [])

  const handleClear = async () => { await api.clearTrafficLog(); load() }

  return (
    <Layout
      title="Traffic Log"
      actions={
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          {rateInfo?.enabled && (
            <span style={{ fontSize: 12, color: '#94a3b8' }}>
              Rate Limit: {rateInfo.limit} req/{rateInfo.window_sec}s
            </span>
          )}
          <span style={{ fontSize: 12, color: '#64748b' }}>{total} total</span>
          <button className="btn btn-ghost" onClick={load}>Refresh</button>
          <button className="btn btn-danger" onClick={handleClear}>Clear</button>
        </div>
      }
    >
      <table className="data-table">
        <thead>
          <tr>
            <th>Time</th>
            <th>User</th>
            <th>Client IP</th>
            <th>Domain</th>
            <th>Exit IP</th>
            <th>Exit</th>
            <th>Proto</th>
            <th>Latency</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e, i) => (
            <tr key={i}>
              <td className="mono">{e.timestamp.split(' ')[1] || e.timestamp}</td>
              <td>{e.user}</td>
              <td className="mono">{e.client_ip}</td>
              <td>{e.domain}</td>
              <td className="mono" style={{ fontSize: 11 }}>{e.actual_ip || e.exit_ip}</td>
              <td style={{ color: e.exit_type === 'ipv6' ? '#22c55e' : '#f59e0b', fontSize: 11 }}>
                {e.exit_type === 'ipv6' ? 'IPv6' : 'v4↓'}
              </td>
              <td>{e.protocol}</td>
              <td>{e.latency_ms}ms</td>
              <td style={{ color: e.success ? '#22c55e' : '#ef4444' }}>
                {e.success ? 'OK' : 'FAIL'}
              </td>
            </tr>
          ))}
          {entries.length === 0 && (
            <tr><td colSpan={9} style={{ textAlign: 'center', color: '#64748b', padding: 20 }}>No traffic recorded yet</td></tr>
          )}
        </tbody>
      </table>
    </Layout>
  )
}
