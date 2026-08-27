import { LucideIcon } from 'lucide-react'
import { ButtonHTMLAttributes } from 'react'

export function Btn({
  variant = 'ghost',
  icon: Icon,
  children,
  className,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'ghost' | 'danger' | 'ion'
  icon?: LucideIcon
}) {
  return (
    <button className={`btn btn-${variant}${className ? ' ' + className : ''}`} {...rest}>
      {Icon && <Icon size={14} strokeWidth={1.75} />}
      {children}
    </button>
  )
}

export function Empty({ icon: Icon, text }: { icon: LucideIcon; text: string }) {
  return (
    <div className="empty">
      <Icon size={28} strokeWidth={1.25} />
      <span>{text}</span>
    </div>
  )
}
