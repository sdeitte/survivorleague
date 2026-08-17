import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import * as Dialog from '@radix-ui/react-dialog'
import { AdminNav } from '../components/AdminNav'
import { useAuth } from '../auth/AuthContext'
import { listAdminUsers, disableUser, enableUser, ApiError, type AdminUser } from '../api'

const PAGE_SIZE = 25

export function AdminUsersPage() {
  const { user: me } = useAuth()
  const queryClient = useQueryClient()
  const [offset, setOffset] = useState(0)
  const [userToDisable, setUserToDisable] = useState<AdminUser | null>(null)
  const [userToEnable, setUserToEnable] = useState<AdminUser | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const query = useQuery({
    queryKey: ['admin', 'users', offset],
    queryFn: () => listAdminUsers(PAGE_SIZE, offset),
  })

  const disableMutation = useMutation({
    mutationFn: (userId: string) => disableUser(userId),
    onSuccess: () => {
      setUserToDisable(null)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
    },
    onError: (err) => {
      setActionError(err instanceof ApiError ? err.message : 'Failed to disable user.')
      setUserToDisable(null)
    },
  })

  const enableMutation = useMutation({
    mutationFn: (userId: string) => enableUser(userId),
    onSuccess: () => {
      setUserToEnable(null)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })
    },
    onError: (err) => {
      setActionError(err instanceof ApiError ? err.message : 'Failed to enable user.')
      setUserToEnable(null)
    },
  })

  const total = query.data?.total ?? 0
  const hasNext = offset + PAGE_SIZE < total
  const hasPrev = offset > 0

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-2xl mx-auto space-y-4">
        <AdminNav />
        <h2 className="text-sm font-medium text-slate-200">
          All users {total > 0 && <span className="text-slate-500">({total})</span>}
        </h2>

        {query.isLoading && <p className="text-sm text-slate-400">Loading users…</p>}
        {query.error && (
          <p className="text-sm text-red-400">
            {query.error instanceof ApiError ? query.error.message : 'Could not load users.'}
          </p>
        )}
        {actionError && <p className="text-sm text-red-400">{actionError}</p>}

        <div className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
          {query.data?.users.map((u) => (
            <div key={u.id} className="flex items-center justify-between p-4">
              <div>
                <p className="text-sm font-medium text-slate-100">
                  {u.display_name} {u.is_site_admin && <span className="text-xs text-emerald-400">(site admin)</span>}
                </p>
                <p className="text-xs text-slate-400">{u.email}</p>
                <p className="text-xs text-slate-500">
                  {u.league_count} league{u.league_count === 1 ? '' : 's'} · joined{' '}
                  {new Date(u.created_at).toLocaleDateString()}
                </p>
              </div>
              <div className="flex items-center gap-3">
                <span
                  className={
                    'text-xs px-2 py-0.5 rounded-full border ' +
                    (u.status === 'active' ? 'border-emerald-700 text-emerald-400' : 'border-red-700 text-red-400')
                  }
                >
                  {u.status}
                </span>
                {u.status === 'active' ? (
                  <button
                    type="button"
                    disabled={u.id === me?.id}
                    title={u.id === me?.id ? "You can't disable your own account" : undefined}
                    onClick={() => setUserToDisable(u)}
                    className="text-xs text-red-400 underline disabled:opacity-30 disabled:no-underline"
                  >
                    Disable
                  </button>
                ) : (
                  <button type="button" onClick={() => setUserToEnable(u)} className="text-xs text-emerald-400 underline">
                    Enable
                  </button>
                )}
              </div>
            </div>
          ))}
          {query.data?.users.length === 0 && <p className="p-4 text-sm text-slate-400">No users found.</p>}
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

      <Dialog.Root open={!!userToDisable} onOpenChange={(open) => !open && setUserToDisable(null)}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/60" />
          <Dialog.Content className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-sm rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-4">
            <Dialog.Title className="text-lg font-semibold text-slate-100">Disable this account?</Dialog.Title>
            <Dialog.Description className="text-sm text-slate-400">
              {userToDisable?.display_name} ({userToDisable?.email}) will be unable to log in until re-enabled.
              Any session already in progress stays valid for up to 15 minutes (access token expiry). This can
              be undone at any time from this page.
            </Dialog.Description>
            <div className="flex gap-2">
              <Dialog.Close asChild>
                <button type="button" className="flex-1 rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100">
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="button"
                disabled={disableMutation.isPending}
                onClick={() => userToDisable && disableMutation.mutate(userToDisable.id)}
                className="flex-1 rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
              >
                {disableMutation.isPending ? 'Disabling…' : 'Disable'}
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

      <Dialog.Root open={!!userToEnable} onOpenChange={(open) => !open && setUserToEnable(null)}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/60" />
          <Dialog.Content className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-sm rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-4">
            <Dialog.Title className="text-lg font-semibold text-slate-100">Re-enable this account?</Dialog.Title>
            <Dialog.Description className="text-sm text-slate-400">
              {userToEnable?.display_name} ({userToEnable?.email}) will be able to log in again immediately.
            </Dialog.Description>
            <div className="flex gap-2">
              <Dialog.Close asChild>
                <button type="button" className="flex-1 rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100">
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="button"
                disabled={enableMutation.isPending}
                onClick={() => userToEnable && enableMutation.mutate(userToEnable.id)}
                className="flex-1 rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
              >
                {enableMutation.isPending ? 'Enabling…' : 'Enable'}
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </main>
  )
}
