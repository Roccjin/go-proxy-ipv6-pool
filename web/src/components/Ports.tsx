import { useState, useEffect } from 'react'
import { EthernetPort, Plus, RefreshCw, Square } from 'lucide-react'
import { api, PortInfo } from '../api'
import { useI18n } from '../i18n'
import Layout from './Layout'
import { Btn, Empty } from './ui'

function PortTable({
  title,
  ports,
  empty,
  onStop,
}: {
  title: string
  ports: PortInfo[]
  empty: string
  onStop: (addr: string) => void
}) {
  const { m } = useI18n()
  return (
    <>
      <div className="section-label">{title}</div>
      <table className="data-table">
        <thead><tr><th>{m.ports.address}</th><th>{m.ports.status}</th><th>{m.ports.main}</th><th>{m.common.action}</th></tr></thead>
        <tbody>
          {ports.length === 0 && (
            <tr><td colSpan={4}><Empty icon={EthernetPort} text={empty} /></td></tr>
          )}
          {ports.map(p => (
            <tr key={p.addr}>
              <td className="mono">{p.addr}</td>
              <td>
                <span className={`badge ${p.running ? 'ion' : 'fail'}`}>
                  {p.running ? m.ports.running : m.ports.stopped}
                </span>
              </td>
              <td>{p.is_main ? m.common.yes : '—'}</td>
              <td>
                {!p.is_main && p.running && (
                  <Btn variant="danger" icon={Square} onClick={() => onStop(p.addr)}>{m.common.stop}</Btn>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}

export default function Ports({ flash }: { flash: (msg: string) => void }) {
  const { m, t } = useI18n()
  const [ports, setPorts] = useState<PortInfo[]>([])
  const [expanding, setExpanding] = useState(false)

  const load = async () => {
    try {
      const data = await api.ports()
      setPorts(data.ports ?? [])
    } catch {}
  }

  useEffect(() => { load() }, [])

  const handleExpand = async () => {
    setExpanding(true)
    try {
      const res = await api.expandPorts()
      flash(t('ports.added', { n: res.added?.length ?? 0 }))
      load()
    } catch {
      flash(m.ports.expandFailed)
    } finally {
      setExpanding(false)
    }
  }

  const handleStop = async (addr: string) => {
    try {
      await api.stopPort(addr)
      flash(t('ports.stoppedAddr', { addr }))
      load()
    } catch {
      flash(m.ports.stopFailed)
    }
  }

  const httpPorts = ports.filter(p => p.type === 'http')
  const socksPorts = ports.filter(p => p.type === 'socks5')

  return (
    <Layout title={m.ports.title} actions={
      <>
        <Btn icon={RefreshCw} onClick={load}>{m.common.refresh}</Btn>
        <Btn variant="primary" icon={Plus} onClick={handleExpand} disabled={expanding}>
          {expanding ? m.ports.expanding : m.ports.expand}
        </Btn>
      </>
    }>
      <PortTable title={m.ports.http} ports={httpPorts} empty={m.ports.emptyHttp} onStop={handleStop} />
      <PortTable title={m.ports.socks} ports={socksPorts} empty={m.ports.emptySocks} onStop={handleStop} />
    </Layout>
  )
}
