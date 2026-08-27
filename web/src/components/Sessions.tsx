import { useState, useEffect } from 'react'
import { api, Session } from '../api'
import Layout from './Layout'

export default function Sessions({ flash }: { flash: (msg: string) => void }) {
  const [sessions, setSessions] = useState<Session[]>([])

  const load = async () => {
    try { setSessions(await api.sessions()) } catch {}
  }

  useEffect(() => { load() }, [])

  const handleClear = async () => {
    await api.clearSessions()
    flash('All sticky sessions cleared')
    load()
  }

  const handleRemove = async (s: Session) => {
    await api.removeSession(s.account, s.sid)
    flash('Session kicked')
    load()
  }

  return (
    <Layout title="Sticky Sessions" actions={<><button className="btn btn-ghost" onClick={load}>Refresh</button><button className="btn btn-ghost" onClick={handleClear}>Clear All</button></>}>
      <div style={{ overflowX: 'auto' }}>
      <table className="data-table">
        <thead>
          <tr>
            <th>#</th>
            <th>Account</th>
            <th>SID</th>
            <th>Exit IP</th>
            <th>Hits</th>
            <th>TTL (min)</th>
            <th>Remain</th>
            <th>Created</th>
            <th>Last Seen</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {sessions.length === 0 && <tr><td colSpan={10} style={{ color: '#475569' }}>No sticky sessions</td></tr>}
          {sessions.map((s, i) => (
            <tr key={s.account + '/' + s.sid}>
              <td>{i + 1}</td>
              <td className="mono">{s.account}</td>
              <td className="mono">{s.sid}</td>
              <td className="mono" style={{ fontSize: 11 }}>{s.exit_ip}</td>
              <td>{s.hits}</td>
              <td>{s.ttl_min}</td>
              <td>{s.ttl_remain_sec}s</td>
              <td style={{ color: '#475569' }}>{s.created_at}</td>
              <td>{s.last_seen}</td>
              <td><button className="btn btn-danger" onClick={() => handleRemove(s)}>Kick</button></td>
            </tr>
          ))}
        </tbody>
      </table>
      </div>
    </Layout>
  )
}
