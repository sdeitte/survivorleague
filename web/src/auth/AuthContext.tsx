import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import * as api from '../api'
import type { User } from '../api'

interface AuthContextValue {
  user: User | null
  /** True while the initial session bootstrap (see below) is in flight. */
  isLoading: boolean
  login: (email: string, password: string) => Promise<void>
  register: (email: string, password: string, displayName: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  const handleAuthFailure = useCallback(() => {
    setUser(null)
  }, [])

  useEffect(() => {
    api.setOnAuthFailure(handleAuthFailure)
    return () => api.setOnAuthFailure(null)
  }, [handleAuthFailure])

  // On mount there's no access token in memory (it never survives a
  // reload by design). Calling getMe() with no token gets a 401, which
  // apiFetch's built-in retry-after-refresh logic turns into: attempt
  // POST /auth/refresh using the httpOnly cookie -> on success, retry
  // getMe() with the new token. This is exactly how a returning user with
  // a still-valid refresh cookie gets silently logged back in.
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const me = await api.getMe()
        if (!cancelled) setUser(me)
      } catch {
        if (!cancelled) setUser(null)
      } finally {
        if (!cancelled) setIsLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const login = useCallback(async (email: string, password: string) => {
    const session = await api.login({ email, password })
    api.setAccessToken(session.access_token)
    setUser(session.user)
  }, [])

  const register = useCallback(async (email: string, password: string, displayName: string) => {
    const session = await api.register({ email, password, display_name: displayName })
    api.setAccessToken(session.access_token)
    setUser(session.user)
  }, [])

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } finally {
      api.setAccessToken(null)
      setUser(null)
    }
  }, [])

  return (
    <AuthContext.Provider value={{ user, isLoading, login, register, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider')
  return ctx
}
