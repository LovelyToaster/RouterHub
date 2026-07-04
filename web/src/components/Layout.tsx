import { useState } from 'react'
import { Outlet, NavLink, useNavigate, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import {
  LayoutDashboard,
  Activity,
  Boxes,
  Key,
  Settings,
  LogOut,
  Menu,
  X,
  Router,
} from 'lucide-react'
import { useAuth } from '@/hooks/useAuth'
import { logout as apiLogout } from '@/api/client'
import { AppearanceControls } from '@/components/AppearanceControls'
import { useUserTimezone } from '@/hooks/useUserTimezone'

const navItems = [
  { to: '/app/dashboard', icon: LayoutDashboard, labelKey: 'nav.dashboard' },
  { to: '/app/providers', icon: Boxes, labelKey: 'nav.providers' },
  { to: '/app/requests', icon: Activity, labelKey: 'nav.requests' },
  { to: '/app/api-keys', icon: Key, labelKey: 'nav.apiKeys' },
  { to: '/app/settings', icon: Settings, labelKey: 'nav.settings' },
]

export function Layout() {
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const { clearAuth } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const { t } = useTranslation()

  // Ensure the current user has a stored timezone; writes browser IANA on first login.
  useUserTimezone()

  const handleLogout = async () => {
    try {
      await apiLogout()
    } catch {
      // ignore
    }
    clearAuth()
    navigate('/login')
  }

  return (
    <div className="min-h-screen bg-surface">
      {/* Mobile sidebar overlay */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 bg-black/20 dark:bg-black/60 z-40 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={`
          fixed top-0 left-0 z-50 h-screen w-64
          bg-surface-light border-r border-card-border
          transform transition-transform duration-300 ease-out
          lg:translate-x-0
          ${sidebarOpen ? 'translate-x-0' : '-translate-x-full'}
        `}
      >
        <div className="flex flex-col h-full">
          {/* Logo */}
          <div className="flex items-center gap-3 px-6 py-5 border-b border-card-border">
            <div className="p-2 rounded-xl bg-accent">
              <Router className="w-5 h-5 text-white" />
            </div>
            <span className="text-lg font-bold text-text-primary">
              RouterHub
            </span>
          </div>

          {/* Nav */}
          <nav className="flex-1 px-3 py-4 space-y-1 overflow-y-auto">
            {navItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                onClick={() => setSidebarOpen(false)}
          className={({ isActive }) =>
                   `flex items-center gap-3 px-4 py-2.5 rounded-xl text-sm font-medium transition-all duration-200 ${
                     isActive
                       ? 'bg-accent/20 text-accent-light border border-accent/30'
                        : 'text-text-muted hover:text-text-primary hover:bg-card border border-transparent'
                   }`
                 }
              >
                <item.icon className="w-4 h-4" />
                {t(item.labelKey)}
              </NavLink>
            ))}
          </nav>

          {/* Appearance controls */}
          <div className="px-3 py-3 border-t border-card-border">
            <AppearanceControls compact placement="top" />
          </div>

          {/* Logout */}
          <div className="px-3 py-3 border-t border-card-border">
            <button
              onClick={handleLogout}
              className="flex items-center gap-3 px-4 py-2.5 rounded-xl text-sm font-medium text-text-muted hover:text-red-600 dark:hover:text-red-300 hover:bg-red-500/10 w-full transition-all duration-200"
            >
              <LogOut className="w-4 h-4" />
              {t('nav.logout')}
            </button>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <div className="lg:ml-64 min-h-screen">
        {/* Mobile top bar */}
        <header className="sticky top-0 z-30 bg-surface border-b border-card-border lg:hidden">
          <div className="flex items-center justify-between px-4 py-3">
            <button
              onClick={() => setSidebarOpen(!sidebarOpen)}
              className="p-2 rounded-xl text-text-muted hover:text-text-primary hover:bg-card transition-colors"
            >
              {sidebarOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
            </button>
            <div className="text-sm font-semibold text-text-primary">RouterHub</div>
            <AppearanceControls compact />
          </div>
        </header>

        {/* Page content */}
        <main className="p-4 md:p-6 lg:p-8">
          <div key={location.pathname} className="page-fade">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
