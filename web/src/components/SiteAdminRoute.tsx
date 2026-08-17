import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

// Client-side gating on user.is_site_admin — UX only. The real
// enforcement is server-side: every /admin/* route independently requires
// requireSiteAdmin (see api/internal/httpapi/middleware.go), so a
// non-admin who somehow reaches one of these screens still gets a 403 from
// every request. This component just avoids showing the admin nav/screens
// to someone who can't use them.
export function SiteAdminRoute({ children }: { children: ReactNode }) {
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

  if (!user.is_site_admin) {
    return <Navigate to="/" replace />
  }

  return <>{children}</>
}
