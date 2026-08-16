import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { fetchHealth, API_BASE_URL } from '../api'

// Phase 0's original placeholder page, kept around as a standalone
// unauthenticated health-check view now that HomePage is the real landing
// page (see the plan's Phase 1 instructions: "health check can stay too,
// just isn't the main page anymore").
export function HealthCheckPage() {
  const { data, error, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['health'],
    queryFn: fetchHealth,
    retry: false,
  })

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-md w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <h1 className="text-xl font-semibold">API health check</h1>
        <p className="text-sm text-slate-400">
          Checks whether the web client can reach the Go API at{' '}
          <code className="text-slate-300">{API_BASE_URL}</code>.
        </p>

        <div className="rounded-lg bg-slate-800/60 p-4 text-sm">
          {isLoading && <p>Checking API health…</p>}
          {error && (
            <p className="text-red-400">Could not reach the API: {(error as Error).message}</p>
          )}
          {data && (
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
              <dt className="text-slate-400">status</dt>
              <dd className={data.status === 'ok' ? 'text-emerald-400' : 'text-red-400'}>
                {data.status}
              </dd>
              <dt className="text-slate-400">db</dt>
              <dd className={data.db === 'ok' ? 'text-emerald-400' : 'text-red-400'}>{data.db}</dd>
              {data.error && (
                <>
                  <dt className="text-slate-400">error</dt>
                  <dd className="text-red-400">{data.error}</dd>
                </>
              )}
            </dl>
          )}
        </div>

        <div className="flex items-center justify-between">
          <button
            type="button"
            onClick={() => void refetch()}
            disabled={isFetching}
            className="rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
          >
            {isFetching ? 'Refreshing…' : 'Refresh'}
          </button>
          <Link to="/" className="text-xs text-slate-500 underline">
            Back
          </Link>
        </div>
      </div>
    </main>
  )
}
