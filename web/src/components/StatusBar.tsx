import { Overview } from '../api'

export default function StatusBar({ data, tick, online }: { data: Overview | null; tick: number; online: boolean }) {
  return (
    <div className="header-bar">
      <div className="header-title">IPv6 Proxy Admin</div>
      <div className="header-meta">
        <span><span className={`status-dot ${online ? 'online' : 'offline'}`} />{online ? 'Online' : 'Offline'}</span>
        <span>Uptime: {data?.uptime_str ?? '-'}</span>
        <span>Refresh: {tick}s ago</span>
      </div>
    </div>
  )
}
