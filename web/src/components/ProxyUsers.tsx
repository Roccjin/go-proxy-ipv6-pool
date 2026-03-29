import { useState, useEffect } from 'react'
import { UserInfo, api } from '../api'
import Layout from './Layout'

export default function ProxyUsers({ flash }: { flash: (msg: string) => void }) {
  const [users, setUsers] = useState<UserInfo[]>([])
  const [newUser, setNewUser] = useState('')
  const [newPass, setNewPass] = useState('')
  const [editingRL, setEditingRL] = useState<string | null>(null)
  const [rlValue, setRlValue] = useState('')

  const load = async () => {
    try {
      const res = await api.users()
      setUsers(res.users ?? [])
    } catch {}
  }

  useEffect(() => { load() }, [])

  const handleAdd = async () => {
    if (!newUser || !newPass) return
    await api.addUser(newUser, newPass)
    setNewUser('')
    setNewPass('')
    flash('User added')
    load()
  }

  const handleRemove = async (u: string) => {
    await api.removeUser(u)
    flash('User removed')
    load()
  }

  const startEditRL = (u: UserInfo) => {
    setEditingRL(u.user)
    setRlValue(String(u.rate_limit))
  }

  const saveRL = async (user: string) => {
    const val = parseInt(rlValue, 10)
    if (isNaN(val) || val < 0) return
    await api.setUserRateLimit(user, val)
    setEditingRL(null)
    flash('Rate limit updated')
    load()
  }

  return (
    <Layout title="Proxy Users" actions={<button className="btn btn-ghost" onClick={load}>Refresh</button>}>
      <table className="data-table">
        <thead>
          <tr>
            <th>Username</th>
            <th>Rate Limit (req/min)</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {users.map(u => (
            <tr key={u.user}>
              <td className="mono">{u.user}</td>
              <td>
                {editingRL === u.user ? (
                  <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                    <input
                      className="input"
                      type="number"
                      min={1}
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
        <button className="btn btn-primary" onClick={handleAdd}>Add User</button>
      </div>
    </Layout>
  )
}
