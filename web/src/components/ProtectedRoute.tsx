import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { user, isLoading } = useAuth()

  if (isLoading) {
    return (
      <main className="min-h-screen bg-slate-950 flex items-center justify-center text-slate-400 text-sm">
        Loading…
      </main>
    )
  }

  if (!user) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}
