import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import * as Dialog from '@radix-ui/react-dialog'
import {
  getInviteCode,
  getLeague,
  listMembers,
  regenerateInviteCode,
  removeMember,
  updateLeague,
  ApiError,
  type Member,
} from '../api'

export function LeagueDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [actionError, setActionError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [memberToRemove, setMemberToRemove] = useState<Member | null>(null)

  const leagueQuery = useQuery({
    queryKey: ['league', id],
    queryFn: () => getLeague(id!),
    enabled: !!id,
  })
  const membersQuery = useQuery({
    queryKey: ['league', id, 'members'],
    queryFn: () => listMembers(id!),
    enabled: !!id,
  })

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
      void queryClient.invalidateQueries({ queryKey: ['league', id, 'members'] })
    },
    onError: (err) => {
      setActionError(err instanceof ApiError ? err.message : 'Failed to remove member.')
      setMemberToRemove(null)
    },
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

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <Link to="/" className="text-xs text-slate-500 underline">
          ← My Leagues
        </Link>

        <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-1">
          <div className="flex items-center justify-between">
            <h1 className="text-xl font-semibold">{league.name}</h1>
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
          <p className="text-sm text-slate-400">
            {league.conference} · {league.season_year}
          </p>

          {isCommissioner && (
            <label className="flex items-center gap-2 pt-3 text-sm text-slate-300">
              <input
                type="checkbox"
                checked={league.membership.is_contestant}
                disabled={toggleContestantMutation.isPending}
                onChange={(e) => toggleContestantMutation.mutate(e.target.checked)}
                className="rounded border-slate-700 bg-slate-800"
              />
              Playing as a contestant (uncheck to manage only)
            </label>
          )}
        </div>

        {isCommissioner && (
          <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-3">
            <h2 className="text-sm font-medium text-slate-200">Invite code</h2>
            {inviteQuery.isLoading && <p className="text-sm text-slate-400">Loading…</p>}
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
              className="text-sm text-slate-400 underline disabled:opacity-50"
            >
              {regenerateMutation.isPending ? 'Regenerating…' : 'Regenerate code (invalidates the old one)'}
            </button>
          </div>
        )}

        {actionError && <p className="text-red-400 text-sm">{actionError}</p>}

        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          <h2 className="p-4 text-sm font-medium text-slate-200">Members</h2>
          {membersQuery.isLoading && <p className="p-4 text-sm text-slate-400">Loading members…</p>}
          {membersQuery.data?.map((member) => (
            <div key={member.membership_id} className="flex items-center justify-between p-4">
              <div>
                <p className="text-sm text-slate-100">{member.display_name}</p>
                <p className="text-xs text-slate-400">
                  {member.role}
                  {!member.is_contestant && ' · not playing'}
                  {member.status === 'eliminated' && ' · eliminated'}
                </p>
              </div>
              {isCommissioner && member.role !== 'commissioner' && (
                <button
                  type="button"
                  onClick={() => setMemberToRemove(member)}
                  className="text-xs text-red-400 underline"
                >
                  Remove
                </button>
              )}
            </div>
          ))}
        </div>
      </div>

      <Dialog.Root open={!!memberToRemove} onOpenChange={(open) => !open && setMemberToRemove(null)}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/60" />
          <Dialog.Content className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-sm rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-4">
            <Dialog.Title className="text-lg font-semibold text-slate-100">Remove member?</Dialog.Title>
            <Dialog.Description className="text-sm text-slate-400">
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
    </main>
  )
}
