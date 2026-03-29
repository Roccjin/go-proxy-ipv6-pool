import { useState } from 'react'
import { api } from '../api'

export default function Login({ onLogin }: { onLogin: () => void }) {
  const [user, setUser] = useState('')
  const [pass, setPass] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await api.login(user, pass)
      onLogin()
    } catch {
      setError('Invalid credentials')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-overlay">
      <form className="login-card" onSubmit={handleSubmit}>
        <h2 className="login-title">IPv6 Proxy</h2>
        <p className="login-subtitle">Admin Console</p>
        <input
          className="input login-input"
          placeholder="Username"
          value={user}
          onChange={e => setUser(e.target.value)}
          autoFocus
        />
        <input
          className="input login-input"
          type="password"
          placeholder="Password"
          value={pass}
          onChange={e => setPass(e.target.value)}
        />
        {error && <div className="login-error">{error}</div>}
        <button className="btn btn-primary login-btn" type="submit" disabled={loading}>
          {loading ? 'Signing in...' : 'Sign In'}
        </button>
      </form>
    </div>
  )
}
