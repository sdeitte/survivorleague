import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { getLeaderboard, getLeague, ApiError } from '../api'

// The leaderboard screen — a sorted list with status badges. Sorting
// itself is entirely the server's job (see GET /leagues/:id/leaderboard's
// contract: active first, then eliminated ordered by how late they were
// eliminated); this component just renders the response in the order it
// arrives, no client-side re-sorting.
export function LeaderboardPage() {
  const { id } = useParams<{ id: string }>()

  const leagueQuery = useQuery({
    queryKey: ['league', id],
    queryFn: () => getLeague(id!),
    enabled: !!id,
  })
  const leaderboardQuery = useQuery({
    queryKey: ['league', id, 'leaderboard'],
    queryFn: () => getLeaderboard(id!),
    enabled: !!id,
    // Standings only change when a game finalizes — a slow poll is plenty
    // per the plan's polling-based real-time architecture.
    refetchInterval: 60_000,
  })

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <Link to={`/leagues/${id}`} className="text-xs text-slate-500 underline">
          ← {leagueQuery.data?.name ?? 'League'}
        </Link>

        <h1 className="text-xl font-semibold">Leaderboard</h1>

        {leaderboardQuery.isLoading && <p className="text-sm text-slate-400">Loading standings…</p>}
        {leaderboardQuery.error && (
          <p className="text-red-400 text-sm">
            {leaderboardQuery.error instanceof ApiError ? leaderboardQuery.error.message : 'Could not load the leaderboard.'}
          </p>
        )}

        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          {leaderboardQuery.data?.map((entry, i) => (
            <div key={entry.membership_id} className="flex items-center justify-between p-4">
              <div className="flex items-center gap-3">
                <span className="text-xs text-slate-500 w-5 text-right">{i + 1}</span>
                <div>
                  <p className="text-sm text-slate-100">{entry.display_name}</p>
                  {!entry.is_contestant && <p className="text-xs text-slate-500">not playing</p>}
                </div>
              </div>
              <div className="flex items-center gap-2">
                {entry.bought_back && (
                  <span className="text-xs px-2 py-0.5 rounded-full border border-amber-700 text-amber-400">
                    bought back
                  </span>
                )}
                <span
                  className={
                    'text-xs px-2 py-0.5 rounded-full border ' +
                    (entry.status === 'active'
                      ? 'border-emerald-700 text-emerald-400'
                      : 'border-slate-700 text-slate-500')
                  }
                >
                  {entry.status === 'active' ? 'Alive' : 'Eliminated'}
                </span>
              </div>
            </div>
          ))}
          {leaderboardQuery.data?.length === 0 && (
            <p className="p-4 text-sm text-slate-400">No members yet.</p>
          )}
        </div>
      </div>
    </main>
  )
}
