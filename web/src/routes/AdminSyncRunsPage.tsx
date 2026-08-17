import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminNav } from '../components/AdminNav'
import { listSyncRuns, triggerScheduleSync, ApiError } from '../api'

const currentSeasonYear = () => {
  const now = new Date()
  // Mirrors api/cmd/server/main.go's currentSeasonYear: Jul-Dec is "this
  // year's" season, Jan-Jun is still last year's.
  return now.getMonth() + 1 >= 7 ? now.getFullYear() : now.getFullYear() - 1
}

export function AdminSyncRunsPage() {
  const queryClient = useQueryClient()
  const [seasonYear, setSeasonYear] = useState(currentSeasonYear())

  const query = useQuery({
    queryKey: ['admin', 'sync-runs'],
    queryFn: listSyncRuns,
    refetchInterval: 15_000,
  })

  const triggerMutation = useMutation({
    mutationFn: () => triggerScheduleSync(seasonYear),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['admin', 'sync-runs'] }),
  })

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-2xl mx-auto space-y-4">
        <AdminNav />

        <div className="rounded-xl border border-slate-800 bg-slate-900 p-4 space-y-3">
          <h2 className="text-sm font-medium text-slate-200">Trigger a schedule sync</h2>
          <div className="flex items-center gap-2">
            <input
              type="number"
              value={seasonYear}
              onChange={(e) => setSeasonYear(Number(e.target.value))}
              className="w-28 rounded-md border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100"
            />
            <button
              type="button"
              disabled={triggerMutation.isPending}
              onClick={() => triggerMutation.mutate()}
              className="rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
            >
              {triggerMutation.isPending ? 'Syncing…' : 'Sync now'}
            </button>
          </div>
          {triggerMutation.isSuccess && (
            <p className="text-xs text-emerald-400">
              Sync {triggerMutation.data.status === 'success' ? 'completed' : 'recorded'} — status:{' '}
              {triggerMutation.data.status}
              {triggerMutation.data.error ? ` (${triggerMutation.data.error})` : ''}
            </p>
          )}
          {triggerMutation.error && (
            <p className="text-xs text-red-400">
              {triggerMutation.error instanceof ApiError ? triggerMutation.error.message : 'Failed to trigger sync.'}
            </p>
          )}
        </div>

        <h2 className="text-sm font-medium text-slate-200">Recent sync runs</h2>
        {query.isLoading && <p className="text-sm text-slate-400">Loading…</p>}
        {query.error && (
          <p className="text-sm text-red-400">
            {query.error instanceof ApiError ? query.error.message : 'Could not load sync runs.'}
          </p>
        )}

        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          {query.data?.map((run) => (
            <div key={run.id} className="p-4 space-y-1">
              <div className="flex items-center justify-between">
                <p className="text-sm text-slate-100">{run.kind}</p>
                <span
                  className={
                    'text-xs px-2 py-0.5 rounded-full border ' +
                    (run.status === 'success'
                      ? 'border-emerald-700 text-emerald-400'
                      : run.status === 'failed'
                        ? 'border-red-700 text-red-400'
                        : 'border-amber-700 text-amber-400')
                  }
                >
                  {run.status}
                </span>
              </div>
              <p className="text-xs text-slate-500">
                Started {new Date(run.started_at).toLocaleString()}
                {run.finished_at && ` · finished ${new Date(run.finished_at).toLocaleString()}`}
              </p>
              {run.error && <p className="text-xs text-red-400">{run.error}</p>}
              <details className="text-xs text-slate-500">
                <summary className="cursor-pointer select-none">Details</summary>
                <pre className="mt-1 whitespace-pre-wrap break-all rounded bg-slate-950 p-2">
                  {JSON.stringify(run.details, null, 2)}
                </pre>
              </details>
            </div>
          ))}
          {query.data?.length === 0 && <p className="p-4 text-sm text-slate-400">No sync runs yet.</p>}
        </div>
      </div>
    </main>
  )
}
