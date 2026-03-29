import { useState, useEffect } from 'react'
import { api, BannedIP } from '../api'
import Layout from './Layout'

export default function BannedIPs({ flash }: { flash: (msg: string) => void }) {
  const [banned, setBanned] = useState<BannedIP[]>([])
  const [banIP, setBanIP] = useState('')
  const [whitelist, setWhitelist] = useState<string[]>([])
  const [wlIP, setWlIP] = useState('')

  const load = async () => {
    try {
      const [b, w] = await Promise.all([api.banned(), api.whitelist()])
      setBanned(b.banned ?? [])
      setWhitelist(w.whitelist ?? [])
    } catch {}
  }

  useEffect(() => { load() }, [])

  const handleUnban = async (ip: string) => {
    await api.unban(ip)
    flash('IP unbanned')
    load()
  }

  const handleBan = async () => {
    if (!banIP.trim()) return
    await api.ban(banIP.trim())
    setBanIP('')
    flash('IP banned')
    load()
  }

  const handleWhitelistAdd = async () => {
    if (!wlIP.trim()) return
    await api.whitelistAdd(wlIP.trim())
    setWlIP('')
    flash('IP whitelisted')
    load()
  }

  const handleWhitelistRemove = async (ip: string) => {
    await api.whitelistRemove(ip)
    flash('Removed from whitelist')
    load()
  }

  return (
    <Layout title="Banned IPs" actions={<button className="btn btn-ghost" onClick={load}>Refresh</button>}>
      <table className="data-table">
        <thead><tr><th>IP</th><th>Banned At</th><th>Failures</th><th>Action</th></tr></thead>
        <tbody>
          {banned.length === 0 && <tr><td colSpan={4} style={{ color: '#475569' }}>No banned IPs</td></tr>}
          {banned.map(b => (
            <tr key={b.ip}>
              <td className="mono" style={{ color: '#ef4444' }}>{b.ip}</td>
              <td>{b.banned_at}</td>
              <td>{b.failures}</td>
              <td><button className="btn btn-ghost" onClick={() => handleUnban(b.ip)}>Unban</button></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="input-row">
        <input className="input" placeholder="IP address to ban" value={banIP} onChange={e => setBanIP(e.target.value)} />
        <button className="btn btn-danger" onClick={handleBan}>Ban IP</button>
      </div>

      <div style={{ marginTop: 24 }}>
        <h3 style={{ color: '#e2e8f0', fontSize: 14, marginBottom: 12 }}>IP Whitelist</h3>
        <table className="data-table">
          <thead><tr><th>IP</th><th>Action</th></tr></thead>
          <tbody>
            {whitelist.length === 0 && <tr><td colSpan={2} style={{ color: '#475569' }}>No whitelisted IPs</td></tr>}
            {whitelist.map(ip => (
              <tr key={ip}>
                <td className="mono" style={{ color: '#22c55e' }}>{ip}</td>
                <td><button className="btn btn-danger" onClick={() => handleWhitelistRemove(ip)}>Remove</button></td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="input-row">
          <input className="input" placeholder="IP to whitelist" value={wlIP} onChange={e => setWlIP(e.target.value)} />
          <button className="btn btn-primary" onClick={handleWhitelistAdd}>Add to Whitelist</button>
        </div>
      </div>
    </Layout>
  )
}
