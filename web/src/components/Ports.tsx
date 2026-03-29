import { useState, useEffect } from 'react'
import { api, PortInfo } from '../api'
import Layout from './Layout'

export default function Ports({ flash }: { flash: (msg: string) => void }) {
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
      flash(`Added ${res.added?.length ?? 0} ports`)
      load()
    } catch {
      flash('Failed to expand ports')
    } finally {
      setExpanding(false)
    }
  }

  const handleStop = async (addr: string) => {
    try {
      await api.stopPort(addr)
      flash(`Stopped ${addr}`)
      load()
    } catch {
      flash('Failed to stop port')
    }
  }

  const httpPorts = ports.filter(p => p.type === 'http')
  const socksPorts = ports.filter(p => p.type === 'socks5')

  return (
    <Layout title="Port Management" actions={
      <>
        <button className="btn btn-ghost" onClick={load}>Refresh</button>
        <button className="btn btn-primary" onClick={handleExpand} disabled={expanding}>
          {expanding ? 'Expanding...' : 'Expand +5 Pairs'}
        </button>
      </>
    }>
      <h3 style={{ color: '#e2e8f0', fontSize: 14, marginBottom: 12 }}>HTTP Proxy Ports</h3>
      <table className="data-table">
        <thead><tr><th>Address</th><th>Status</th><th>Main</th><th>Action</th></tr></thead>
        <tbody>
          {httpPorts.length === 0 && <tr><td colSpan={4} style={{ color: '#475569' }}>No HTTP ports</td></tr>}
          {httpPorts.map(p => (
            <tr key={p.addr}>
              <td className="mono">{p.addr}</td>
              <td style={{ color: p.running ? '#22c55e' : '#ef4444' }}>{p.running ? 'Running' : 'Stopped'}</td>
              <td>{p.is_main ? 'Yes' : '—'}</td>
              <td>
                {!p.is_main && p.running && (
                  <button className="btn btn-danger" onClick={() => handleStop(p.addr)}>Stop</button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <div style={{ marginTop: 24 }}>
        <h3 style={{ color: '#e2e8f0', fontSize: 14, marginBottom: 12 }}>SOCKS5 Proxy Ports</h3>
        <table className="data-table">
          <thead><tr><th>Address</th><th>Status</th><th>Main</th><th>Action</th></tr></thead>
          <tbody>
            {socksPorts.length === 0 && <tr><td colSpan={4} style={{ color: '#475569' }}>No SOCKS5 ports</td></tr>}
            {socksPorts.map(p => (
              <tr key={p.addr}>
                <td className="mono">{p.addr}</td>
                <td style={{ color: p.running ? '#22c55e' : '#ef4444' }}>{p.running ? 'Running' : 'Stopped'}</td>
                <td>{p.is_main ? 'Yes' : '—'}</td>
                <td>
                  {!p.is_main && p.running && (
                    <button className="btn btn-danger" onClick={() => handleStop(p.addr)}>Stop</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Layout>
  )
}
