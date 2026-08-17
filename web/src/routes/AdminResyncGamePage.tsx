import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { AdminNav } from '../components/AdminNav'
import { listWeeks, listWeekGames, resyncGame, ApiError, type ResyncGameResponse } from '../api'

const currentSeasonYear = () => {
  const now = new Date()
  return now.getMonth() + 1 >= 7 ? now.getFullYear() : now.getFullYear() - 1
}

// The direct unblock mechanism for a game whose status is stuck
// postponed/canceled/stale: browse to it by season/week (a game's internal
// UUID isn't something an admin would otherwise have memorized), then
// resync it. Shows whether the resync brought it to 'final' and, if so,
// which league-weeks that actually finalized as a result.
export function AdminResyncGamePage() {
  const [seasonYear, setSeasonYear] = useState(currentSeasonYear())
  const [weekId, setWeekId] = useState<string>('')
  const [lastResult, setLastResult] = useState<{ gameId: string; response: ResyncGameResponse } | null>(null)

  const weeksQuery = useQuery({
    queryKey: ['weeks', seasonYear],
    queryFn: () => listWeeks(seasonYear),
  })

  const gamesQuery = useQuery({
    queryKey: ['week-games', weekId],
    queryFn: () => listWeekGames(weekId),
    enabled: !!weekId,
  })

  const resyncMutation = useMutation({
    mutationFn: (gameId: string) => resyncGame(gameId),
    onSuccess: (response, gameId) => {
      setLastResult({ gameId, response })
      void gamesQuery.refetch()
    },
  })

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-2xl mx-auto space-y-4">
        <AdminNav />
        <p className="text-sm text-slate-400">
          Re-fetches one game from CollegeFootballData.com and upserts it. If it's now final, this also runs the
          same grading pass the live poll loop would — any league-week that was blocked on this game and can now
          finalize, will.
        </p>

        <div className="rounded-xl border border-slate-800 bg-slate-900 p-4 space-y-3">
          <div className="flex items-center gap-3">
            <label className="text-xs text-slate-400 flex items-center gap-2">
              Season
              <input
                type="number"
                value={seasonYear}
                onChange={(e) => {
                  setSeasonYear(Number(e.target.value))
                  setWeekId('')
                }}
                className="w-24 rounded-md border border-slate-700 bg-slate-800 px-2 py-1 text-sm text-slate-100"
              />
            </label>
            <label className="text-xs text-slate-400 flex items-center gap-2">
              Week
              <select
                value={weekId}
                onChange={(e) => setWeekId(e.target.value)}
                className="rounded-md border border-slate-700 bg-slate-800 px-2 py-1 text-sm text-slate-100"
              >
                <option value="">Select…</option>
                {weeksQuery.data?.map((w) => (
                  <option key={w.id} value={w.id}>
                    Week {w.week_number}
                  </option>
                ))}
              </select>
            </label>
          </div>
          {weeksQuery.error && <p className="text-xs text-red-400">Could not load weeks for {seasonYear}.</p>}
        </div>

        {weekId && (
          <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
            {gamesQuery.isLoading && <p className="p-4 text-sm text-slate-400">Loading games…</p>}
            {gamesQuery.error && <p className="p-4 text-sm text-red-400">Could not load this week's games.</p>}
            {gamesQuery.data?.map((game) => (
              <div key={game.id} className="p-4 space-y-2">
                <div className="flex items-center justify-between">
                  <p className="text-sm text-slate-100">
                    {game.away_team.name} @ {game.home_team.name}
                  </p>
                  <span
                    className={
                      'text-xs px-2 py-0.5 rounded-full border ' +
                      (game.status === 'final'
                        ? 'border-emerald-700 text-emerald-400'
                        : game.status === 'postponed' || game.status === 'canceled'
                          ? 'border-red-700 text-red-400'
                          : 'border-slate-700 text-slate-300')
                    }
                  >
                    {game.status}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <p className="text-xs text-slate-500">
                    {new Date(game.kickoff_at).toLocaleString()}
                    {game.home_score != null && game.away_score != null && ` · ${game.away_score}–${game.home_score}`}
                  </p>
                  <button
                    type="button"
                    disabled={resyncMutation.isPending}
                    onClick={() => resyncMutation.mutate(game.id)}
                    className="text-xs text-slate-100 underline disabled:opacity-50"
                  >
                    {resyncMutation.isPending && resyncMutation.variables === game.id ? 'Resyncing…' : 'Resync'}
                  </button>
                </div>
                {lastResult?.gameId === game.id && (
                  <div className="rounded-md bg-slate-950 p-3 text-xs text-slate-300 space-y-1">
                    <p>
                      Now status <span className="text-slate-100">{lastResult.response.game.status}</span>
                    </p>
                    {lastResult.response.finalized_league_weeks.length === 0 ? (
                      <p className="text-slate-500">No league-weeks finalized as a result.</p>
                    ) : (
                      <div>
                        <p className="text-emerald-400">
                          {lastResult.response.finalized_league_weeks.length} league-week
                          {lastResult.response.finalized_league_weeks.length === 1 ? '' : 's'} finalized:
                        </p>
                        <ul className="list-disc list-inside text-slate-400">
                          {lastResult.response.finalized_league_weeks.map((f) => (
                            <li key={`${f.league_id}-${f.week_id}`}>
                              league {f.league_id.slice(0, 8)}… {f.mass_wipeout && '(mass wipeout)'}
                            </li>
                          ))}
                        </ul>
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))}
            {gamesQuery.data?.length === 0 && <p className="p-4 text-sm text-slate-400">No games this week.</p>}
          </div>
        )}

        {resyncMutation.error && (
          <p className="text-sm text-red-400">
            {resyncMutation.error instanceof ApiError ? resyncMutation.error.message : 'Failed to resync game.'}
          </p>
        )}
      </div>
    </main>
  )
}
