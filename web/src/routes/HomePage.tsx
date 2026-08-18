import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { BrandWordmark } from '../components/BrandWordmark'
import { getConferenceLogoUrl } from '../leagues/conferenceLogos'
import { listLeagues, resendVerification, ApiError } from '../api'

// Shown on the home page for a signed-in user whose /me response has
// email_verified_at: null. There's no real email delivery to click through
// in this environment, so a resend button is the full extent of this UI —
// see api/internal/auth/password_reset.go for the backend side.
function VerifyEmailBanner() {
  const [status, setStatus] = useState<'idle' | 'sending' | 'sent' | 'error'>('idle')
  const [error, setError] = useState<string | null>(null)

  const onResend = async () => {
    setStatus('sending')
    setError(null)
    try {
      await resendVerification()
      setStatus('sent')
    } catch (err) {
      setStatus('error')
      setError(err instanceof ApiError ? err.message : 'Failed to send verification email.')
    }
  }

  return (
    <div className="rounded-xl border border-amber-800/60 bg-amber-950/40 p-4 flex items-center justify-between gap-3">
      <div>
        <p className="text-sm text-amber-200 font-medium">Verify your email address</p>
        <p className="text-xs text-amber-300/80 mt-0.5">
          {status === 'sent'
            ? 'Verification email sent — check your inbox.'
            : "We sent a verification link when you signed up. Didn't get it?"}
        </p>
        {error && <p className="text-xs text-red-400 mt-0.5">{error}</p>}
      </div>
      <button
        type="button"
        onClick={() => void onResend()}
        disabled={status === 'sending' || status === 'sent'}
        className="shrink-0 rounded-md border border-amber-700 px-3 py-1.5 text-xs font-medium text-amber-200 disabled:opacity-50"
      >
        {status === 'sending' ? 'Sending…' : status === 'sent' ? 'Sent' : 'Resend email'}
      </button>
    </div>
  )
}

// "My Leagues" — the main authenticated landing page (Phase 2 replaces
// Phase 1's /me-fetching placeholder with the real home view: leagues the
// signed-in user belongs to, plus entry points to create one or join one
// by code).
export function HomePage() {
  const { user, logout } = useAuth()
  const { data: leagues, error, isLoading } = useQuery({
    queryKey: ['leagues'],
    queryFn: listLeagues,
  })

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <div className="flex justify-center">
          <BrandWordmark size={325} />
        </div>

        <div className="flex items-center justify-between">
          <p className="text-sm text-slate-500">
            Signed in as <span className="text-slate-200">{user?.display_name}</span>
          </p>
          <button type="button" onClick={() => void logout()} className="text-xs text-slate-500 underline">
            Log out
          </button>
        </div>

        {user && user.email_verified_at === null && <VerifyEmailBanner />}

        <div className="flex gap-2">
          <Link
            to="/leagues/new"
            className="flex-1 text-center rounded-md bg-emerald-600 px-3 py-2 text-sm font-medium text-white hover:bg-emerald-500"
          >
            Create a league
          </Link>
          <Link
            to="/leagues/join"
            className="flex-1 text-center rounded-md border border-slate-700 px-3 py-2 text-sm font-medium text-slate-100"
          >
            Join by code
          </Link>
        </div>

        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          {isLoading && <p className="p-4 text-sm text-slate-500">Loading your leagues…</p>}
          {error && (
            <p className="p-4 text-sm text-red-400">Could not load leagues: {(error as Error).message}</p>
          )}
          {leagues && leagues.length === 0 && (
            <p className="p-4 text-sm text-slate-500">
              You're not in any leagues yet. Create one, or join with an invite code.
            </p>
          )}
          {leagues?.map((league) => (
            <Link
              key={league.id}
              to={`/leagues/${league.id}`}
              className={
                'flex items-center justify-between p-4 hover:bg-slate-800/60 transition-colors' +
                (league.status === 'closed' ? ' opacity-60' : '')
              }
            >
              <div className="flex items-center gap-3 min-w-0">
                {getConferenceLogoUrl(league.conference) && (
                  <img
                    src={getConferenceLogoUrl(league.conference)}
                    alt=""
                    className="h-14 w-14 object-contain shrink-0"
                    loading="lazy"
                  />
                )}
                <div className="min-w-0">
                  <p className="text-sm font-medium text-slate-100 truncate">{league.name}</p>
                  <p className="text-xs text-slate-500">
                    {league.conference} · {league.season_year}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                {league.status === 'closed' && (
                  <span className="text-xs px-2 py-0.5 rounded-full border border-amber-700 text-amber-400">
                    closed
                  </span>
                )}
                <span
                  className={
                    'text-xs px-2 py-0.5 rounded-full border ' +
                    (league.membership.role === 'commissioner'
                      ? 'border-emerald-700 text-emerald-400'
                      : 'border-slate-700 text-slate-300')
                  }
                >
                  {league.membership.role}
                </span>
              </div>
            </Link>
          ))}
        </div>

        <div className="flex items-center gap-4">
          <Link to="/notification-preferences" className="text-xs text-slate-500 underline">
            Notification preferences
          </Link>
          <Link to="/health" className="text-xs text-slate-500 underline">
            API health check
          </Link>
          {user?.is_site_admin && (
            <Link to="/admin" className="text-xs text-emerald-400 underline">
              Site admin
            </Link>
          )}
        </div>
      </div>
    </main>
  )
}
