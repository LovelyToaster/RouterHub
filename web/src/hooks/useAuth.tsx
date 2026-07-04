import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'
import { getToken, setToken, clearToken } from '@/api/client'

interface AuthContextType {
  token: string | null
  isAuthenticated: boolean
  setAuth: (token: string) => void
  clearAuth: () => void
}

const AuthContext = createContext<AuthContextType | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(() => getToken())

  const setAuth = useCallback((newToken: string) => {
    setToken(newToken)
    setTokenState(newToken)
  }, [])

  const clearAuth = useCallback(() => {
    clearToken()
    setTokenState(null)
  }, [])

  return (
    <AuthContext.Provider
      value={{
        token,
        isAuthenticated: !!token,
        setAuth,
        clearAuth,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextType {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return ctx
}
