import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthProvider, useAuth } from '@/hooks/useAuth'
import { AppearanceProvider } from '@/hooks/useAppearance'
import { Layout } from '@/components/Layout'
import { SetupPage } from '@/pages/SetupPage'
import { LoginPage } from '@/pages/LoginPage'
import { DashboardPage } from '@/pages/DashboardPage'
import { RequestsPage } from '@/pages/RequestsPage'
import { ProvidersPage } from '@/pages/ProvidersPage'
import { ApiKeysPage } from '@/pages/ApiKeysPage'
import { SettingsPage } from '@/pages/SettingsPage'
import { getSetupStatus } from '@/api/client'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 10000,
      refetchOnWindowFocus: false,
    },
  },
})

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated } = useAuth()
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

function SetupGuard({ children }: { children: React.ReactNode }) {
  const [checking, setChecking] = useState(true)
  const [initialized, setInitialized] = useState<boolean | null>(null)
  const { t } = useTranslation()

  useEffect(() => {
    getSetupStatus()
      .then((res) => setInitialized(res.initialized))
      .catch(() => setInitialized(true)) // If error, assume initialized
      .finally(() => setChecking(false))
  }, [])

  if (checking) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-surface">
        <div className="flex flex-col items-center gap-3">
          <div className="animate-spin h-8 w-8 border-2 border-accent border-t-transparent rounded-full" />
          <span className="text-sm text-text-secondary">{t('app.loading')}</span>
        </div>
      </div>
    )
  }

  if (initialized === false) {
    return <Navigate to="/setup" replace />
  }

  return <>{children}</>
}

function AppRoutes() {
  const { isAuthenticated } = useAuth()
  const [checkingInit, setCheckingInit] = useState(true)
  const [initialized, setInitialized] = useState<boolean | null>(null)
  const { t } = useTranslation()

  useEffect(() => {
    getSetupStatus()
      .then((res) => setInitialized(res.initialized))
      .catch(() => setInitialized(true))
      .finally(() => setCheckingInit(false))
  }, [])

  if (checkingInit) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-surface">
        <div className="flex flex-col items-center gap-3">
          <div className="animate-spin h-8 w-8 border-2 border-accent border-t-transparent rounded-full" />
          <span className="text-sm text-text-secondary">{t('app.loading')}</span>
        </div>
      </div>
    )
  }

  return (
    <Routes>
      <Route
        path="/setup"
        element={
          isAuthenticated && initialized ? (
            <Navigate to="/app/dashboard" replace />
          ) : !initialized ? (
            <SetupPage />
          ) : (
            <Navigate to="/login" replace />
          )
        }
      />
      <Route
        path="/login"
        element={
          isAuthenticated ? (
            <Navigate to="/app/dashboard" replace />
          ) : !initialized ? (
            <Navigate to="/setup" replace />
          ) : (
            <LoginPage />
          )
        }
      />
      <Route
        path="/app"
        element={
          <ProtectedRoute>
            <SetupGuard>
              <Layout />
            </SetupGuard>
          </ProtectedRoute>
        }
      >
        <Route index element={<Navigate to="/app/dashboard" replace />} />
        <Route path="dashboard" element={<DashboardPage />} />
        <Route path="requests" element={<RequestsPage />} />
        <Route path="providers" element={<ProvidersPage />} />
        <Route path="model-aliases" element={<Navigate to="/app/providers" replace />} />
        <Route path="api-keys" element={<ApiKeysPage />} />
        <Route path="settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/app/dashboard" replace />} />
    </Routes>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AuthProvider>
          <AppearanceProvider>
            <AppRoutes />
          </AppearanceProvider>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
