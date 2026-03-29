import { useState, useEffect } from 'react'
import { api, DomainRule } from '../api'
import Layout from './Layout'

const STRATEGY_NAMES: Record<number, string> = { 0: 'Round Robin', 1: 'Random' }

export default function DomainRules({ flash }: { flash: (msg: string) => void }) {
  const [rules, setRules] = useState<DomainRule[]>([])
  const [domain, setDomain] = useState('')
  const [count, setCount] = useState(3)
  const [strategy, setStrategy] = useState(0)

  const load = async () => {
    try {
      const res = await api.domainRules()
      setRules(res.rules ?? [])
    } catch {}
  }

  useEffect(() => { load() }, [])

  const handleAdd = async () => {
    if (!domain || count <= 0) return
    await api.addDomainRule(domain, count, strategy)
    setDomain('')
    flash('Rule added')
    load()
  }

  const handleRemove = async (d: string) => {
    await api.removeDomainRule(d)
    flash('Rule removed')
    load()
  }

  return (
    <Layout title="Domain Rules" actions={<button className="btn btn-ghost" onClick={load}>Refresh</button>}>
      <table className="data-table">
        <thead>
          <tr><th>Domain</th><th>Exit Count</th><th>Strategy</th><th>Action</th></tr>
        </thead>
        <tbody>
          {rules.length === 0 && <tr><td colSpan={4} style={{ color: '#475569' }}>No domain rules configured</td></tr>}
          {rules.map(r => (
            <tr key={r.domain}>
              <td className="mono">{r.domain}</td>
              <td>{r.count}</td>
              <td>{STRATEGY_NAMES[r.strategy] ?? r.strategy}</td>
              <td><button className="btn btn-danger" onClick={() => handleRemove(r.domain)}>Remove</button></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="input-row">
        <input className="input" placeholder="domain.com" value={domain} onChange={e => setDomain(e.target.value)} />
        <input className="input" type="number" min={1} style={{ width: 70 }} value={count} onChange={e => setCount(+e.target.value)} />
        <select className="input" value={strategy} onChange={e => setStrategy(+e.target.value)}>
          <option value={0}>Round Robin</option>
          <option value={1}>Random</option>
        </select>
        <button className="btn btn-primary" onClick={handleAdd}>Add Rule</button>
      </div>
    </Layout>
  )
}
