import { ReactNode } from 'react'

export default function Layout({ title, children, actions }: { title: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="glass">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div className="glass-title">{title}</div>
        {actions}
      </div>
      {children}
    </div>
  )
}
