import { useEffect, useState } from 'react'
import { UserInfo, api } from '../api'
import Layout from './Layout'

export default function Generator({ flash }: { flash: (msg: string) => void }) {
  const [accounts, setAccounts] = useState<UserInfo[]>([])
  const [account, setAccount] = useState('')
  const [password, setPassword] = useState('')
  const [mode, setMode] = useState('sticky')
  const [ttl, setTtl] = useState('10')
  const [count, setCount] = useState('10')
  const [protocol, setProtocol] = useState('http')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('')
  const [lines, setLines] = useState<string[]>([])
  const [curl, setCurl] = useState('')

  useEffect(() => {
    api.users().then(res => {
      const list = res.accounts ?? res.users ?? []
      setAccounts(list)
      if (list.length && !account) setAccount(list[0].user)
    }).catch(() => {})
  }, [])

  const generate = async () => {
    if (!account || !password) {
      flash('Account and password required')
      return
    }
    const res = await api.generateCredentials({
      account,
      mode,
      ttl_minutes: parseInt(ttl, 10) || 10,
      count: parseInt(count, 10) || 10,
      protocol,
      host,
      port: parseInt(port, 10) || 0,
      password,
    })
    if (res.status === 'error') {
      flash(res.message || 'Generate failed')
      return
    }
    setLines(res.lines || [])
    setCurl(res.curl || '')
    flash('Generated ' + (res.lines?.length ?? 0) + ' credentials')
  }

  const copy = async () => {
    await navigator.clipboard.writeText(lines.join('\n'))
    flash('Copied')
  }

  return (
    <Layout title="Credential Generator" actions={<button className="btn btn-primary" onClick={generate}>Generate</button>}>
      <p style={{ color: '#94a3b8', fontSize: 13, marginBottom: 12 }}>
        Sticky format: <span className="mono">account_sid_XXXXXXXX_time_N:password@host:port</span>.
        Rotate uses the bare account name. Credentials are not stored; first use creates the sticky session.
      </p>
      <div className="input-row" style={{ flexWrap: 'wrap' }}>
        <select className="input" value={account} onChange={e => setAccount(e.target.value)}>
          {accounts.map(a => <option key={a.user} value={a.user}>{a.user}</option>)}
        </select>
        <input className="input" type="password" placeholder="account password" value={password} onChange={e => setPassword(e.target.value)} />
        <select className="input" value={mode} onChange={e => setMode(e.target.value)}>
          <option value="sticky">sticky</option>
          <option value="rotate">rotate</option>
        </select>
        <input className="input" type="number" min={1} max={180} value={ttl} onChange={e => setTtl(e.target.value)} style={{ width: 80 }} title="minutes" disabled={mode !== 'sticky'} />
        <input className="input" type="number" min={1} max={100} value={count} onChange={e => setCount(e.target.value)} style={{ width: 80 }} title="count" />
        <select className="input" value={protocol} onChange={e => setProtocol(e.target.value)}>
          <option value="http">HTTP/HTTPS</option>
          <option value="socks5">SOCKS5</option>
        </select>
        <input className="input" placeholder="host (optional)" value={host} onChange={e => setHost(e.target.value)} />
        <input className="input" placeholder="port (optional)" value={port} onChange={e => setPort(e.target.value)} style={{ width: 90 }} />
      </div>
      {curl && <p className="mono" style={{ fontSize: 12, color: '#818cf8', margin: '12px 0' }}>{curl}</p>}
      {lines.length > 0 && (
        <>
          <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 8 }}>
            <button className="btn btn-ghost" onClick={copy}>Copy all</button>
          </div>
          <textarea className="input" readOnly value={lines.join('\n')} style={{ width: '100%', minHeight: 240, fontFamily: 'ui-monospace, monospace', fontSize: 12 }} />
        </>
      )}
    </Layout>
  )
}
