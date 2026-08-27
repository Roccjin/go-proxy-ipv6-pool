import { useState, useEffect } from 'react'
import { Activity, ArrowDown, Hexagon, RefreshCw, Trash2 } from 'lucide-react'
import { TrafficEntry, RateLimitInfo, api } from '../api'
import { useI18n } from '../i18n'
import Layout from './Layout'
import { Btn, Empty } from './ui'

export default function TrafficLog() {
  const { m, t } = useI18n()
  const [entries, setEntries] = useState<TrafficEntry[]>([])
  const [total, setTotal] = useState(0)
  const [rateInfo, setRateInfo] = useState<RateLimitInfo | null>(null)

  const load = () => {
    api.trafficLog().then(d => { setEntries(d.entries); setTotal(d.total) })
    api.rateLimit().then(setRateInfo)
  }

  useEffect(() => { load(); const timer = setInterval(load, 5000); return () => clearInterval(timer) }, [])

  const handleClear = async () => { await api.clearTrafficLog(); load() }

  return (
    <Layout
      title={m.traffic.title}
      actions={
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          {rateInfo?.enabled && (
            <span className="badge" style={{ color: 'var(--mute)' }}>
              {t('traffic.rateLimit', { limit: rateInfo.limit ?? 0, window: rateInfo.window_sec ?? 0 })}
            </span>
          )}
          <span className="badge" style={{ color: 'var(--mute)' }}>{t('traffic.total', { n: total })}</span>
          <Btn icon={RefreshCw} onClick={load}>{m.common.refresh}</Btn>
          <Btn variant="danger" icon={Trash2} onClick={handleClear}>{m.common.clear}</Btn>
        </div>
      }
    >
      <table className="data-table">
        <thead>
          <tr>
            <th>{m.traffic.time}</th>
            <th>{m.traffic.user}</th>
            <th>{m.traffic.sid}</th>
            <th>{m.traffic.mode}</th>
            <th>{m.traffic.clientIp}</th>
            <th>{m.traffic.domain}</th>
            <th>{m.traffic.exitIp}</th>
            <th>{m.traffic.exit}</th>
            <th>{m.traffic.proto}</th>
            <th>{m.traffic.latency}</th>
            <th>{m.traffic.status}</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e, i) => (
            <tr key={i}>
              <td className="mono">{e.timestamp.split(' ')[1] || e.timestamp}</td>
              <td>{e.user}</td>
              <td className="mono">{e.sid || '—'}</td>
              <td>{e.mode || '—'}</td>
              <td className="mono">{e.client_ip}</td>
              <td>{e.domain}</td>
              <td className="mono" style={{ fontSize: 11 }}>{e.actual_ip || e.exit_ip}</td>
              <td>
                {e.exit_type === 'ipv6'
                  ? <span className="badge ion"><Hexagon size={12} strokeWidth={1.75} /> IPv6</span>
                  : <span className="badge warn"><ArrowDown size={12} strokeWidth={1.75} /> IPv4</span>}
              </td>
              <td>{e.protocol}</td>
              <td>{e.latency_ms}ms</td>
              <td>
                {e.success
                  ? <span className="badge ion">{m.traffic.ok}</span>
                  : <span className="badge fail">{m.traffic.fail}</span>}
              </td>
            </tr>
          ))}
          {entries.length === 0 && (
            <tr><td colSpan={11}><Empty icon={Activity} text={m.traffic.empty} /></td></tr>
          )}
        </tbody>
      </table>
    </Layout>
  )
}
