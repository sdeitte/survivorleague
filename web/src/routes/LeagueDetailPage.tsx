import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import * as Dialog from '@radix-ui/react-dialog'
import { BrandWordmark } from '../components/BrandWordmark'
import { LeaderboardList } from '../components/LeaderboardList'
import { LeagueChat } from '../components/LeagueChat'
import { getConferenceLogoUrl } from '../leagues/conferenceLogos'
import {
  buyBackMember,
  closeLeague,
  getInviteCode,
  getLatestRecap,
  getLeaderboard,
  getLeague,
  regenerateInviteCode,
  removeMember,
  sendInvites,
  updateLeague,
  ApiError,
  type InviteSendResult,
  type LeaderboardEntry,
} from '../api'

export function LeagueDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [actionError, setActionError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [memberToRemove, setMemberToRemove] = useState<LeaderboardEntry | null>(null)
  const [memberToBuyBack, setMemberToBuyBack] = useState<LeaderboardEntry | null>(null)
  const [closeModalOpen, setCloseModalOpen] = useState(false)
  const [closeConfirmText, setCloseConfirmText] = useState('')
  const [inviteRows, setInviteRows] = useState<{ name: string; email: string }[]>([{ name: '', email: '' }])
  const [inviteResults, setInviteResults] = useState<InviteSendResult[] | null>(null)

  const leagueQuery = useQuery({
    queryKey: ['league', id],
    queryFn: () => getLeague(id!),
    enabled: !!id,
  })
  // The league overview's member-management list IS the leaderboard —
  // same data, same sort order (active first, then eliminated by how long
  // they lasted), just with commissioner actions (buy-back/remove)
  // attached to each row. No separate "plain members list" endpoint/query
  // — see leaderboard.sql's role column addition, which is what made this
  // possible (it previously only came from the now-removed listMembers).
  const membersQuery = useQuery({
    queryKey: ['league', id, 'leaderboard'],
    queryFn: () => getLeaderboard(id!),
    enabled: !!id,
    // Standings only change when a game finalizes — a slow poll is plenty,
    // matching the old standalone Leaderboard page's identical interval.
    refetchInterval: 60_000,
  })
  const recapQuery = useQuery({
    queryKey: ['league', id, 'recap'],
    queryFn: () => getLatestRecap(id!),
    enabled: !!id,
    // A 404 (no week has finalized yet) is expected/terminal, not worth
    // retrying — same treatment every other "might not exist yet" query
    // in this app gives its own 404 case.
    retry: false,
  })
  const noRecapYet = recapQuery.error instanceof ApiError && recapQuery.error.status === 404

  const isCommissioner = leagueQuery.data?.membership.role === 'commissioner'

  const inviteQuery = useQuery({
    queryKey: ['league', id, 'invite'],
    queryFn: () => getInviteCode(id!),
    enabled: !!id && isCommissioner,
  })

  const regenerateMutation = useMutation({
    mutationFn: () => regenerateInviteCode(id!),
    onSuccess: (data) => {
      queryClient.setQueryData(['league', id, 'invite'], data)
      setCopied(false)
    },
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to regenerate invite code.'),
  })

  const toggleContestantMutation = useMutation({
    mutationFn: (isContestant: boolean) => updateLeague(id!, { commissioner_is_contestant: isContestant }),
    onSuccess: (league) => queryClient.setQueryData(['league', id], league),
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to update league.'),
  })

  const removeMemberMutation = useMutation({
    mutationFn: (membershipId: string) => removeMember(id!, membershipId),
    onSuccess: () => {
      setMemberToRemove(null)
      void queryClient.invalidateQueries({ queryKey: ['league', id, 'leaderboard'] })
    },
    onError: (err) => {
      setActionError(err instanceof ApiError ? err.message : 'Failed to remove member.')
      setMemberToRemove(null)
    },
  })

  const buyBackMutation = useMutation({
    mutationFn: (membershipId: string) => buyBackMember(id!, membershipId),
    onSuccess: () => {
      setMemberToBuyBack(null)
      void queryClient.invalidateQueries({ queryKey: ['league', id, 'leaderboard'] })
    },
    onError: (err) => {
      setActionError(err instanceof ApiError ? err.message : 'Failed to buy back member.')
      setMemberToBuyBack(null)
    },
  })

  const closeMutation = useMutation({
    mutationFn: () => closeLeague(id!, closeConfirmText),
    onSuccess: (updated) => {
      queryClient.setQueryData(['league', id], updated)
      setCloseModalOpen(false)
      setCloseConfirmText('')
    },
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to close league.'),
  })

  const updateInviteRow = (index: number, field: 'name' | 'email', value: string) => {
    setInviteRows((rows) => rows.map((row, i) => (i === index ? { ...row, [field]: value } : row)))
  }
  const addInviteRow = () => setInviteRows((rows) => [...rows, { name: '', email: '' }])
  const removeInviteRow = (index: number) => setInviteRows((rows) => rows.filter((_, i) => i !== index))

  const sendInvitesMutation = useMutation({
    mutationFn: () => sendInvites(id!, inviteRows.filter((row) => row.email.trim() !== '')),
    onSuccess: (results) => {
      setInviteResults(results)
      if (results.every((r) => r.sent)) {
        setInviteRows([{ name: '', email: '' }])
      }
    },
    onError: (err) => setActionError(err instanceof ApiError ? err.message : 'Failed to send invites.'),
  })

  const copyInviteCode = async () => {
    if (!inviteQuery.data) return
    try {
      await navigator.clipboard.writeText(inviteQuery.data.invite_code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard API can be unavailable (e.g. insecure context); the code
      // is still visible on screen for manual copy, so this is a soft failure.
    }
  }

  if (leagueQuery.isLoading) {
    return (
      <main className="min-h-screen bg-slate-950 flex items-center justify-center text-slate-400 text-sm">
        Loading league…
      </main>
    )
  }

  if (leagueQuery.error || !leagueQuery.data) {
    return (
      <main className="min-h-screen bg-slate-950 flex items-center justify-center text-slate-100 p-6">
        <div className="max-w-sm w-full space-y-3 text-center">
          <p className="text-red-400 text-sm">
            {leagueQuery.error instanceof ApiError
              ? leagueQuery.error.message
              : 'Could not load this league.'}
          </p>
          <button
            type="button"
            onClick={() => navigate('/')}
            className="text-sm text-slate-300 underline"
          >
            Back to My Leagues
          </button>
        </div>
      </main>
    )
  }

  const league = leagueQuery.data
  const isClosed = league.status === 'closed'
  // Mirrors the backend's isLeagueJoinable — already covers "closed", so
  // this alone gates the invite code / invite-by-email UI (no separate
  // !isClosed check needed on top of it). Defaults to true while the
  // invite query is still loading so these cards don't flash hidden then
  // shown on first render.
  const inviteJoinable = inviteQuery.data?.joinable ?? true
  const closePhrase = `I want to close ${league.name}`

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <div className="flex justify-center text-lg">
          <BrandWordmark size={200} />
        </div>

        <Link to="/" className="text-xs text-slate-500 underline">
          ← My Leagues
        </Link>

        {isClosed && (
          <div className="rounded-xl border border-amber-800/60 bg-amber-950/40 p-4">
            <p className="text-sm text-amber-200 font-medium">This league is closed</p>
            <p className="text-xs text-amber-300/80 mt-0.5">
              No new picks, joins, or changes can be made. The league and its history are still here to look back
              on.
            </p>
          </div>
        )}

        {isClosed ? (
          <span className="block text-center rounded-md bg-slate-800 px-3 py-2 text-sm font-medium text-slate-500 cursor-not-allowed">
            Make your pick
          </span>
        ) : (
          <Link
            to={`/leagues/${id}/picks`}
            className="block text-center rounded-md bg-emerald-600 px-3 py-2 text-sm font-medium text-white hover:bg-emerald-500 transition-colors"
          >
            Make your pick
          </Link>
        )}

        <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-1">
          <div className="flex items-center justify-between">
            <h1 className="text-xl font-semibold">{league.name}</h1>
            <div className="flex items-center gap-2">
              {isClosed && (
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
          </div>
          <p className="text-sm text-slate-500 flex items-center gap-1.5">
            {getConferenceLogoUrl(league.conference) && (
              <img src={getConferenceLogoUrl(league.conference)} alt="" className="h-8 w-8 object-contain" loading="lazy" />
            )}
            {league.conference} · {league.season_year}
          </p>

          {isCommissioner && (
            <label className="flex items-center gap-2 pt-3 text-sm text-slate-300">
              <input
                type="checkbox"
                checked={league.membership.is_contestant}
                disabled={toggleContestantMutation.isPending || isClosed}
                onChange={(e) => toggleContestantMutation.mutate(e.target.checked)}
                className="rounded border-slate-700 bg-slate-800"
              />
              Playing as a contestant (uncheck to manage only)
            </label>
          )}
        </div>

        {isCommissioner && inviteJoinable && (
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-3">
            <h2 className="text-sm font-medium text-slate-200">Invite code</h2>
            {inviteQuery.isLoading && <p className="text-sm text-slate-500">Loading…</p>}
            {inviteQuery.data && (
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded-md bg-slate-800 px-3 py-2 text-lg tracking-widest text-center text-slate-100">
                  {inviteQuery.data.invite_code}
                </code>
                <button
                  type="button"
                  onClick={() => void copyInviteCode()}
                  className="rounded-md border border-slate-700 px-3 py-2 text-sm text-slate-100"
                >
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
            )}
            <button
              type="button"
              onClick={() => regenerateMutation.mutate()}
              disabled={regenerateMutation.isPending}
              className="text-sm text-slate-500 underline disabled:opacity-50"
            >
              {regenerateMutation.isPending ? 'Regenerating…' : 'Regenerate code (invalidates the old one)'}
            </button>
          </div>
        )}

        {isCommissioner && inviteJoinable && (
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-3">
            <h2 className="text-sm font-medium text-slate-200">Invite by email</h2>
            <p className="text-xs text-slate-500">
              Add names and emails, then send everyone your invite link at once.
            </p>
            <div className="space-y-2">
              {inviteRows.map((row, i) => (
                <div key={i} className="flex gap-2">
                  <input
                    type="text"
                    placeholder="Name (optional)"
                    value={row.name}
                    onChange={(e) => updateInviteRow(i, 'name', e.target.value)}
                    className="w-2/5 rounded-md border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-slate-100 placeholder:text-slate-600"
                  />
                  <input
                    type="email"
                    placeholder="email@example.com"
                    value={row.email}
                    onChange={(e) => updateInviteRow(i, 'email', e.target.value)}
                    className="flex-1 rounded-md border border-slate-700 bg-slate-950 px-2 py-1.5 text-sm text-slate-100 placeholder:text-slate-600"
                  />
                  <button
                    type="button"
                    onClick={() => removeInviteRow(i)}
                    disabled={inviteRows.length === 1}
                    className="px-1 text-slate-500 hover:text-red-400 disabled:opacity-30 disabled:hover:text-slate-500"
                    aria-label="Remove row"
                  >
                    ✕
                  </button>
                </div>
              ))}
            </div>
            <div className="flex items-center justify-between">
              <button type="button" onClick={addInviteRow} className="text-sm text-slate-500 underline">
                + Add another
              </button>
              <button
                type="button"
                onClick={() => sendInvitesMutation.mutate()}
                disabled={sendInvitesMutation.isPending || inviteRows.every((row) => row.email.trim() === '')}
                className="rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
              >
                {sendInvitesMutation.isPending ? 'Sending…' : 'Send invites'}
              </button>
            </div>
            {inviteResults && (
              <div className="space-y-1 border-t border-slate-800 pt-2">
                {inviteResults.map((r, i) => (
                  <p key={r.email + i} className={'text-xs ' + (r.sent ? 'text-emerald-400' : 'text-red-400')}>
                    {r.email} — {r.sent ? 'sent' : r.error}
                  </p>
                ))}
              </div>
            )}
          </div>
        )}

        {actionError && <p className="text-red-400 text-sm">{actionError}</p>}

        <LeagueChat leagueId={id!} isCommissioner={isCommissioner} />

        {recapQuery.data && !noRecapYet && (
          <section className="rounded-xl border border-slate-800 bg-slate-900 p-4 space-y-2">
            <h2 className="text-sm font-medium text-slate-200">This week's recap</h2>
            <p className="text-sm text-slate-300 whitespace-pre-line">{recapQuery.data.body}</p>
          </section>
        )}

        <div className="space-y-2">
          <h2 className="text-sm font-medium text-slate-200">Leaderboard</h2>
          {membersQuery.isLoading && <p className="text-sm text-slate-500">Loading standings…</p>}
          {membersQuery.error && (
            <p className="text-sm text-red-400">
              {membersQuery.error instanceof ApiError ? membersQuery.error.message : 'Could not load the leaderboard.'}
            </p>
          )}
          {membersQuery.data && (
            <LeaderboardList
              leagueId={id!}
              entries={membersQuery.data}
              renderActions={
                isCommissioner && !isClosed
                  ? (member) => (
                      <>
                        {member.status === 'eliminated' &&
                          (member.bought_back ? (
                            <span className="text-xs text-slate-500">Buy-back already used</span>
                          ) : (
                            <button
                              type="button"
                              onClick={() => setMemberToBuyBack(member)}
                              className="text-xs text-emerald-400 underline"
                            >
                              Buy back
                            </button>
                          ))}
                        {member.role !== 'commissioner' && (
                          <button
                            type="button"
                            onClick={() => setMemberToRemove(member)}
                            className="text-xs text-red-400 underline"
                          >
                            Remove
                          </button>
                        )}
                      </>
                    )
                  : undefined
              }
            />
          )}
        </div>

        {isCommissioner && !isClosed && (
          <div className="rounded-xl border border-red-900/60 bg-red-950/20 p-6 space-y-2">
            <h2 className="text-sm font-medium text-red-300">Danger zone</h2>
            <p className="text-xs text-red-400/80">
              Closing this league locks it for everyone — no more picks, joins, or changes. This can't be undone
              by you, though nothing is deleted.
            </p>
            <button
              type="button"
              onClick={() => setCloseModalOpen(true)}
              className="rounded-md border border-red-800 px-3 py-1.5 text-sm font-medium text-red-300 hover:bg-red-900/40"
            >
              Close league
            </button>
          </div>
        )}
      </div>

      <Dialog.Root open={!!memberToRemove} onOpenChange={(open) => !open && setMemberToRemove(null)}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/60" />
          <Dialog.Content className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-sm rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-4">
            <Dialog.Title className="text-lg font-semibold text-slate-100">Remove member?</Dialog.Title>
            <Dialog.Description className="text-sm text-slate-500">
              {memberToRemove?.display_name} will lose access to this league. They can rejoin later with the
              invite code.
            </Dialog.Description>
            <div className="flex gap-2">
              <Dialog.Close asChild>
                <button type="button" className="flex-1 rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100">
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="button"
                disabled={removeMemberMutation.isPending}
                onClick={() => memberToRemove && removeMemberMutation.mutate(memberToRemove.membership_id)}
                className="flex-1 rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
              >
                {removeMemberMutation.isPending ? 'Removing…' : 'Remove'}
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

      <Dialog.Root open={!!memberToBuyBack} onOpenChange={(open) => !open && setMemberToBuyBack(null)}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/60" />
          <Dialog.Content className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-sm rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-4">
            <Dialog.Title className="text-lg font-semibold text-slate-100">Buy back this member?</Dialog.Title>
            <Dialog.Description className="text-sm text-slate-500">
              {memberToBuyBack?.display_name} will be reinstated as an active contestant. This is a one-time
              lifeline per member — it cannot be undone or used again for them, even if they're eliminated
              again later. Their previously-used teams stay locked.
            </Dialog.Description>
            <div className="flex gap-2">
              <Dialog.Close asChild>
                <button type="button" className="flex-1 rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100">
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="button"
                disabled={buyBackMutation.isPending}
                onClick={() => memberToBuyBack && buyBackMutation.mutate(memberToBuyBack.membership_id)}
                className="flex-1 rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
              >
                {buyBackMutation.isPending ? 'Buying back…' : 'Buy back'}
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

      <Dialog.Root
        open={closeModalOpen}
        onOpenChange={(open) => {
          setCloseModalOpen(open)
          if (!open) setCloseConfirmText('')
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/60" />
          <Dialog.Content className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-sm rounded-xl border border-red-900/60 bg-slate-900 p-6 space-y-4">
            <Dialog.Title className="text-lg font-semibold text-slate-100">Close {league.name}?</Dialog.Title>
            <Dialog.Description className="text-sm text-slate-500">
              Every member will be locked out — no more picks, joins, or league changes. The league and its full
              history stay saved, but this can't be undone by you. To confirm, type{' '}
              <span className="font-mono text-slate-200">{closePhrase}</span> below.
            </Dialog.Description>
            <input
              type="text"
              value={closeConfirmText}
              onChange={(e) => setCloseConfirmText(e.target.value)}
              onPaste={(e) => e.preventDefault()}
              onDrop={(e) => e.preventDefault()}
              autoComplete="off"
              spellCheck={false}
              placeholder={closePhrase}
              className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-slate-100 placeholder:text-slate-600"
            />
            {closeMutation.error && (
              <p className="text-sm text-red-400">
                {closeMutation.error instanceof ApiError ? closeMutation.error.message : 'Failed to close league.'}
              </p>
            )}
            <div className="flex gap-2">
              <Dialog.Close asChild>
                <button type="button" className="flex-1 rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100">
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="button"
                disabled={closeConfirmText !== closePhrase || closeMutation.isPending}
                onClick={() => closeMutation.mutate()}
                className="flex-1 rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40 disabled:cursor-not-allowed"
              >
                {closeMutation.isPending ? 'Closing…' : 'Close league'}
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </main>
  )
}
