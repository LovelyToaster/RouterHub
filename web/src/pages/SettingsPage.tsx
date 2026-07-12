import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Palette, Info, Clock } from 'lucide-react'
import { GlassCard, GlassCardHeader } from '@/components/ui/GlassCard'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { AppearanceControls } from '@/components/AppearanceControls'
import { getSettings, updateSettings, getMe, updateMe, getSystemInfo } from '@/api/client'
import {
  TIMEZONE_OPTIONS,
  formatTimezoneOption,
  getBrowserTimezone,
} from '@/lib/timezone'
import { useUserTimezone } from '@/hooks/useUserTimezone'

export function SettingsPage() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const { tz } = useUserTimezone()
  const [activeTab, setActiveTab] = useState<'general' | 'logs'>('general')
  const [bodyCapture, setBodyCapture] = useState<string>('error')
  const [retentionDays, setRetentionDays] = useState<string>('0')

  const { data: settings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: getSettings,
  })

  const { data: me } = useQuery({
    queryKey: ['me'],
    queryFn: getMe,
  })

  const { data: systemInfo } = useQuery({
    queryKey: ['system-info'],
    queryFn: getSystemInfo,
    staleTime: Infinity,
  })

  const [timezone, setTimezone] = useState<string>('')

  // Effective timezone: stored value, else the browser's detected IANA.
  const effectiveTz = timezone || me?.timezone || getBrowserTimezone()

  useEffect(() => {
    if (me) setTimezone(me.timezone ?? '')
  }, [me])

  // Initialize log settings from persisted values (only when present).
  useEffect(() => {
    if (settings) {
      const v = settings.find((s) => s.key === 'log.body_capture')?.value
      if (v) setBodyCapture(v)
    }
  }, [settings])

  useEffect(() => {
    if (settings) {
      const v = settings.find((s) => s.key === 'log.request_log_retention_days')?.value
      if (v !== undefined) setRetentionDays(v)
    }
  }, [settings])

  const updateMut = useMutation({
    mutationFn: (data: Record<string, string>) => updateSettings(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const updateMeMut = useMutation({
    mutationFn: (tz: string) => updateMe({ timezone: tz }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['me'] })
    },
  })

  const saveSetting = (key: string, value: string) => {
    updateMut.mutate({ [key]: value })
  }

  const handleTimezoneChange = (v: string) => {
    setTimezone(v)
    updateMeMut.mutate(v)
  }

  const handleRetentionBlur = () => {
    const n = Math.max(0, Math.floor(Number(retentionDays) || 0))
    setRetentionDays(String(n))
    saveSetting('log.request_log_retention_days', String(n))
  }

  // Build the dropdown options: ensure the currently effective tz is present.
  const tzOptions = (() => {
    const list = [...TIMEZONE_OPTIONS]
    if (effectiveTz && !list.includes(effectiveTz)) {
      list.unshift(effectiveTz)
    }
    return list.map((tz) => ({ value: tz, label: formatTimezoneOption(tz) }))
  })()

  const bodyCaptureOptions = [
    { value: 'none', label: t('settings.bodyCaptureNone') },
    { value: 'error', label: t('settings.bodyCaptureError') },
    { value: 'all', label: t('settings.bodyCaptureAll') },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-text-primary">{t('settings.title')}</h1>
        <p className="text-text-secondary mt-1">{t('settings.description')}</p>
      </div>

      {/* Tabs */}
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => setActiveTab('general')}
          className={`px-4 py-2 rounded-xl border text-sm font-medium transition-colors ${
            activeTab === 'general'
              ? 'bg-accent/20 text-accent-light border-accent/30'
              : 'text-text-muted hover:text-text-primary hover:bg-card border-card-border'
          }`}
        >
          {t('settings.tabGeneral')}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('logs')}
          className={`px-4 py-2 rounded-xl border text-sm font-medium transition-colors ${
            activeTab === 'logs'
              ? 'bg-accent/20 text-accent-light border-accent/30'
              : 'text-text-muted hover:text-text-primary hover:bg-card border-card-border'
          }`}
        >
          {t('settings.tabLogs')}
        </button>
      </div>

      {isLoading ? (
        <div className="space-y-4">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-32 rounded-2xl bg-card animate-pulse" />
          ))}
        </div>
      ) : (
        <>
          {activeTab === 'general' && (
            <>
              {/* Appearance */}
              <GlassCard>
                <GlassCardHeader
                  title={t('settings.appearance')}
                  description={t('settings.appearanceDescription')}
                  action={<Palette className="w-5 h-5 text-accent" />}
                />
                <div className="p-6">
                  <AppearanceControls />
                </div>
              </GlassCard>

              {/* Region / Timezone */}
              <GlassCard>
                <GlassCardHeader
                  title={t('settings.region')}
                  description={t('settings.regionDescription')}
                  action={<Clock className="w-5 h-5 text-accent" />}
                />
                <div className="p-6 max-w-sm">
                  <Select
                    label={t('settings.timezone')}
                    value={effectiveTz}
                    onChange={(e) => handleTimezoneChange(e.target.value)}
                    options={tzOptions}
                  />
                </div>
              </GlassCard>

              {/* System Info */}
              <GlassCard>
                <GlassCardHeader
                  title={t('settings.systemInfo')}
                  description={t('settings.systemInfoDescription')}
                  action={<Info className="w-5 h-5 text-accent" />}
                />
                <div className="p-6">
                  <div className="space-y-2 text-sm">
                    <div className="flex justify-between">
                      <span className="text-text-secondary">RouterHub</span>
                      <span className="text-text-primary">v{systemInfo?.app_version ?? '—'}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-text-secondary">Go</span>
                      <span className="text-text-primary">{systemInfo?.go_version ?? '—'}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-text-secondary">{t('settings.platform')}</span>
                      <span className="text-text-primary">{systemInfo?.platform ?? '—'}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-text-secondary">{t('settings.buildDate')}</span>
                      <span className="text-text-primary font-mono">
                        {systemInfo?.build_date
                          ? new Date(systemInfo.build_date).toLocaleString(
                              i18n.language === 'zh-CN' ? 'zh-CN' : 'en-US',
                              {
                                year: 'numeric',
                                month: '2-digit',
                                day: '2-digit',
                                hour: '2-digit',
                                minute: '2-digit',
                                second: '2-digit',
                                timeZone: tz,
                              },
                            )
                          : '—'}
                      </span>
                    </div>
                  </div>
                </div>
              </GlassCard>

            </>
          )}

          {activeTab === 'logs' && (
            <>
              {/* Log options */}
              <GlassCard>
                <GlassCardHeader
                  title={t('settings.logOptions')}
                  description={t('settings.logOptionsDescription')}
                />
                <div className="p-6 max-w-sm space-y-2">
                  <Select
                    label={t('settings.bodyCapture')}
                    value={bodyCapture}
                    onChange={(e) => {
                      setBodyCapture(e.target.value)
                      saveSetting('log.body_capture', e.target.value)
                    }}
                    options={bodyCaptureOptions}
                  />
                  <p className="text-xs text-text-muted">{t('settings.bodyCaptureDescription')}</p>
                </div>
              </GlassCard>

              {/* Log retention */}
              <GlassCard>
                <GlassCardHeader
                  title={t('settings.retention')}
                  description={t('settings.retentionDescription')}
                />
                <div className="p-6 max-w-sm space-y-2">
                  <Input
                    label={t('settings.requestLogRetention')}
                    type="number"
                    min={0}
                    value={retentionDays}
                    onChange={(e) => setRetentionDays(e.target.value)}
                    onBlur={handleRetentionBlur}
                  />
                  <p className="text-xs text-text-muted">
                    {t('settings.requestLogRetentionDescription')}
                  </p>
                </div>
              </GlassCard>
            </>
          )}
        </>
      )}
    </div>
  )
}
