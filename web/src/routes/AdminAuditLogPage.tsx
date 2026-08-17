import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AdminNav } from '../components/AdminNav'
import { listAuditLog, ApiError } from '../api'

const PAGE_SIZE = 25

export function AdminAuditLogPage() {
  const [offset, setOffset] = useState(0)
  const [actionFilter, setActionFilter] = useState('')
  const [actorFilter, setActorFilter] = useState('')

  const query = useQuery({
    queryKey: ['admin', 'audit-log', offset, actionFilter, actorFilter],
    queryFn: () =>
      listAuditLog(PAGE_SIZE, offset, {
        action: actionFilter || undefined,
        actor_user_id: actorFilter || undefined,
      }),
  })

  const total = query.data?.total ?? 0
  const hasNext = offset + PAGE_SIZE < total
  const hasPrev = offset > 0

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-2xl mx-auto space-y-4">
        <AdminNav />

        <div className="flex flex-wrap gap-2">
          <input
            type="text"
            placeholder="Filter by action (e.g. resync_game)"
            value={actionFilter}
            onChange={(e) => {
              setActionFilter(e.target.value)
              setOffset(0)
            }}
            className="flex-1 min-w-[12rem] rounded-md border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100"
          />
          <input
            type="text"
            placeholder="Filter by actor_user_id (UUID)"
            value={actorFilter}
            onChange={(e) => {
              setActorFilter(e.target.value)
              setOffset(0)
            }}
            className="flex-1 min-w-[12rem] rounded-md border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100"
          />
        </div>

        <h2 className="text-sm font-medium text-slate-200">
          Audit log {total > 0 && <span className="text-slate-500">({total})</span>}
        </h2>
        {query.isLoading && <p className="text-sm text-slate-400">Loading…</p>}
        {query.error && (
          <p className="text-sm text-red-400">
            {query.error instanceof ApiError ? query.error.message : 'Could not load the audit log.'}
          </p>
        )}

        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          {query.data?.entries.map((entry) => (
            <div key={entry.id} className="p-4 space-y-1">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium text-slate-100">{entry.action}</p>
                <p className="text-xs text-slate-500">{new Date(entry.created_at).toLocaleString()}</p>
              </div>
              <p className="text-xs text-slate-400">
                {entry.actor_user_id ? `actor: ${entry.actor_user_id}` : 'actor: system (cron)'}
                {entry.target_type && ` · target: ${entry.target_type} ${entry.target_id?.slice(0, 8)}…`}
                {entry.league_id && ` · league: ${entry.league_id.slice(0, 8)}…`}
              </p>
              {Object.keys(entry.metadata).length > 0 && (
                <details className="text-xs text-slate-500">
                  <summary className="cursor-pointer select-none">Metadata</summary>
                  <pre className="mt-1 whitespace-pre-wrap break-all rounded bg-slate-950 p-2">
                    {JSON.stringify(entry.metadata, null, 2)}
                  </pre>
                </details>
              )}
            </div>
          ))}
          {query.data?.entries.length === 0 && <p className="p-4 text-sm text-slate-400">No matching entries.</p>}
        </div>

        {total > PAGE_SIZE && (
          <div className="flex items-center justify-between text-sm">
            <button
              type="button"
              disabled={!hasPrev}
              onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
              className="rounded-md border border-slate-700 px-3 py-1.5 text-slate-100 disabled:opacity-40"
            >
              Previous
            </button>
            <span className="text-xs text-slate-500">
              {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
            </span>
            <button
              type="button"
              disabled={!hasNext}
              onClick={() => setOffset((o) => o + PAGE_SIZE)}
              className="rounded-md border border-slate-700 px-3 py-1.5 text-slate-100 disabled:opacity-40"
            >
              Next
            </button>
          </div>
        )}
      </div>
    </main>
  )
}
