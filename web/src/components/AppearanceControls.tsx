import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAppearance } from '@/hooks/useAppearance'
import { Sun, Moon, Monitor, Globe, Check } from 'lucide-react'

const languageOptions = [
  { value: 'zh-CN', labelKey: 'appearance.languageZh' },
  { value: 'en-US', labelKey: 'appearance.languageEn' },
] as const

const themeOptions = [
  { value: 'light', labelKey: 'appearance.themeLight', icon: Sun },
  { value: 'dark', labelKey: 'appearance.themeDark', icon: Moon },
  { value: 'system', labelKey: 'appearance.themeSystem', icon: Monitor },
] as const

interface AppearanceControlsProps {
  compact?: boolean
  placement?: 'top' | 'bottom' | 'right'
}

function PopupMenu({
  children,
  onClose,
  placement = 'bottom',
}: {
  children: React.ReactNode
  onClose: () => void
  placement?: 'top' | 'bottom' | 'right'
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose()
      }
    }
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    // Delay to avoid the same click that opened it
    const timer = setTimeout(() => {
      document.addEventListener('click', handleClickOutside)
      document.addEventListener('keydown', handleEscape)
    }, 0)
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', handleClickOutside)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [onClose])

  return (
    <div
      ref={ref}
      role="menu"
      className={`absolute z-50 min-w-[160px] bg-surface-light border border-card-border rounded-xl shadow-xl p-1.5 ${
        placement === 'top'
          ? 'left-0 bottom-full mb-2'
          : placement === 'right'
            ? 'left-full bottom-0 ml-2'
            : 'right-0 top-full mt-2'
      }`}
    >
      {children}
    </div>
  )
}

export function AppearanceControls({ compact = false, placement = 'bottom' }: AppearanceControlsProps) {
  const { t } = useTranslation()
  const { language, theme, setLanguage, setTheme } = useAppearance()
  const [langOpen, setLangOpen] = useState(false)
  const [themeOpen, setThemeOpen] = useState(false)

  if (compact) {
    return (
      <div className="flex items-center gap-1 relative">
        {/* Language popup trigger */}
        <div className="relative">
          <button
            type="button"
            aria-haspopup="menu"
            aria-expanded={langOpen}
            onClick={() => { setLangOpen((o) => !o); setThemeOpen(false) }}
            className="p-1.5 rounded-lg text-text-muted hover:text-text-primary hover:bg-card transition-colors"
            title={t('appearance.chooseLanguage')}
          >
            <Globe className="w-4 h-4" />
          </button>
          {langOpen && (
            <PopupMenu onClose={() => setLangOpen(false)} placement={placement}>
              <div className="px-2 py-1 text-xs font-medium text-text-muted uppercase tracking-wider">
                {t('appearance.language')}
              </div>
              {languageOptions.map((opt) => (
                <button
                  key={opt.value}
                  onClick={() => { setLanguage(opt.value); setLangOpen(false) }}
                  role="menuitemradio"
                  aria-checked={language === opt.value}
                  className={`w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm transition-colors ${
                    language === opt.value
                      ? 'bg-accent/20 text-accent-light'
                      : 'text-text-primary hover:bg-card'
                  }`}
                >
                  <span className="flex-1 text-left">{t(opt.labelKey)}</span>
                  {language === opt.value && (
                    <Check className="w-3.5 h-3.5 text-accent-light" />
                  )}
                </button>
              ))}
            </PopupMenu>
          )}
        </div>

        {/* Theme popup trigger */}
        <div className="relative">
          <button
            type="button"
            aria-haspopup="menu"
            aria-expanded={themeOpen}
            onClick={() => { setThemeOpen((o) => !o); setLangOpen(false) }}
            className="p-1.5 rounded-lg text-text-muted hover:text-text-primary hover:bg-card transition-colors"
            title={t('appearance.chooseTheme')}
          >
            {theme === 'light' && <Sun className="w-4 h-4" />}
            {theme === 'dark' && <Moon className="w-4 h-4" />}
            {theme === 'system' && <Monitor className="w-4 h-4" />}
          </button>
          {themeOpen && (
            <PopupMenu onClose={() => setThemeOpen(false)} placement={placement}>
              <div className="px-2 py-1 text-xs font-medium text-text-muted uppercase tracking-wider">
                {t('appearance.theme')}
              </div>
              {themeOptions.map((opt) => {
                const Icon = opt.icon
                return (
                  <button
                    key={opt.value}
                    onClick={() => { setTheme(opt.value); setThemeOpen(false) }}
                    role="menuitemradio"
                    aria-checked={theme === opt.value}
                    className={`w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm transition-colors ${
                      theme === opt.value
                        ? 'bg-accent/20 text-accent-light'
                        : 'text-text-primary hover:bg-card'
                    }`}
                  >
                    <Icon className="w-4 h-4" />
                    <span className="flex-1 text-left">{t(opt.labelKey)}</span>
                    {theme === opt.value && (
                      <Check className="w-3.5 h-3.5 text-accent-light" />
                    )}
                  </button>
                )
              })}
            </PopupMenu>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Language */}
      <div>
        <label className="block text-sm font-medium text-text-secondary mb-1.5">
          {t('appearance.language')}
        </label>
        <div className="flex gap-2">
          {languageOptions.map((opt) => (
            <button
              key={opt.value}
              onClick={() => setLanguage(opt.value)}
              className={`flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium transition-all duration-200 border ${
                language === opt.value
                  ? 'bg-accent/20 text-accent-light border-accent/30'
                    : 'text-text-muted hover:text-text-primary hover:bg-card border-card-border'
              }`}
            >
              <Globe className="w-4 h-4" />
              {t(opt.labelKey)}
            </button>
          ))}
        </div>
      </div>

      {/* Theme */}
      <div>
        <label className="block text-sm font-medium text-text-secondary mb-1.5">
          {t('appearance.theme')}
        </label>
        <div className="flex gap-2">
          {themeOptions.map((opt) => {
            const Icon = opt.icon
            return (
              <button
                key={opt.value}
                onClick={() => setTheme(opt.value)}
                className={`flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-medium transition-all duration-200 border ${
                  theme === opt.value
                    ? 'bg-accent/20 text-accent-light border-accent/30'
                  : 'text-text-muted hover:text-text-primary hover:bg-card border-card-border'
                }`}
              >
                <Icon className="w-4 h-4" />
                {t(opt.labelKey)}
              </button>
            )
          })}
        </div>
      </div>
    </div>
  )
}
