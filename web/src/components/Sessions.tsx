import { useState, useEffect } from 'react'
import { RefreshCw, Timer, UserX } from 'lucide-react'
import { api, Session } from '../api'
import { useI18n } from '../i18n'
import Layout from './Layout'
import { Btn, Empty } from './ui'

export default function Sessions({ flash }: { flash: (msg: string) => void }) {
  const { m } = useI18n()
  const [sessions, setSessions] = useState<Session[]>([])

  const load = async () => {
    try { setSessions(await api.sessions()) } catch {}
  }

  useEffect(() => { load() }, [])

  const handleClear = async () => {
    await api.clearSessions()
    flash(m.sessions.cleared)
    load()
  }

  const handleRemove = async (s: Session) => {
    await api.removeSession(s.account, s.sid)
    flash(m.sessions.kicked)
    load()
  }

  return (
    <Layout title={m.sessions.title} actions={
      <>
        <Btn icon={RefreshCw} onClick={load}>{m.common.refresh}</Btn>
        <Btn variant="danger" icon={UserX} onClick={handleClear}>{m.sessions.clearAll}</Btn>
      </>
    }>
      <div style={{ overflowX: 'auto' }}>
        <table className="data-table">
          <thead>
            <tr>
              <th>#</th>
              <th>{m.sessions.account}</th>
              <th>{m.sessions.sid}</th>
              <th>{m.sessions.exitIp}</th>
              <th>{m.sessions.hits}</th>
              <th>{m.sessions.ttl}</th>
              <th>{m.sessions.remain}</th>
              <th>{m.sessions.created}</th>
              <th>{m.sessions.lastSeen}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {sessions.length === 0 && (
              <tr><td colSpan={10}><Empty icon={Timer} text={m.sessions.empty} /></td></tr>
            )}
            {sessions.map((s, i) => (
              <tr key={s.account + '/' + s.sid}>
                <td>{i + 1}</td>
                <td className="mono">{s.account}</td>
                <td className="mono">{s.sid}</td>
                <td className="mono" style={{ fontSize: 11 }}>{s.exit_ip}</td>
                <td>{s.hits}</td>
                <td>{s.ttl_min}</td>
                <td>{s.ttl_remain_sec}s</td>
                <td style={{ color: 'var(--mute)' }}>{s.created_at}</td>
                <td>{s.last_seen}</td>
                <td><Btn variant="danger" icon={UserX} onClick={() => handleRemove(s)}>{m.sessions.kick}</Btn></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Layout>
  )
}
