import { type ReactNode } from 'react'

interface BadgeProps {
  children: ReactNode
  variant?: 'success' | 'warning' | 'danger' | 'info' | 'default'
}

const variants = {
  success: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 border-emerald-500/30',
  warning: 'bg-amber-500/15 text-amber-700 dark:text-amber-300 border-amber-500/30',
  danger: 'bg-red-500/15 text-red-700 dark:text-red-300 border-red-500/30',
  info: 'bg-blue-500/15 text-blue-700 dark:text-blue-300 border-blue-500/30',
  default: 'bg-card text-text-secondary border-card-border',
}

export function Badge({ children, variant = 'default' }: BadgeProps) {
  return (
    <span
      className={`
        inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium
        ${variants[variant]}
      `}
    >
      {children}
    </span>
  )
}
