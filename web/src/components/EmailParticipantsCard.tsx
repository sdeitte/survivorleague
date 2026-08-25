import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { broadcastEmail, listMemberEmails, ApiError, type InviteSendResult } from '../api'

// Commissioner-only card on the league page: copy every current member's
// email address in one click (for mass-emailing outside the app), or
// compose and send one right from here — sent from a fixed noreply
// address (see api/internal/notify.Service.SendLeagueBroadcastEmail), not
// whatever address ordinary transactional email (invites, password
// reset) comes from. Not gated on the league being open/joinable —
// unlike invites, emailing existing participants is a valid thing to do
// even after a league has closed (e.g. a season wrap-up message).
export function EmailParticipantsCard({ leagueId }: { leagueId: string }) {
  const [copied, setCopied] = useState(false)
  const [composing, setComposing] = useState(false)
  const [subject, setSubject] = useState('')
  const [message, setMessage] = useState('')
  const [results, setResults] = useState<InviteSendResult[] | null>(null)

  const membersQuery = useQuery({
    queryKey: ['league', leagueId, 'member-emails'],
    queryFn: () => listMemberEmails(leagueId),
  })

  const sendMutation = useMutation({
    mutationFn: () => broadcastEmail(leagueId, { subject, message }),
    onSuccess: (res) => {
      setResults(res)
      if (res.every((r) => r.sent)) {
        setComposing(false)
        setSubject('')
        setMessage('')
      }
    },
  })

  const copyAllEmails = async () => {
    if (!membersQuery.data) return
    try {
      await navigator.clipboard.writeText(membersQuery.data.map((m) => m.email).join(', '))
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard API can be unavailable (e.g. insecure context) — the
      // count is still visible on screen, so this is a soft failure.
    }
  }

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-3">
      <h2 className="text-sm font-medium text-slate-200">Email participants</h2>
      {membersQuery.isLoading && <p className="text-sm text-slate-500">Loading…</p>}
      {membersQuery.error && (
        <p className="text-sm text-red-400">
          {membersQuery.error instanceof ApiError ? membersQuery.error.message : 'Could not load member emails.'}
        </p>
      )}
      {membersQuery.data && (
        <>
          <div className="flex items-center justify-between">
            <p className="text-xs text-slate-500">{membersQuery.data.length} members</p>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => void copyAllEmails()}
                className="rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              >
                {copied ? 'Copied!' : 'Copy all emails'}
              </button>
              {!composing && (
                <button
                  type="button"
                  onClick={() => {
                    setComposing(true)
                    setResults(null)
                  }}
                  className="rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white"
                >
                  Compose
                </button>
              )}
            </div>
          </div>

          {composing && (
            <div className="space-y-2 border-t border-slate-800 pt-3">
              <input
                type="text"
                placeholder="Subject"
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
                className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-1.5 text-sm text-slate-100 placeholder:text-slate-600"
              />
              <textarea
                placeholder="Message"
                value={message}
                onChange={(e) => setMessage(e.target.value)}
                rows={5}
                maxLength={5000}
                className="w-full rounded-md border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-slate-100 placeholder:text-slate-600"
              />
              {sendMutation.error && (
                <p className="text-sm text-red-400">
                  {sendMutation.error instanceof ApiError ? sendMutation.error.message : 'Failed to send email.'}
                </p>
              )}
              <div className="flex items-center justify-between">
                <button
                  type="button"
                  onClick={() => {
                    setComposing(false)
                    setResults(null)
                  }}
                  className="text-sm text-slate-500 underline"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  disabled={!subject.trim() || !message.trim() || sendMutation.isPending}
                  onClick={() => sendMutation.mutate()}
                  className="rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
                >
                  {sendMutation.isPending ? 'Sending…' : `Send to ${membersQuery.data.length} members`}
                </button>
              </div>
            </div>
          )}

          {results && (
            <div className="space-y-1 border-t border-slate-800 pt-2">
              {results.map((r, i) => (
                <p key={r.email + i} className={'text-xs ' + (r.sent ? 'text-emerald-400' : 'text-red-400')}>
                  {r.email} — {r.sent ? 'sent' : r.error}
                </p>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
