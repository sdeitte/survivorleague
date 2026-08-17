import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AdminNav } from '../components/AdminNav'
import { listAdminLeagues, ApiError } from '../api'

const PAGE_SIZE = 25

export function AdminLeaguesPage() {
  const [offset, setOffset] = useState(0)

  const query = useQuery({
    queryKey: ['admin', 'leagues', offset],
    queryFn: () => listAdminLeagues(PAGE_SIZE, offset),
  })

  const total = query.data?.total ?? 0
  const hasNext = offset + PAGE_SIZE < total
  const hasPrev = offset > 0

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-2xl mx-auto space-y-4">
        <AdminNav />
        <h2 className="text-sm font-medium text-slate-200">
          All leagues {total > 0 && <span className="text-slate-500">({total})</span>}
        </h2>

        {query.isLoading && <p className="text-sm text-slate-400">Loading leagues…</p>}
        {query.error && (
          <p className="text-sm text-red-400">
            {query.error instanceof ApiError ? query.error.message : 'Could not load leagues.'}
          </p>
        )}

        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          {query.data?.leagues.map((league) => (
            <div key={league.id} className="p-4 space-y-1">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium text-slate-100">{league.name}</p>
                <span className="text-xs px-2 py-0.5 rounded-full border border-slate-700 text-slate-300">
                  {league.status}
                </span>
              </div>
              <p className="text-xs text-slate-400">
                {league.conference} · {league.season_year} · {league.member_count} member
                {league.member_count === 1 ? '' : 's'}
              </p>
              <p className="text-xs text-slate-500">
                Commissioner: {league.commissioner.display_name} ({league.commissioner.email})
              </p>
              <p className="text-xs text-slate-600">Created {new Date(league.created_at).toLocaleString()}</p>
            </div>
          ))}
          {query.data?.leagues.length === 0 && <p className="p-4 text-sm text-slate-400">No leagues found.</p>}
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
