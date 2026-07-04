import { type ReactNode } from 'react'

interface GlassCardProps {
  children: ReactNode
  className?: string
  hover?: boolean
}

export function GlassCard({ children, className = '', hover = true }: GlassCardProps) {
  return (
    <div
      className={`
        rounded-2xl border border-card-border
        bg-card shadow-sm
        ${hover ? 'hover:border-accent/30' : ''}
        transition-colors duration-200
        ${className}
      `}
    >
      {children}
    </div>
  )
}

export function GlassCardHeader({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: ReactNode
}) {
  return (
    <div className="flex items-center justify-between px-6 py-4 border-b border-card-border">
      <div>
        <h3 className="text-lg font-semibold text-text-primary">{title}</h3>
        {description && (
          <p className="text-sm text-text-muted mt-0.5">{description}</p>
        )}
      </div>
      {action && <div>{action}</div>}
    </div>
  )
}
