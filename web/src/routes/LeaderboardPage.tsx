import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { BrandWordmark } from '../components/BrandWordmark'
import { getLeaderboard, getLeague, listMembershipPicks, ApiError, type LeaderboardEntry, type MembershipWeekPick } from '../api'

// The leaderboard screen — a sorted list with status badges, each row
// expandable into that member's full-season pick history. Sorting itself
// is entirely the server's job (see GET /leagues/:id/leaderboard's
// contract: active first, then eliminated ordered by how late they were
// eliminated); this component just renders the response in the order it
// arrives, no client-side re-sorting.
export function LeaderboardPage() {
  const { id } = useParams<{ id: string }>()
  const [expandedMembershipId, setExpandedMembershipId] = useState<string | null>(null)

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
        <div className="flex justify-center text-lg">
          <BrandWordmark size={200} />
        </div>

        <Link to={`/leagues/${id}`} className="text-xs text-slate-500 underline">
          ← {leagueQuery.data?.name ?? 'League'}
        </Link>

        <h1 className="text-xl font-semibold">Leaderboard</h1>

        {leaderboardQuery.isLoading && <p className="text-sm text-slate-500">Loading standings…</p>}
        {leaderboardQuery.error && (
          <p className="text-red-400 text-sm">
            {leaderboardQuery.error instanceof ApiError ? leaderboardQuery.error.message : 'Could not load the leaderboard.'}
          </p>
        )}

        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          {leaderboardQuery.data?.map((entry, i) => (
            <LeaderboardRow
              key={entry.membership_id}
              leagueId={id!}
              entry={entry}
              rank={i + 1}
              expanded={expandedMembershipId === entry.membership_id}
              onToggle={() =>
                setExpandedMembershipId((cur) => (cur === entry.membership_id ? null : entry.membership_id))
              }
            />
          ))}
          {leaderboardQuery.data?.length === 0 && (
            <p className="p-4 text-sm text-slate-500">No members yet.</p>
          )}
        </div>
      </div>
    </main>
  )
}

function LeaderboardRow({
  leagueId,
  entry,
  rank,
  expanded,
  onToggle,
}: {
  leagueId: string
  entry: LeaderboardEntry
  rank: number
  expanded: boolean
  onToggle: () => void
}) {
  const picksQuery = useQuery({
    queryKey: ['league', leagueId, 'members', entry.membership_id, 'picks'],
    queryFn: () => listMembershipPicks(leagueId, entry.membership_id),
    enabled: expanded,
  })

  return (
    <div>
      <button type="button" onClick={onToggle} className="w-full flex items-center justify-between p-4 text-left hover:bg-slate-800/40">
        <div className="flex items-center gap-3">
          <span className="text-xs text-slate-500 w-5 text-right">{rank}</span>
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
              (entry.status === 'active' ? 'border-emerald-700 text-emerald-400' : 'border-slate-700 text-slate-500')
            }
          >
            {entry.status === 'active' ? 'Alive' : 'Eliminated'}
          </span>
          <span className="text-slate-500 text-xs">{expanded ? '▲' : '▼'}</span>
        </div>
      </button>

      {expanded && (
        <div className="px-4 pb-4">
          {picksQuery.isLoading && <p className="text-xs text-slate-500">Loading picks…</p>}
          {picksQuery.error && (
            <p className="text-xs text-red-400">
              {picksQuery.error instanceof ApiError ? picksQuery.error.message : 'Could not load this member’s picks.'}
            </p>
          )}
          {picksQuery.data && picksQuery.data.length === 0 && (
            <p className="text-xs text-slate-500">No schedule data yet.</p>
          )}
          {picksQuery.data && picksQuery.data.length > 0 && (
            <div className="rounded-lg border border-slate-800 divide-y divide-slate-800 overflow-hidden">
              {picksQuery.data.map((wp) => (
                <WeekPickRow key={wp.week_number} pick={wp} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function WeekPickRow({ pick }: { pick: MembershipWeekPick }) {
  const revealed = pick.has_picked && !!pick.team_name

  return (
    <div className="flex items-center justify-between px-3 py-2 bg-slate-950/60">
      <span className="text-xs text-slate-500 w-14 shrink-0">Wk {pick.week_number}</span>
      <div className="flex-1 min-w-0">
        {!pick.has_picked ? (
          <span className="text-xs text-slate-600">Not picked</span>
        ) : revealed ? (
          <span className="text-xs text-slate-200 inline-flex items-center gap-1.5">
            {pick.team_logo_url && (
              <span className="h-4 w-4 rounded-full bg-white p-0.5 shrink-0">
                <img src={pick.team_logo_url} alt="" className="h-full w-full object-contain" loading="lazy" />
              </span>
            )}
            {pick.team_name} <span className="text-slate-500">{pick.is_home ? 'vs' : '@'} {pick.opponent_name}</span>
          </span>
        ) : (
          <span className="text-xs text-slate-500">Picked (hidden until kickoff)</span>
        )}
      </div>
      <ResultBadge pick={pick} revealed={revealed} />
    </div>
  )
}

function ResultBadge({ pick, revealed }: { pick: MembershipWeekPick; revealed: boolean }) {
  if (!pick.has_picked) return null
  if (!revealed || !pick.result || pick.result === 'pending') {
    return <span className="text-xs text-slate-500 shrink-0 ml-2">{pick.is_locked ? 'In progress' : 'Pending'}</span>
  }
  if (pick.result === 'win') {
    return <span className="text-xs text-emerald-400 shrink-0 ml-2">Won</span>
  }
  if (pick.result === 'loss') {
    return <span className="text-xs text-red-400 shrink-0 ml-2">Lost</span>
  }
  return <span className="text-xs text-slate-500 shrink-0 ml-2">Void</span>
}
