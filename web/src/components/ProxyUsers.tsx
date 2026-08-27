import { useState, useEffect } from 'react'
import { UserInfo, api } from '../api'
import Layout from './Layout'

export default function ProxyUsers({ flash }: { flash: (msg: string) => void }) {
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
      flash(res.message || 'Failed')
      return
    }
    setNewUser('')
    setNewPass('')
    flash('Account added')
    load()
  }

  const handleRemove = async (u: string) => {
    await api.removeUser(u)
    flash('Account removed')
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
    flash('Rate limit updated')
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
    <Layout title="Proxy Accounts" actions={<button className="btn btn-ghost" onClick={load}>Refresh</button>}>
      <table className="data-table">
        <thead>
          <tr>
            <th>Username</th>
            <th>Enabled</th>
            <th>Default Mode</th>
            <th>Default TTL (min)</th>
            <th>Max TTL</th>
            <th>Rate Limit (req/min)</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {users.map(u => (
            <tr key={u.user}>
              <td className="mono">{u.user}</td>
              <td>
                <button className="btn btn-ghost" onClick={() => toggleEnabled(u)}>
                  {u.enabled ? 'on' : 'off'}
                </button>
              </td>
              <td>
                <select className="input" value={u.default_mode || 'rotate'} onChange={e => setMode(u, e.target.value)}>
                  <option value="rotate">rotate</option>
                  <option value="sticky">sticky</option>
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
                    <button className="btn btn-primary" onClick={() => saveRL(u.user)}>Save</button>
                    <button className="btn btn-ghost" onClick={() => setEditingRL(null)}>Cancel</button>
                  </div>
                ) : (
                  <span className="mono" style={{ cursor: 'pointer' }} onClick={() => startEditRL(u)}>
                    {u.rate_limit}
                  </span>
                )}
              </td>
              <td><button className="btn btn-danger" onClick={() => handleRemove(u.user)}>Remove</button></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="input-row">
        <input className="input" placeholder="username" value={newUser} onChange={e => setNewUser(e.target.value)} />
        <input className="input" type="password" placeholder="password" value={newPass} onChange={e => setNewPass(e.target.value)} />
        <select className="input" value={newMode} onChange={e => setNewMode(e.target.value)}>
          <option value="rotate">rotate</option>
          <option value="sticky">sticky</option>
        </select>
        <input className="input" type="number" min={1} max={180} value={newTTL} onChange={e => setNewTTL(e.target.value)} style={{ width: 80 }} title="default sticky minutes" />
        <button className="btn btn-primary" onClick={handleAdd}>Add Account</button>
      </div>
    </Layout>
  )
}
