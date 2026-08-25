import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { deleteMessage, listMessages, postMessage, ApiError } from '../api'

// The league smack-talk feed — a height-bounded, independently-scrolling
// box so the leaderboard below it on the overview page stays visible
// without the chat pushing everything down. Messages older than 7 days
// are already excluded server-side (see internal/chat.Service) — nothing
// client-side to filter. Polls every 10s (faster than the 60s leaderboard
// poll elsewhere in this app — this is the one thing on the page meant to
// feel live). No automated moderation (see the plan's explicit call to
// skip a profanity filter) — isCommissioner gets a delete affordance per
// message instead.
export function LeagueChat({ leagueId, isCommissioner }: { leagueId: string; isCommissioner: boolean }) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)

  const messagesQuery = useQuery({
    queryKey: ['league', leagueId, 'messages'],
    queryFn: () => listMessages(leagueId),
    refetchInterval: 10_000,
  })

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messagesQuery.data])

  const postMutation = useMutation({
    mutationFn: (body: string) => postMessage(leagueId, body),
    onSuccess: (msg) => {
      setDraft('')
      queryClient.setQueryData(['league', leagueId, 'messages'], (old: typeof messagesQuery.data) =>
        old ? [...old, msg] : [msg],
      )
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (messageId: string) => deleteMessage(leagueId, messageId),
    onSuccess: (_data, messageId) => {
      queryClient.setQueryData(['league', leagueId, 'messages'], (old: typeof messagesQuery.data) =>
        old?.filter((m) => m.id !== messageId),
      )
    },
  })

  const submit = () => {
    const body = draft.trim()
    if (!body || postMutation.isPending) return
    postMutation.mutate(body)
  }

  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-4 space-y-3">
      <h2 className="text-sm font-medium text-slate-200">League chat</h2>

      <div ref={scrollRef} className="max-h-64 overflow-y-auto space-y-2 pr-1">
        {messagesQuery.isLoading && <p className="text-xs text-slate-500">Loading messages…</p>}
        {messagesQuery.error && (
          <p className="text-xs text-red-400">
            {messagesQuery.error instanceof ApiError ? messagesQuery.error.message : 'Could not load chat.'}
          </p>
        )}
        {messagesQuery.data?.length === 0 && (
          <p className="text-xs text-slate-500">No messages yet — say something.</p>
        )}
        {messagesQuery.data?.map((m) => (
          <div key={m.id} className="group flex items-start justify-between gap-2 text-sm">
            <p className="min-w-0">
              <span className="text-slate-400 font-medium">{m.team_name || m.display_name}</span>{' '}
              <span className="text-slate-100 break-words">{m.body}</span>
            </p>
            {isCommissioner && (
              <button
                type="button"
                onClick={() => deleteMutation.mutate(m.id)}
                disabled={deleteMutation.isPending}
                className="shrink-0 text-xs text-slate-600 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-opacity"
              >
                delete
              </button>
            )}
          </div>
        ))}
      </div>

      <div className="flex items-center gap-2 pt-1">
        <input
          type="text"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit()
          }}
          placeholder="Say something…"
          maxLength={1000}
          className="flex-1 rounded-md border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 placeholder:text-slate-600"
        />
        <button
          type="button"
          onClick={submit}
          disabled={!draft.trim() || postMutation.isPending}
          className="rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
        >
          Send
        </button>
      </div>
      {postMutation.error && (
        <p className="text-xs text-red-400">
          {postMutation.error instanceof ApiError ? postMutation.error.message : 'Failed to send message.'}
        </p>
      )}
    </div>
  )
}
