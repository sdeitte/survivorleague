import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { BrandWordmark } from '../components/BrandWordmark'
import {
  getAvailableTeams,
  getCurrentWeek,
  getLeague,
  getMyPick,
  listWeekPicks,
  listWeeks,
  upsertMyPick,
  ApiError,
  type AvailableTeam,
  type Pick,
  type Week,
} from '../api'

// The weekly picks screen — this was the old app's most complex screen too
// (its Pick.js was 639 lines). Fetches available-teams for the selected
// week (already filtered to the league's locked conference, already
// carrying is_locked/is_used_elsewhere/is_current_pick so this component
// never has to compute lock/used logic itself — that's the server's job,
// per the API contract), lets the requester select a team (PUT .../picks/me),
// and once they've picked (or the week has started), shows the rest of the
// league's pick status via GET .../picks, exactly as that endpoint shapes
// it (own pick always visible; others hidden pre-lock as a game_id/team_id
// pair, revealed post-lock) — this component deliberately does not try to
// be cleverer than that response.
export function PicksPage() {
  const { id } = useParams<{ id: string }>()
  const queryClient = useQueryClient()
  const [selectedWeekId, setSelectedWeekId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const leagueQuery = useQuery({
    queryKey: ['league', id],
    queryFn: () => getLeague(id!),
    enabled: !!id,
  })

  const weeksQuery = useQuery({
    queryKey: ['weeks', leagueQuery.data?.season_year],
    queryFn: () => listWeeks(leagueQuery.data!.season_year),
    enabled: !!leagueQuery.data,
  })

  // Default to the week that's currently occurring schedule-wise for the
  // league's conference — one request to the server (which already knows
  // every week's kickoff range), not a client-side scan across every
  // week's available-teams.
  const currentWeekQuery = useQuery({
    queryKey: ['league', id, 'current-week'],
    queryFn: () => getCurrentWeek(id!),
    enabled: !!id && selectedWeekId === null,
    retry: false, // a 404 (no schedule data yet) is an expected, terminal state, not worth retrying
  })

  useEffect(() => {
    if (selectedWeekId === null && currentWeekQuery.data) {
      setSelectedWeekId(currentWeekQuery.data.id)
    }
  }, [selectedWeekId, currentWeekQuery.data])

  const weekId = selectedWeekId ?? undefined

  const availableTeamsQuery = useQuery({
    queryKey: ['league', id, 'weeks', weekId, 'available-teams'],
    queryFn: () => getAvailableTeams(id!, weekId!),
    enabled: !!id && !!weekId,
  })

  // Every other week's own pick, used only to render a helpful "already
  // used in Week N" reason next to a team flagged is_used_elsewhere — the
  // available-teams response tells us THAT a team is used elsewhere, not
  // WHICH week, so this fills in the gap client-side.
  const allPicksQuery = useQuery({
    queryKey: ['league', id, 'all-my-picks', weeksQuery.data?.map((w) => w.id).join(',')],
    queryFn: () =>
      Promise.all((weeksQuery.data ?? []).map((week) => getMyPick(id!, week.id).then((pick) => ({ week, pick })))),
    enabled: !!id && !!weeksQuery.data && weeksQuery.data.length > 0,
  })

  // The same week's available-teams list already covers every team with a
  // game that week (not just ones eligible for the requester specifically),
  // so it doubles as a lookup for rendering another member's revealed pick
  // by name rather than a bare id.
  const teamNameById = useMemo(() => {
    const map = new Map<string, string>()
    for (const t of availableTeamsQuery.data?.teams ?? []) map.set(t.team_id, t.team_name)
    return map
  }, [availableTeamsQuery.data])

  const teamLogoById = useMemo(() => {
    const map = new Map<string, string>()
    for (const t of availableTeamsQuery.data?.teams ?? []) {
      if (t.team_logo_url) map.set(t.team_id, t.team_logo_url)
    }
    return map
  }, [availableTeamsQuery.data])

  const usedTeamWeekNumbers = useMemo(() => {
    const map = new Map<string, number>()
    for (const { week, pick } of allPicksQuery.data ?? []) {
      if (pick && week.id !== weekId) map.set(pick.team_id, week.week_number)
    }
    return map
  }, [allPicksQuery.data, weekId])

  const hasOwnPick = !!availableTeamsQuery.data?.current_pick
  const anyGameLockedThisWeek = availableTeamsQuery.data?.teams.some((t) => t.is_locked) ?? false

  const weekPicksQuery = useQuery({
    queryKey: ['league', id, 'weeks', weekId, 'picks'],
    queryFn: () => listWeekPicks(id!, weekId!),
    enabled: !!id && !!weekId && (hasOwnPick || anyGameLockedThisWeek),
  })

  const pickMutation = useMutation({
    mutationFn: (team: AvailableTeam) => upsertMyPick(id!, weekId!, { game_id: team.game_id, team_id: team.team_id }),
    onSuccess: (pick: Pick) => {
      setActionError(null)
      queryClient.setQueryData(
        ['league', id, 'weeks', weekId, 'available-teams'],
        (old: typeof availableTeamsQuery.data) =>
          old && {
            ...old,
            current_pick: pick,
            teams: old.teams.map((t) => ({ ...t, is_current_pick: t.team_id === pick.team_id })),
          },
      )
      void queryClient.invalidateQueries({ queryKey: ['league', id, 'weeks', weekId, 'picks'] })
      void queryClient.invalidateQueries({ queryKey: ['league', id, 'all-my-picks'] })
    },
    onError: (err) => {
      if (err instanceof ApiError) {
        if (err.status === 409) {
          setActionError(
            err.message.toLowerCase().includes('used')
              ? "You've already used that team in a different week."
              : 'Your pick for this week is already locked — its game has started.',
          )
        } else {
          setActionError(err.message)
        }
      } else {
        setActionError('Failed to save your pick.')
      }
    },
  })

  if (leagueQuery.isLoading || weeksQuery.isLoading) {
    return (
      <main className="min-h-screen bg-slate-950 flex items-center justify-center text-slate-400 text-sm">
        Loading picks…
      </main>
    )
  }

  if (leagueQuery.error || !leagueQuery.data) {
    return (
      <main className="min-h-screen bg-slate-950 flex items-center justify-center text-slate-100 p-6">
        <div className="max-w-sm w-full space-y-3 text-center">
          <p className="text-red-400 text-sm">
            {leagueQuery.error instanceof ApiError ? leagueQuery.error.message : 'Could not load this league.'}
          </p>
          <Link to="/" className="text-sm text-slate-300 underline">
            Back to My Leagues
          </Link>
        </div>
      </main>
    )
  }

  const league = leagueQuery.data
  const weeks: Week[] = weeksQuery.data ?? []
  const selectedWeek = weeks.find((w) => w.id === weekId)
  const selectedIndex = weeks.findIndex((w) => w.id === weekId)
  const goToOffset = (offset: number) => {
    if (selectedIndex < 0) return
    const next = weeks[selectedIndex + offset]
    if (next) setSelectedWeekId(next.id)
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <div className="flex justify-center text-lg">
          <BrandWordmark size={200} />
        </div>

        <Link to={`/leagues/${id}`} className="text-xs text-slate-500 underline">
          ← {league.name}
        </Link>

        <div>
          <h1 className="text-xl font-semibold">Picks</h1>
          <p className="text-sm text-slate-500">
            {league.conference} · {league.season_year}
          </p>
        </div>

        {weeks.length === 0 ? (
          <p className="text-sm text-slate-500">No schedule data yet — check back once weeks are synced.</p>
        ) : (
          <div className="flex items-center justify-center gap-2">
            <button
              type="button"
              onClick={() => goToOffset(-1)}
              disabled={selectedIndex <= 0}
              aria-label="Previous week"
              className="rounded-md border border-slate-700 px-2.5 py-1.5 text-slate-300 disabled:opacity-30 disabled:cursor-not-allowed enabled:hover:bg-slate-800"
            >
              ‹
            </button>
            <select
              value={weekId ?? ''}
              onChange={(e) => setSelectedWeekId(e.target.value)}
              className="rounded-md border border-slate-700 bg-slate-900 px-3 py-1.5 text-sm text-slate-100 text-center"
            >
              {weeks.map((week) => (
                <option key={week.id} value={week.id}>
                  Week {week.week_number}
                </option>
              ))}
            </select>
            <button
              type="button"
              onClick={() => goToOffset(1)}
              disabled={selectedIndex < 0 || selectedIndex >= weeks.length - 1}
              aria-label="Next week"
              className="rounded-md border border-slate-700 px-2.5 py-1.5 text-slate-300 disabled:opacity-30 disabled:cursor-not-allowed enabled:hover:bg-slate-800"
            >
              ›
            </button>
          </div>
        )}

        {actionError && (
          <p className="text-sm text-red-400 rounded-md border border-red-900/50 bg-red-950/40 px-3 py-2">
            {actionError}
          </p>
        )}

        {weekId && (
          <>
            {availableTeamsQuery.data?.current_pick &&
              (() => {
                const pickedTeam = availableTeamsQuery.data!.teams.find((t) => t.is_current_pick)
                return (
                  <section className="rounded-xl border border-emerald-800/60 bg-emerald-950/30 p-4">
                    <p className="text-xs text-emerald-400 mb-1">
                      Your pick — {selectedWeek ? `Week ${selectedWeek.week_number}` : 'this week'}
                    </p>
                    {pickedTeam ? (
                      <div className="flex items-center gap-3">
                        {pickedTeam.team_logo_url && (
                          <div className="h-[72px] w-[72px] rounded-full bg-white p-2 shrink-0">
                            <img src={pickedTeam.team_logo_url} alt="" className="h-full w-full object-contain" loading="lazy" />
                          </div>
                        )}
                        <div>
                          <p className="text-lg font-semibold text-slate-100">{pickedTeam.team_name}</p>
                          <p className="text-sm text-slate-300">
                            {pickedTeam.is_home ? 'vs' : '@'} {pickedTeam.opponent_name} ·{' '}
                            {new Date(pickedTeam.kickoff_at).toLocaleString(undefined, {
                              weekday: 'short',
                              month: 'short',
                              day: 'numeric',
                              hour: 'numeric',
                              minute: '2-digit',
                            })}
                          </p>
                          {pickedTeam.is_locked && <p className="text-xs text-slate-500 mt-1">Locked</p>}
                        </div>
                      </div>
                    ) : (
                      <p className="text-sm text-slate-300">Saved</p>
                    )}
                  </section>
                )
              })()}

            <section className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
              <h2 className="p-4 text-sm font-medium text-slate-200">
                {selectedWeek ? `Week ${selectedWeek.week_number}` : 'This week'}'s teams
              </h2>
              {availableTeamsQuery.isLoading && <p className="p-4 text-sm text-slate-500">Loading teams…</p>}
              {availableTeamsQuery.error && (
                <p className="p-4 text-sm text-red-400">Could not load teams for this week.</p>
              )}
              {availableTeamsQuery.data?.teams.length === 0 && (
                <p className="p-4 text-sm text-slate-500">No {league.conference} games scheduled this week.</p>
              )}
              {availableTeamsQuery.data?.teams.map((team) => {
                const disabled = team.is_used_elsewhere || (team.is_locked && !team.is_current_pick)
                const usedWeek = usedTeamWeekNumbers.get(team.team_id)
                let reason: string | null = null
                if (team.is_used_elsewhere) {
                  reason = usedWeek ? `Already used in Week ${usedWeek}` : 'Already used in a different week'
                } else if (team.is_locked && !team.is_current_pick) {
                  reason = 'Game already started'
                }
                return (
                  <button
                    key={team.team_id}
                    type="button"
                    disabled={disabled || pickMutation.isPending}
                    onClick={() => pickMutation.mutate(team)}
                    className={
                      'w-full flex items-center justify-between p-4 text-left transition-colors ' +
                      (team.is_current_pick
                        ? 'bg-emerald-950/40'
                        : disabled
                          ? 'opacity-40 cursor-not-allowed'
                          : 'hover:bg-slate-800/60')
                    }
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      {team.team_logo_url && (
                        <div className="h-11 w-11 rounded-full bg-white p-1.5 shrink-0">
                          <img src={team.team_logo_url} alt="" className="h-full w-full object-contain" loading="lazy" />
                        </div>
                      )}
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-slate-100">
                          {team.team_name}
                          {team.is_current_pick && <span className="ml-2 text-xs text-emerald-400">Your pick</span>}
                        </p>
                        <p className="text-xs text-slate-500">
                          {team.is_home ? 'vs' : '@'} {team.opponent_name} ·{' '}
                          {new Date(team.kickoff_at).toLocaleString(undefined, {
                            weekday: 'short',
                            month: 'short',
                            day: 'numeric',
                            hour: 'numeric',
                            minute: '2-digit',
                          })}
                        </p>
                        {reason && <p className="text-xs text-amber-500 mt-0.5">{reason}</p>}
                      </div>
                    </div>
                    {team.is_locked && <span className="text-xs text-slate-500 shrink-0 ml-2">Locked</span>}
                  </button>
                )
              })}
            </section>

            {(hasOwnPick || anyGameLockedThisWeek) && (
              <section className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
                <h2 className="p-4 text-sm font-medium text-slate-200">League picks</h2>
                {weekPicksQuery.isLoading && <p className="p-4 text-sm text-slate-500">Loading…</p>}
                {weekPicksQuery.error && (
                  <p className="p-4 text-sm text-red-400">
                    {weekPicksQuery.error instanceof ApiError
                      ? weekPicksQuery.error.message
                      : 'Could not load the league picks for this week.'}
                  </p>
                )}
                {weekPicksQuery.data?.map((status) => (
                  <div key={status.membership_id} className="flex items-center justify-between p-4">
                    <p className="text-sm text-slate-100">{status.display_name}</p>
                    {status.has_picked ? (
                      status.team_id ? (
                        <span className="text-xs text-emerald-400 flex items-center gap-1.5">
                          {teamLogoById.get(status.team_id) && (
                            <span className="h-6 w-6 rounded-full bg-white p-1 shrink-0">
                              <img
                                src={teamLogoById.get(status.team_id)}
                                alt=""
                                className="h-full w-full object-contain"
                                loading="lazy"
                              />
                            </span>
                          )}
                          {teamNameById.get(status.team_id) ?? 'Picked'}
                        </span>
                      ) : (
                        <span className="text-xs text-slate-500">Picked (hidden until kickoff)</span>
                      )
                    ) : (
                      <span className="text-xs text-slate-500">Not yet picked</span>
                    )}
                  </div>
                ))}
              </section>
            )}
          </>
        )}
      </div>
    </main>
  )
}
