import { Link } from 'react-router-dom'
import { AdminNav } from '../components/AdminNav'

const LINKS = [
  { to: '/admin/leagues', title: 'Leagues', description: 'Every league across the whole system, with commissioner and member count.' },
  { to: '/admin/users', title: 'Users', description: 'Every user, with account status and how many leagues they belong to. Disable/enable accounts here.' },
  { to: '/admin/sync-runs', title: 'Sync runs', description: 'History of CFBD schedule syncs, and a button to trigger one manually.' },
  { to: '/admin/resync-game', title: 'Resync game', description: 'Re-fetch a single game from CFBD — the fix for a game stuck postponed/canceled and blocking a league-week.' },
  { to: '/admin/audit-log', title: 'Audit log', description: 'Every privileged commissioner/admin action, newest first.' },
]

export function AdminOverviewPage() {
  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <AdminNav />
        <p className="text-sm text-slate-400">
          Cross-league oversight tools. These act on the whole system, not just leagues you're a member of.
        </p>
        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          {LINKS.map((link) => (
            <Link key={link.to} to={link.to} className="block p-4 hover:bg-slate-800/60 transition-colors">
              <p className="text-sm font-medium text-slate-100">{link.title}</p>
              <p className="text-xs text-slate-400 mt-1">{link.description}</p>
            </Link>
          ))}
        </div>
      </div>
    </main>
  )
}
