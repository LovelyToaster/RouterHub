import {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'
import { getSettings, updateSettings } from '@/api/client'

export type ThemeMode = 'light' | 'dark' | 'system'
export type Language = 'zh-CN' | 'en-US'

interface AppearanceContextType {
  language: Language
  theme: ThemeMode
  resolvedTheme: 'light' | 'dark'
  setLanguage: (lang: Language) => void
  setTheme: (theme: ThemeMode) => void
}

const AppearanceContext = createContext<AppearanceContextType | null>(null)

function getSystemTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined') return 'dark'
  return window.matchMedia('(prefers-color-scheme: light)').matches
    ? 'light'
    : 'dark'
}

function resolveTheme(theme: ThemeMode): 'light' | 'dark' {
  if (theme === 'system') return getSystemTheme()
  return theme
}

function applyThemeClass(resolved: 'light' | 'dark') {
  const root = document.documentElement
  // Freeze every transition/animation while we swap CSS variables, then thaw on
  // the next frame. Prevents the "hundred elements flicker" during theme switch.
  root.classList.add('theme-switching')
  root.classList.remove('light', 'dark')
  root.classList.add(resolved)
  // Force a synchronous style flush so the browser applies the new variables
  // while transitions are still frozen.
  // eslint-disable-next-line @typescript-eslint/no-unused-expressions
  root.offsetHeight
  // Two RAFs to make sure paint has committed before re-enabling transitions.
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      root.classList.remove('theme-switching')
    })
  })
}

export function AppearanceProvider({ children }: { children: ReactNode }) {
  const { i18n } = useTranslation()

  const [language, setLanguageState] = useState<Language>(() => {
    const stored = localStorage.getItem('app_language') as Language | null
    if (stored && (stored === 'zh-CN' || stored === 'en-US')) return stored
    const detected = i18n.language?.startsWith('en') ? 'en-US' : 'zh-CN'
    return detected as Language
  })

  const [theme, setThemeState] = useState<ThemeMode>(() => {
    const stored = localStorage.getItem('app_theme') as ThemeMode | null
    if (stored && (stored === 'light' || stored === 'dark' || stored === 'system')) return stored
    return 'dark'
  })

  const [resolvedTheme, setResolvedTheme] = useState<'light' | 'dark'>(() =>
    resolveTheme(theme),
  )

  // Apply theme class whenever resolved theme changes
  useEffect(() => {
    applyThemeClass(resolvedTheme)
  }, [resolvedTheme])

  // Listen for system theme changes when in 'system' mode
  useEffect(() => {
    if (theme !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: light)')
    const handler = () => setResolvedTheme(resolveTheme('system'))
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [theme])

  // Sync initial language to i18next
  useEffect(() => {
    if (i18n.language !== language) {
      i18n.changeLanguage(language)
    }
  }, [language, i18n])

  // Load settings on mount to sync with backend (skip on login/setup pages to avoid 401)
  useEffect(() => {
    const path = window.location.pathname
    if (path === '/login' || path === '/setup') return

    getSettings()
      .then((settings) => {
        for (const s of settings) {
          if (s.key === 'app.language' && (s.value === 'zh-CN' || s.value === 'en-US')) {
            setLanguageState(s.value as Language)
          }
          if (s.key === 'app.theme' && (s.value === 'light' || s.value === 'dark' || s.value === 'system')) {
            setThemeState(s.value as ThemeMode)
          }
        }
      })
      .catch(() => {
        // ignore — settings may not be available yet
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const setLanguage = useCallback(
    (lang: Language) => {
      setLanguageState(lang)
      localStorage.setItem('app_language', lang)
      i18n.changeLanguage(lang)
      // Persist to backend (fire-and-forget)
      updateSettings({ 'app.language': lang }).catch(() => {})
    },
    [i18n],
  )

  const setTheme = useCallback(
    (newTheme: ThemeMode) => {
      setThemeState(newTheme)
      localStorage.setItem('app_theme', newTheme)
      const resolved = resolveTheme(newTheme)
      setResolvedTheme(resolved)
      applyThemeClass(resolved)
      // Persist to backend (fire-and-forget)
      updateSettings({ 'app.theme': newTheme }).catch(() => {})
    },
    [],
  )

  return (
    <AppearanceContext.Provider
      value={{
        language,
        theme,
        resolvedTheme,
        setLanguage,
        setTheme,
      }}
    >
      {children}
    </AppearanceContext.Provider>
  )
}

export function useAppearance(): AppearanceContextType {
  const ctx = useContext(AppearanceContext)
  if (!ctx) {
    throw new Error('useAppearance must be used within an AppearanceProvider')
  }
  return ctx
}
