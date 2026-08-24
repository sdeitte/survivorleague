import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { BrandWordmark } from '../components/BrandWordmark'
import { LeaderboardList } from '../components/LeaderboardList'
import { getLatestRecap, getLeaderboard, getLeague, ApiError } from '../api'

// The leaderboard screen — standings rendering itself lives in
// LeaderboardList (shared with the league overview page). Sorting is
// entirely the server's job (see GET /leagues/:id/leaderboard's contract:
// active first, then eliminated ordered by how late they were
// eliminated).
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
  const recapQuery = useQuery({
    queryKey: ['league', id, 'recap'],
    queryFn: () => getLatestRecap(id!),
    enabled: !!id,
    // A 404 (no week has finalized yet — a brand-new league) is an
    // expected, terminal state, not worth retrying, same treatment
    // PicksPage gives GET current-week's identical 404 case.
    retry: false,
  })
  const noRecapYet = recapQuery.error instanceof ApiError && recapQuery.error.status === 404

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <div className="flex justify-center text-lg">
          <BrandWordmark size={200} />
        </div>

        <Link to={`/leagues/${id}`} className="text-xs text-slate-500 underline">
          ← {leagueQuery.data?.name ?? 'League'}
        </Link>

        <h1 className="text-xl font-semibold">Leaderboard</h1>

        {recapQuery.data && !noRecapYet && (
          <section className="rounded-xl border border-slate-800 bg-slate-900 p-4 space-y-2">
            <h2 className="text-sm font-medium text-slate-200">This week's recap</h2>
            <p className="text-sm text-slate-300 whitespace-pre-line">{recapQuery.data.body}</p>
          </section>
        )}

        {leaderboardQuery.isLoading && <p className="text-sm text-slate-500">Loading standings…</p>}
        {leaderboardQuery.error && (
          <p className="text-red-400 text-sm">
            {leaderboardQuery.error instanceof ApiError ? leaderboardQuery.error.message : 'Could not load the leaderboard.'}
          </p>
        )}

        {leaderboardQuery.data && <LeaderboardList leagueId={id!} entries={leaderboardQuery.data} />}
      </div>
    </main>
  )
}
