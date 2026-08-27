import { useState, useEffect } from 'react'
import { Globe, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { api, DomainRule } from '../api'
import { useI18n } from '../i18n'
import Layout from './Layout'
import { Btn, Empty } from './ui'

export default function DomainRules({ flash }: { flash: (msg: string) => void }) {
  const { m } = useI18n()
  const [rules, setRules] = useState<DomainRule[]>([])
  const [domain, setDomain] = useState('')
  const [count, setCount] = useState(3)
  const [strategy, setStrategy] = useState(0)

  const strategyNames: Record<number, string> = { 0: m.domains.roundRobin, 1: m.domains.random }

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
    flash(m.domains.added)
    load()
  }

  const handleRemove = async (d: string) => {
    await api.removeDomainRule(d)
    flash(m.domains.removed)
    load()
  }

  return (
    <Layout title={m.domains.title} actions={<Btn icon={RefreshCw} onClick={load}>{m.common.refresh}</Btn>}>
      <table className="data-table">
        <thead>
          <tr><th>{m.domains.domain}</th><th>{m.domains.exitCount}</th><th>{m.domains.strategy}</th><th>{m.common.action}</th></tr>
        </thead>
        <tbody>
          {rules.length === 0 && (
            <tr><td colSpan={4}><Empty icon={Globe} text={m.domains.empty} /></td></tr>
          )}
          {rules.map(r => (
            <tr key={r.domain}>
              <td className="mono">{r.domain}</td>
              <td>{r.count}</td>
              <td>{strategyNames[r.strategy] ?? r.strategy}</td>
              <td><Btn variant="danger" icon={Trash2} onClick={() => handleRemove(r.domain)}>{m.common.remove}</Btn></td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="input-row">
        <input className="input" placeholder="domain.com" value={domain} onChange={e => setDomain(e.target.value)} />
        <input className="input" type="number" min={1} style={{ width: 70 }} value={count} onChange={e => setCount(+e.target.value)} />
        <select className="input" value={strategy} onChange={e => setStrategy(+e.target.value)}>
          <option value={0}>{m.domains.roundRobin}</option>
          <option value={1}>{m.domains.random}</option>
        </select>
        <Btn variant="primary" icon={Plus} onClick={handleAdd}>{m.domains.add}</Btn>
      </div>
    </Layout>
  )
}
