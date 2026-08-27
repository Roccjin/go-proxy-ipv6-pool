import { useState } from 'react'
import { ArrowUpRight, Languages, Lock, User } from 'lucide-react'
import { api } from '../api'
import { LanguageSwitcher, useI18n } from '../i18n'

export default function Login({ onLogin }: { onLogin: () => void }) {
  const { m } = useI18n()
  const [user, setUser] = useState('')
  const [pass, setPass] = useState('')
  const [error, setError] = useState(false)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(false)
    setLoading(true)
    try {
      await api.login(user, pass)
      onLogin()
    } catch {
      setError(true)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-overlay">
      <div className="login-lang">
        <Languages size={14} strokeWidth={1.6} />
        <LanguageSwitcher />
      </div>
      <div className="login-left">
        <div className="login-copy">
          <div className="mast-kicker">{m.app.kicker}</div>
          <h1>{m.login.title}</h1>
          <p>{m.login.subtitle}</p>
        </div>
        <div className="login-giant">{m.app.unit}</div>
      </div>
      <form className="login-card" onSubmit={handleSubmit}>
        <div className="login-kicker">{m.login.kicker}</div>
        <h2 className="login-title">{m.app.title}</h2>
        <p className="login-subtitle">{m.login.subtitle}</p>
        <label className="field">
          <User size={14} strokeWidth={1.6} />
          <input
            className="input"
            placeholder={m.login.username}
            value={user}
            onChange={e => setUser(e.target.value)}
            autoFocus
          />
        </label>
        <label className="field">
          <Lock size={14} strokeWidth={1.6} />
          <input
            className="input"
            type="password"
            placeholder={m.login.password}
            value={pass}
            onChange={e => setPass(e.target.value)}
          />
        </label>
        {error && <div className="login-error">{m.login.invalid}</div>}
        <button className="btn btn-primary login-btn" type="submit" disabled={loading}>
          {loading ? m.login.signingIn : m.login.signIn}
          <ArrowUpRight size={16} strokeWidth={1.75} />
        </button>
      </form>
    </div>
  )
}
