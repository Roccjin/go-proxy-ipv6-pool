import { useState, useEffect } from 'react'
import { Ban, Plus, RefreshCw, ShieldCheck, ShieldOff, Trash2 } from 'lucide-react'
import { api, BannedIP } from '../api'
import { useI18n } from '../i18n'
import Layout from './Layout'
import { Btn, Empty } from './ui'

export default function BannedIPs({ flash }: { flash: (msg: string) => void }) {
  const { m } = useI18n()
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
    flash(m.banned.unbanned)
    load()
  }

  const handleBan = async () => {
    if (!banIP.trim()) return
    await api.ban(banIP.trim())
    setBanIP('')
    flash(m.banned.banned)
    load()
  }

  const handleWhitelistAdd = async () => {
    if (!wlIP.trim()) return
    await api.whitelistAdd(wlIP.trim())
    setWlIP('')
    flash(m.banned.whitelisted)
    load()
  }

  const handleWhitelistRemove = async (ip: string) => {
    await api.whitelistRemove(ip)
    flash(m.banned.wlRemoved)
    load()
  }

  return (
    <Layout title={m.banned.title} actions={<Btn icon={RefreshCw} onClick={load}>{m.common.refresh}</Btn>}>
      <table className="data-table">
        <thead><tr><th>{m.banned.ip}</th><th>{m.banned.bannedAt}</th><th>{m.banned.failures}</th><th>{m.common.action}</th></tr></thead>
        <tbody>
          {banned.length === 0 && (
            <tr><td colSpan={4}><Empty icon={Ban} text={m.banned.empty} /></td></tr>
          )}
          {banned.map(b => (
            <tr key={b.ip}>
              <td className="mono" style={{ color: 'var(--fail)' }}>{b.ip}</td>
              <td>{b.banned_at}</td>
              <td>{b.failures}</td>
              <td><Btn icon={ShieldOff} onClick={() => handleUnban(b.ip)}>{m.banned.unban}</Btn></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="input-row">
        <input className="input" placeholder={m.banned.banPh} value={banIP} onChange={e => setBanIP(e.target.value)} />
        <Btn variant="danger" icon={Ban} onClick={handleBan}>{m.banned.ban}</Btn>
      </div>

      <div className="section-label">{m.banned.whitelist}</div>
      <table className="data-table">
        <thead><tr><th>{m.banned.ip}</th><th>{m.common.action}</th></tr></thead>
        <tbody>
          {whitelist.length === 0 && (
            <tr><td colSpan={2}><Empty icon={ShieldCheck} text={m.banned.wlEmpty} /></td></tr>
          )}
          {whitelist.map(ip => (
            <tr key={ip}>
              <td className="mono" style={{ color: 'var(--ion)' }}>{ip}</td>
              <td><Btn variant="danger" icon={Trash2} onClick={() => handleWhitelistRemove(ip)}>{m.common.remove}</Btn></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="input-row">
        <input className="input" placeholder={m.banned.wlPh} value={wlIP} onChange={e => setWlIP(e.target.value)} />
        <Btn variant="ion" icon={Plus} onClick={handleWhitelistAdd}>{m.banned.wlAdd}</Btn>
      </div>
    </Layout>
  )
}
