import { type InputHTMLAttributes, forwardRef } from 'react'

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ label, error, className = '', ...props }, ref) => {
    return (
      <div className="space-y-1.5">
        {label && (
          <label className="block text-sm font-medium text-text-secondary">
            {label}
          </label>
        )}
        <input
          ref={ref}
          className={`
            w-full rounded-xl border bg-surface-light px-4 py-2.5 text-sm text-text-primary
            placeholder:text-text-muted
            transition-all duration-200
            focus:outline-none focus:ring-2 focus:ring-accent/50 focus:border-accent
            ${error ? 'border-red-500/50 focus:ring-red-500/50 focus:border-red-500' : 'border-surface-border hover:border-accent/30'}
            ${className}
          `}
          {...props}
        />
        {error && <p className="text-xs text-red-400">{error}</p>}
      </div>
    )
  },
)

Input.displayName = 'Input'
