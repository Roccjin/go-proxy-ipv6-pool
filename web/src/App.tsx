import { useState, useEffect, useCallback } from 'react'
import { api, Overview as OverviewData } from './api'
import StatusBar from './components/StatusBar'
import OverviewPanel from './components/Overview'
import DomainRules from './components/DomainRules'
import ProxyUsers from './components/ProxyUsers'
import Sessions from './components/Sessions'
import BannedIPs from './components/BannedIPs'
import TrafficLog from './components/TrafficLog'
import Ports from './components/Ports'
import Login from './components/Login'
import Generator from './components/Generator'

type Tab = 'overview' | 'domains' | 'users' | 'generator' | 'sessions' | 'banned' | 'traffic' | 'ports'

export default function App() {
  const [authed, setAuthed] = useState<boolean | null>(null)
  const [tab, setTab] = useState<Tab>('overview')
  const [overview, setOverview] = useState<OverviewData | null>(null)
  const [online, setOnline] = useState(false)
  const [tick, setTick] = useState(0)
  const [flashMsg, setFlashMsg] = useState('')

  // Check auth on mount by hitting a protected endpoint
  useEffect(() => {
    api.overview().then(() => setAuthed(true)).catch(() => setAuthed(false))
  }, [])

  // Listen for 401 events from api layer
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
    const id = setInterval(() => setTick(t => t + 1), 1000)
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

  if (authed === null) return null // loading
  if (!authed) return <Login onLogin={() => { setAuthed(true); refresh() }} />

  const tabs: { key: Tab; label: string }[] = [
    { key: 'overview', label: 'Overview' },
    { key: 'domains', label: 'Domain Rules' },
    { key: 'users', label: 'Accounts' },
    { key: 'generator', label: 'Generator' },
    { key: 'sessions', label: 'Sessions' },
    { key: 'banned', label: 'Banned IPs' },
    { key: 'traffic', label: 'Traffic Log' },
    { key: 'ports', label: 'Ports' },
  ]

  return (
    <>
      <StatusBar data={overview} tick={tick} online={online} />
      <div className="tabs">
        {tabs.map(t => (
          <button key={t.key} className={`tab ${tab === t.key ? 'active' : ''}`} onClick={() => setTab(t.key)}>
            {t.label}
          </button>
        ))}
        <button className="btn btn-ghost logout-btn" onClick={handleLogout}>Logout</button>
      </div>
      {tab === 'overview' && <OverviewPanel data={overview} onRefresh={refresh} />}
      {tab === 'domains' && <DomainRules flash={flash} />}
      {tab === 'users' && <ProxyUsers flash={flash} />}
      {tab === 'generator' && <Generator flash={flash} />}
      {tab === 'sessions' && <Sessions flash={flash} />}
      {tab === 'banned' && <BannedIPs flash={flash} />}
      {tab === 'traffic' && <TrafficLog />}
      {tab === 'ports' && <Ports flash={flash} />}
      {flashMsg && <div className="flash">{flashMsg}</div>}
    </>
  )
}
