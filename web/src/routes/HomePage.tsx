import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { listLeagues } from '../api'

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
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold">Survivor League</h1>
            <p className="text-sm text-slate-400">
              Signed in as <span className="text-slate-200">{user?.display_name}</span>
            </p>
          </div>
          <button type="button" onClick={() => void logout()} className="text-xs text-slate-400 underline">
            Log out
          </button>
        </div>

        <div className="flex gap-2">
          <Link
            to="/leagues/new"
            className="flex-1 text-center rounded-md bg-slate-100 px-3 py-2 text-sm font-medium text-slate-900"
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
          {isLoading && <p className="p-4 text-sm text-slate-400">Loading your leagues…</p>}
          {error && (
            <p className="p-4 text-sm text-red-400">Could not load leagues: {(error as Error).message}</p>
          )}
          {leagues && leagues.length === 0 && (
            <p className="p-4 text-sm text-slate-400">
              You're not in any leagues yet. Create one, or join with an invite code.
            </p>
          )}
          {leagues?.map((league) => (
            <Link
              key={league.id}
              to={`/leagues/${league.id}`}
              className="flex items-center justify-between p-4 hover:bg-slate-800/60 transition-colors"
            >
              <div>
                <p className="text-sm font-medium text-slate-100">{league.name}</p>
                <p className="text-xs text-slate-400">
                  {league.conference} · {league.season_year}
                </p>
              </div>
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
