import { useState, useEffect } from 'react'
import { api, Session } from '../api'
import Layout from './Layout'

function lat(ms: number): string { return ms < 0 ? '-' : ms + 'ms' }

export default function Sessions({ flash }: { flash: (msg: string) => void }) {
  const [sessions, setSessions] = useState<Session[]>([])
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [expandedIPs, setExpandedIPs] = useState<Set<string>>(new Set())

  const load = async () => {
    try { setSessions(await api.sessions()) } catch {}
  }

  useEffect(() => { load() }, [])

  const toggle = (fp: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(fp) ? next.delete(fp) : next.add(fp)
      return next
    })
  }

  const toggleIPs = (fp: string) => {
    setExpandedIPs(prev => {
      const next = new Set(prev)
      next.has(fp) ? next.delete(fp) : next.add(fp)
      return next
    })
  }

  const handleClear = async () => {
    await api.clearSessions()
    flash('All sessions cleared')
    load()
  }

  const handleRemove = async (fp: string) => {
    await api.removeSession(fp)
    flash('Session removed')
    load()
  }

  return (
    <Layout title="Sticky Sessions" actions={<><button className="btn btn-ghost" onClick={load}>Refresh</button><button className="btn btn-ghost" onClick={handleClear}>Clear All</button></>}>
      <div style={{ overflowX: 'auto' }}>
      <table className="data-table">
        <thead>
          <tr>
            <th style={{ width: 30 }}>#</th><th style={{ width: 100 }}>Fingerprint</th><th style={{ width: 120 }}>Exit IPs</th><th style={{ width: 45 }}>Hits</th>
            <th style={{ width: 50 }}>Max</th><th style={{ width: 50 }}>Min</th><th style={{ width: 50 }}>Avg</th>
            <th style={{ width: 90 }}>Created</th><th style={{ width: 90 }}>Last Seen</th><th style={{ width: 360 }}>Domains</th><th style={{ width: 36 }}></th>
          </tr>
        </thead>
        <tbody>
          {sessions.length === 0 && <tr><td colSpan={11} style={{ color: '#475569' }}>No active sessions</td></tr>}
          {sessions.map((s, i) => (
            <tr key={s.fingerprint}>
              <td>{i + 1}</td>
              <td className="mono fp-tooltip" style={{ color: '#818cf8', maxWidth: 120, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {s.fingerprint}
                <span className="fp-tip">{s.fingerprint}</span>
              </td>
              <td className="mono">
                {(s.exit_ips && s.exit_ips.length > 1) ? (
                  <>
                    <span className="expand-toggle" onClick={() => toggleIPs(s.fingerprint)}>
                      {s.exit_ips.length} IPs {expandedIPs.has(s.fingerprint) ? '▾' : '▸'}
                    </span>
                    {expandedIPs.has(s.fingerprint) && (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 2, marginTop: 4 }}>
                        {s.exit_ips.map(ip => <span key={ip} style={{ fontSize: 11 }}>{ip}</span>)}
                      </div>
                    )}
                  </>
                ) : s.exit_ip}
              </td>
              <td>{s.total_hits}</td>
              <td>{lat(s.max_lat_ms)}</td>
              <td>{lat(s.min_lat_ms)}</td>
              <td>{lat(s.avg_lat_ms)}</td>
              <td style={{ color: '#475569' }}>{s.created_at}</td>
              <td>{s.last_seen}</td>
              <td>
                <span className="expand-toggle" onClick={() => toggle(s.fingerprint)}>
                  {s.domains?.length ?? 0} domain(s)
                </span>
                {expanded.has(s.fingerprint) && s.domains && (
                  <div className="domain-sub">
                    <table className="data-table">
                      <thead><tr><th style={{ width: '38%' }}>Domain</th><th style={{ width: '10%' }}>Hits</th><th style={{ width: '13%' }}>Max</th><th style={{ width: '13%' }}>Min</th><th style={{ width: '26%' }}>Last</th></tr></thead>
                      <tbody>
                        {s.domains.map(d => (
                          <tr key={d.domain}>
                            <td className="mono">{d.domain}</td>
                            <td>{d.hits}</td>
                            <td>{lat(d.max_lat_ms)}</td>
                            <td>{lat(d.min_lat_ms)}</td>
                            <td>{d.last_seen}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </td>
              <td><button className="btn btn-danger" onClick={() => handleRemove(s.fingerprint)}>X</button></td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>
    </Layout>
  )
}
