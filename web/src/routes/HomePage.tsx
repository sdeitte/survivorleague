import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { getMe } from '../api'

// The main authenticated landing page. Calling GET /me here (rather than
// just trusting the AuthContext user already in memory) is today's proof
// that the whole auth flow — token attach, cookie round-trip, and the
// requireAuth middleware on the server — works end to end. Replaces
// Phase 0's /health-fetching placeholder as the app's main page.
export function HomePage() {
  const { user, logout } = useAuth()
  const { data, error, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['me'],
    queryFn: getMe,
  })

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-md w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <div className="flex items-center justify-between">
          <h1 className="text-xl font-semibold">Survivor League</h1>
          <button
            type="button"
            onClick={() => void logout()}
            className="text-xs text-slate-400 underline"
          >
            Log out
          </button>
        </div>

        <p className="text-sm text-slate-400">
          Signed in as <span className="text-slate-200">{user?.display_name}</span>. The card
          below is a live <code className="text-slate-300">GET /me</code> call, proving the
          access token, refresh cookie, and requireAuth middleware all round-trip correctly.
        </p>

        <div className="rounded-lg bg-slate-800/60 p-4 text-sm">
          {isLoading && <p>Loading your profile…</p>}
          {error && <p className="text-red-400">Could not load profile: {(error as Error).message}</p>}
          {data && (
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
              <dt className="text-slate-400">id</dt>
              <dd className="text-slate-200 truncate">{data.id}</dd>
              <dt className="text-slate-400">email</dt>
              <dd className="text-slate-200">{data.email}</dd>
              <dt className="text-slate-400">display_name</dt>
              <dd className="text-slate-200">{data.display_name}</dd>
              <dt className="text-slate-400">is_site_admin</dt>
              <dd className={data.is_site_admin ? 'text-emerald-400' : 'text-slate-200'}>
                {String(data.is_site_admin)}
              </dd>
            </dl>
          )}
        </div>

        <div className="flex items-center justify-between">
          <button
            type="button"
            onClick={() => void refetch()}
            disabled={isFetching}
            className="rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
          >
            {isFetching ? 'Refreshing…' : 'Refresh'}
          </button>
          <Link to="/health" className="text-xs text-slate-500 underline">
            API health check
          </Link>
        </div>
      </div>
    </main>
  )
}
