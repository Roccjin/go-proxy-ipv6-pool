import { ReactNode } from 'react'

export default function Layout({ title, children, actions }: { title: string; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="panel">
      <div className="panel-head">
        <h2 className="panel-title">{title}</h2>
        {actions && <div className="panel-actions">{actions}</div>}
      </div>
      {children}
    </section>
  )
}
