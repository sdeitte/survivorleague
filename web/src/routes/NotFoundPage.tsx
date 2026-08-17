import { Link } from 'react-router-dom'

// Catch-all for any unmatched path (Phase 9 polish) — previously an
// unknown URL (a stale link, a typo, a bookmark to a since-removed route)
// rendered nothing at all, since react-router leaves the outlet empty when
// no <Route> matches. This gives it the same card treatment as the rest of
// the app instead of a blank page.
export function NotFoundPage() {
  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg text-center">
        <h1 className="text-xl font-semibold">Page not found</h1>
        <p className="text-sm text-slate-400">The page you're looking for doesn't exist or may have moved.</p>
        <Link
          to="/"
          className="block w-full rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900"
        >
          Back to home
        </Link>
      </div>
    </main>
  )
}
