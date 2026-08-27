import { useState, useEffect } from 'react'
import { Plus, RefreshCw, Trash2, UserCheck, UserMinus } from 'lucide-react'
import { UserInfo, api } from '../api'
import { useI18n } from '../i18n'
import Layout from './Layout'
import { Btn } from './ui'

export default function ProxyUsers({ flash }: { flash: (msg: string) => void }) {
  const { m } = useI18n()
  const [users, setUsers] = useState<UserInfo[]>([])
  const [newUser, setNewUser] = useState('')
  const [newPass, setNewPass] = useState('')
  const [newMode, setNewMode] = useState('rotate')
  const [newTTL, setNewTTL] = useState('10')
  const [editingRL, setEditingRL] = useState<string | null>(null)
  const [rlValue, setRlValue] = useState('')

  const load = async () => {
    try {
      const res = await api.users()
      setUsers(res.accounts ?? res.users ?? [])
    } catch {}
  }

  useEffect(() => { load() }, [])

  const handleAdd = async () => {
    if (!newUser || !newPass) return
    const res = await api.addUser({
      user: newUser,
      pass: newPass,
      default_mode: newMode,
      default_ttl: parseInt(newTTL, 10) || 10,
    })
    if (res.status === 'error') {
      flash(res.message || m.accounts.failed)
      return
    }
    setNewUser('')
    setNewPass('')
    flash(m.accounts.added)
    load()
  }

  const handleRemove = async (u: string) => {
    await api.removeUser(u)
    flash(m.accounts.removed)
    load()
  }

  const startEditRL = (u: UserInfo) => {
    setEditingRL(u.user)
    setRlValue(String(u.rate_limit))
  }

  const saveRL = async (user: string) => {
    const val = parseInt(rlValue, 10)
    if (isNaN(val) || val < 0) return
    await api.updateAccount({ user, rate_limit: val })
    setEditingRL(null)
    flash(m.accounts.rateUpdated)
    load()
  }

  const toggleEnabled = async (u: UserInfo) => {
    await api.updateAccount({ user: u.user, enabled: !u.enabled })
    load()
  }

  const setMode = async (u: UserInfo, mode: string) => {
    await api.updateAccount({ user: u.user, default_mode: mode })
    load()
  }

  return (
    <Layout title={m.accounts.title} actions={<Btn icon={RefreshCw} onClick={load}>{m.common.refresh}</Btn>}>
      <table className="data-table">
        <thead>
          <tr>
            <th>{m.accounts.username}</th>
            <th>{m.accounts.enabled}</th>
            <th>{m.accounts.defaultMode}</th>
            <th>{m.accounts.defaultTtl}</th>
            <th>{m.accounts.maxTtl}</th>
            <th>{m.accounts.rateLimit}</th>
            <th>{m.common.action}</th>
          </tr>
        </thead>
        <tbody>
          {users.map(u => (
            <tr key={u.user}>
              <td className="mono">{u.user}</td>
              <td>
                <Btn icon={u.enabled ? UserCheck : UserMinus} onClick={() => toggleEnabled(u)}>
                  {u.enabled ? m.common.on : m.common.off}
                </Btn>
              </td>
              <td>
                <select className="input" value={u.default_mode || 'rotate'} onChange={e => setMode(u, e.target.value)}>
                  <option value="rotate">{m.generator.rotate}</option>
                  <option value="sticky">{m.generator.sticky}</option>
                </select>
              </td>
              <td className="mono">{u.default_ttl}</td>
              <td className="mono">{u.max_ttl}</td>
              <td>
                {editingRL === u.user ? (
                  <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                    <input
                      className="input"
                      type="number"
                      min={0}
                      value={rlValue}
                      onChange={e => setRlValue(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && saveRL(u.user)}
                      style={{ width: 90 }}
                      autoFocus
                    />
                    <Btn variant="primary" onClick={() => saveRL(u.user)}>{m.common.save}</Btn>
                    <Btn onClick={() => setEditingRL(null)}>{m.common.cancel}</Btn>
                  </div>
                ) : (
                  <span className="mono" style={{ cursor: 'pointer' }} onClick={() => startEditRL(u)}>
                    {u.rate_limit}
                  </span>
                )}
              </td>
              <td><Btn variant="danger" icon={Trash2} onClick={() => handleRemove(u.user)}>{m.common.remove}</Btn></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="input-row">
        <input className="input" placeholder={m.accounts.usernamePh} value={newUser} onChange={e => setNewUser(e.target.value)} />
        <input className="input" type="password" placeholder={m.accounts.passwordPh} value={newPass} onChange={e => setNewPass(e.target.value)} />
        <select className="input" value={newMode} onChange={e => setNewMode(e.target.value)}>
          <option value="rotate">{m.generator.rotate}</option>
          <option value="sticky">{m.generator.sticky}</option>
        </select>
        <input className="input" type="number" min={1} max={180} value={newTTL} onChange={e => setNewTTL(e.target.value)} style={{ width: 80 }} title={m.accounts.ttlTitle} />
        <Btn variant="primary" icon={Plus} onClick={handleAdd}>{m.accounts.add}</Btn>
      </div>
    </Layout>
  )
}
