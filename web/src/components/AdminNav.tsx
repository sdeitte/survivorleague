import { Link, useLocation } from 'react-router-dom'

const TABS = [
  { to: '/admin', label: 'Overview', exact: true },
  { to: '/admin/leagues', label: 'Leagues' },
  { to: '/admin/users', label: 'Users' },
  { to: '/admin/sync-runs', label: 'Sync runs' },
  { to: '/admin/resync-game', label: 'Resync game' },
  { to: '/admin/audit-log', label: 'Audit log' },
]

// Shared top-of-page nav for every /admin/* screen — a plain tab strip,
// deliberately minimal (this is a site-admin's own oversight tool, not a
// polished member-facing surface — see the plan's Phase 8 scope note).
export function AdminNav() {
  const location = useLocation()

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Site admin</h1>
        <Link to="/" className="text-xs text-slate-500 underline">
          ← Back to app
        </Link>
      </div>
      <nav className="flex flex-wrap gap-2 border-b border-slate-800 pb-3">
        {TABS.map((tab) => {
          const active = tab.exact ? location.pathname === tab.to : location.pathname.startsWith(tab.to)
          return (
            <Link
              key={tab.to}
              to={tab.to}
              className={
                'rounded-md px-3 py-1.5 text-xs font-medium transition-colors ' +
                (active ? 'bg-slate-100 text-slate-900' : 'border border-slate-700 text-slate-300 hover:bg-slate-800')
              }
            >
              {tab.label}
            </Link>
          )
        })}
      </nav>
    </div>
  )
}
