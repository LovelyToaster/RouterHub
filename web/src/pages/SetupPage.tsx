import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import { ShieldCheck, Eye, EyeOff } from 'lucide-react'
import { GlassCard } from '@/components/ui/GlassCard'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { initSetup, login } from '@/api/client'
import { useAuth } from '@/hooks/useAuth'

export function SetupPage() {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const { setAuth } = useAuth()
  const navigate = useNavigate()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (!username || !password) {
      setError(t('setup.fillRequired'))
      return
    }
    if (password.length < 6) {
      setError(t('setup.passwordTooShort'))
      return
    }
    if (password !== confirmPassword) {
      setError(t('setup.passwordMismatch'))
      return
    }

    setLoading(true)
    try {
      await initSetup(username, password)
      // Auto-login and jump to the dashboard.
      const res = await login(username, password)
      setAuth(res.token)
      navigate('/app/dashboard', { replace: true })
    } catch (err: any) {
      setError(err.message || t('setup.initFailed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-surface p-4">
      <div className="w-full max-w-md">
        <GlassCard className="p-8">
          <div className="text-center mb-8">
            <div className="inline-flex p-3 rounded-2xl bg-accent/20 mb-4">
              <ShieldCheck className="w-8 h-8 text-accent-light" />
            </div>
            <h1 className="text-2xl font-bold text-text-primary">{t('setup.title')}</h1>
            <p className="text-text-secondary mt-2 text-sm">
              {t('setup.subtitle')}
            </p>
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            <Input
              label={t('setup.username')}
              placeholder={t('setup.usernamePlaceholder')}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoFocus
            />

            <div className="relative">
              <Input
                label={t('setup.password')}
                type={showPassword ? 'text' : 'password'}
                placeholder={t('setup.passwordPlaceholder')}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                className={password ? 'pr-10' : ''}
              />
              {password && (
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute right-3 bottom-[13px] text-text-muted hover:text-text-primary transition-colors"
                >
                  {showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              )}
            </div>

            <div className="relative">
              <Input
                label={t('setup.confirmPassword')}
                type={showConfirmPassword ? 'text' : 'password'}
                placeholder={t('setup.confirmPasswordPlaceholder')}
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                autoComplete="new-password"
                className={confirmPassword ? 'pr-10' : ''}
              />
              {confirmPassword && (
                <button
                  type="button"
                  onClick={() => setShowConfirmPassword(!showConfirmPassword)}
                  className="absolute right-3 bottom-[13px] text-text-muted hover:text-text-primary transition-colors"
                >
                  {showConfirmPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              )}
            </div>

            {error && (
              <p className="text-sm text-red-400 bg-red-500/10 rounded-lg px-3 py-2">
                {error}
              </p>
            )}

            <Button type="submit" loading={loading} className="w-full" size="lg">
              {t('setup.submit')}
            </Button>
          </form>
        </GlassCard>
      </div>
    </div>
  )
}
