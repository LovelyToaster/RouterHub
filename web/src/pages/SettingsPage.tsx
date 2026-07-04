import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Palette, Info, Sliders, Clock } from 'lucide-react'
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

const KNOWN_APP_KEYS = new Set(['app.language', 'app.theme'])

export function SettingsPage() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const { tz } = useUserTimezone()
  const [formValues, setFormValues] = useState<Record<string, string>>({})

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

  useEffect(() => {
    if (settings) {
      setFormValues((prev) => {
        const next = { ...prev }
        for (const s of settings) {
          if (!(s.key in prev)) {
            next[s.key] = s.value
          }
        }
        return next
      })
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

  const handleChange = (key: string, value: string) => {
    setFormValues((prev) => ({ ...prev, [key]: value }))
  }

  const handleBlur = (key: string, value: string) => {
    // Only push when value actually differs from the persisted one.
    const persisted = settings?.find((s) => s.key === key)?.value ?? ''
    if (value === persisted) return
    updateMut.mutate({ [key]: value })
  }

  const handleTimezoneChange = (v: string) => {
    setTimezone(v)
    updateMeMut.mutate(v)
  }

  const unknownSettings = settings?.filter((s) => !KNOWN_APP_KEYS.has(s.key)) ?? []

  // Build the dropdown options: ensure the currently effective tz is present.
  const tzOptions = (() => {
    const list = [...TIMEZONE_OPTIONS]
    if (effectiveTz && !list.includes(effectiveTz)) {
      list.unshift(effectiveTz)
    }
    return list.map((tz) => ({ value: tz, label: formatTimezoneOption(tz) }))
  })()

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-text-primary">{t('settings.title')}</h1>
        <p className="text-text-secondary mt-1">{t('settings.description')}</p>
      </div>

      {isLoading ? (
        <div className="space-y-4">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="h-32 rounded-2xl bg-card animate-pulse" />
          ))}
        </div>
      ) : (
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

          {/* Advanced Settings */}
          {unknownSettings.length > 0 && (
            <GlassCard>
              <GlassCardHeader
                title={t('settings.advanced')}
                description={t('settings.advancedDescription')}
                action={<Sliders className="w-5 h-5 text-accent" />}
              />
              <div className="p-6">
                <div className="space-y-5">
                  {unknownSettings.map((setting) => (
                    <div key={setting.key}>
                      <Input
                        label={setting.key}
                        value={formValues[setting.key] ?? ''}
                        onChange={(e) => handleChange(setting.key, e.target.value)}
                        onBlur={(e) => handleBlur(setting.key, e.target.value)}
                        placeholder={setting.key}
                      />
                    </div>
                  ))}
                </div>
              </div>
            </GlassCard>
          )}
        </>
      )}
    </div>
  )
}
