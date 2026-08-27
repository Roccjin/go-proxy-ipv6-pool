import { useState, useEffect, useCallback, type ComponentType } from 'react'
import {
  Activity,
  Ban,
  EthernetPort,
  Gauge,
  Globe,
  KeyRound,
  Languages,
  LoaderCircle,
  LogOut,
  Timer,
  Users,
} from 'lucide-react'
import { api, Overview as OverviewData } from './api'
import OverviewPanel from './components/Overview'
import DomainRules from './components/DomainRules'
import ProxyUsers from './components/ProxyUsers'
import Sessions from './components/Sessions'
import BannedIPs from './components/BannedIPs'
import TrafficLog from './components/TrafficLog'
import Ports from './components/Ports'
import Login from './components/Login'
import Generator from './components/Generator'
import HexField from './components/HexField'
import { LanguageSwitcher, useI18n } from './i18n'

type Tab = 'overview' | 'domains' | 'users' | 'generator' | 'sessions' | 'banned' | 'traffic' | 'ports'

const NAV: { key: Tab; icon: ComponentType<{ size?: number; strokeWidth?: number }> }[] = [
  { key: 'overview', icon: Gauge },
  { key: 'users', icon: Users },
  { key: 'generator', icon: KeyRound },
  { key: 'sessions', icon: Timer },
  { key: 'traffic', icon: Activity },
  { key: 'ports', icon: EthernetPort },
  { key: 'banned', icon: Ban },
  { key: 'domains', icon: Globe },
]

export default function App() {
  const [authed, setAuthed] = useState<boolean | null>(null)
  const [tab, setTab] = useState<Tab>('overview')
  const [overview, setOverview] = useState<OverviewData | null>(null)
  const [online, setOnline] = useState(false)
  const [tick, setTick] = useState(0)
  const [flashMsg, setFlashMsg] = useState('')
  const { m, t } = useI18n()

  useEffect(() => {
    api.overview().then(() => setAuthed(true)).catch(() => setAuthed(false))
  }, [])

  useEffect(() => {
    const handler = () => setAuthed(false)
    window.addEventListener('auth-expired', handler)
    return () => window.removeEventListener('auth-expired', handler)
  }, [])

  const refresh = useCallback(async () => {
    try {
      const data = await api.overview()
      setOverview(data)
      setOnline(true)
      setTick(0)
    } catch {
      setOnline(false)
    }
  }, [])

  useEffect(() => {
    if (!authed) return
    refresh()
    const id = setInterval(refresh, 3000)
    return () => clearInterval(id)
  }, [refresh, authed])

  useEffect(() => {
    const id = setInterval(() => setTick(n => n + 1), 1000)
    return () => clearInterval(id)
  }, [])

  const flash = (msg: string) => {
    setFlashMsg(msg)
    setTimeout(() => setFlashMsg(''), 2000)
  }

  const handleLogout = async () => {
    try { await api.logout() } catch {}
    setAuthed(false)
  }

  const labels: Record<Tab, string> = {
    overview: m.nav.overview,
    domains: m.nav.domains,
    users: m.nav.users,
    generator: m.nav.generator,
    sessions: m.nav.sessions,
    banned: m.nav.banned,
    traffic: m.nav.traffic,
    ports: m.nav.ports,
  }

  return (
    <>
      <HexField />
      <div className="grain" />

      {authed === null && (
        <div className="boot">
          <LoaderCircle className="spin" size={22} strokeWidth={1.5} />
          <span>{m.app.boot}</span>
        </div>
      )}

      {authed === false && <Login onLogin={() => { setAuthed(true); refresh() }} />}

      {authed && (
        <div className="app-root">
          <aside className="rail">
            <div className="rail-mark">{m.app.unit}</div>
            <nav className="rail-nav">
              {NAV.map(item => {
                const Icon = item.icon
                return (
                  <button
                    key={item.key}
                    className={`rail-btn ${tab === item.key ? 'active' : ''}`}
                    onClick={() => setTab(item.key)}
                    aria-label={labels[item.key]}
                  >
                    <Icon size={18} strokeWidth={1.6} />
                    <span className="tip">{labels[item.key]}</span>
                  </button>
                )
              })}
            </nav>
            <div className="rail-foot">
              <button className="rail-btn" onClick={handleLogout} aria-label={m.app.logout}>
                <LogOut size={18} strokeWidth={1.6} />
                <span className="tip">{m.app.logout}</span>
              </button>
            </div>
          </aside>

          <div className="stage">
            <header className="mast">
              <div className="mast-ghost">{m.app.unit}</div>
              <div>
                <div className="mast-kicker">{m.app.kicker}</div>
                <h1 className="mast-title">{m.app.title}</h1>
                <div className="mast-sub">
                  {overview?.prefix ? `${overview.prefix}/${overview.prefix_bits}` : 'IPv6'}
                  {' · '}
                  {overview?.max_ipv6 ?? '—'}
                </div>
              </div>
              <div className="mast-meta">
                <span className={`pulse ${online ? 'on' : ''}`} />
                <span>{online ? m.status.online : m.status.offline}</span>
                <span>{m.status.uptime} {overview?.uptime_str ?? '—'}</span>
                <span>{t('status.refreshAgo', { n: tick })}</span>
                <Languages size={14} strokeWidth={1.6} />
                <LanguageSwitcher />
              </div>
            </header>

            <div className="view" key={tab}>
              {tab === 'overview' && <OverviewPanel data={overview} onRefresh={refresh} />}
              {tab === 'domains' && <DomainRules flash={flash} />}
              {tab === 'users' && <ProxyUsers flash={flash} />}
              {tab === 'generator' && <Generator flash={flash} />}
              {tab === 'sessions' && <Sessions flash={flash} />}
              {tab === 'banned' && <BannedIPs flash={flash} />}
              {tab === 'traffic' && <TrafficLog />}
              {tab === 'ports' && <Ports flash={flash} />}
            </div>
          </div>
        </div>
      )}

      {flashMsg && (
        <div className="flash">
          <Activity size={14} strokeWidth={1.75} />
          {flashMsg}
        </div>
      )}
    </>
  )
}
