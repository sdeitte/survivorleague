import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { ApiError, getNotificationPreferences, updateNotificationPreferences } from '../api'
import type { NotificationPreferences } from '../api'

// A simple toggle-list preferences screen (Phase 7) — every type defaults
// on (see notification_preferences' column defaults); `survived` is the
// one the plan explicitly calls out as opt-out framing (push-only, on by
// default, the user can disable it), but the control surface is identical
// for every type: a plain checkbox the user can flip either way.
const TYPE_TOGGLES: { key: keyof NotificationPreferences; label: string; description: string }[] = [
  { key: 'pick_reminder', label: 'Pick reminders', description: "Nudges when you haven't picked yet, ~24h and ~3h before your deadline." },
  { key: 'eliminated', label: 'Eliminated', description: "When you're eliminated from a league." },
  { key: 'survived', label: 'Survived', description: 'When your pick holds up for the week (push only).' },
  { key: 'mass_wipeout', label: 'Mass wipeout', description: 'When everyone in a league loses and nobody is eliminated that week.' },
  { key: 'buyback', label: 'Buy-back', description: 'When a commissioner reinstates you after an elimination.' },
]

const CHANNEL_TOGGLES: { key: keyof NotificationPreferences; label: string; description: string }[] = [
  { key: 'push_enabled', label: 'Push notifications', description: 'Delivered to your registered devices via the mobile app.' },
  { key: 'email_enabled', label: 'Email', description: 'Delivered to your account email.' },
]

export function NotificationPreferencesPage() {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<NotificationPreferences | null>(null)
  const [savedMessage, setSavedMessage] = useState<string | null>(null)

  const prefsQuery = useQuery({
    queryKey: ['notification-preferences'],
    queryFn: getNotificationPreferences,
  })

  useEffect(() => {
    if (prefsQuery.data && !draft) setDraft(prefsQuery.data)
  }, [prefsQuery.data, draft])

  const saveMutation = useMutation({
    mutationFn: (prefs: NotificationPreferences) => updateNotificationPreferences(prefs),
    onSuccess: (prefs) => {
      queryClient.setQueryData(['notification-preferences'], prefs)
      setDraft(prefs)
      setSavedMessage('Saved.')
      setTimeout(() => setSavedMessage(null), 2000)
    },
  })

  const toggle = (key: keyof NotificationPreferences) => {
    if (!draft) return
    const next = { ...draft, [key]: !draft[key] }
    setDraft(next)
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <Link to="/" className="text-xs text-slate-500 underline">
          ← Home
        </Link>

        <div>
          <h1 className="text-xl font-semibold">Notification preferences</h1>
          <p className="text-sm text-slate-400">Choose what you get notified about, and how.</p>
        </div>

        {prefsQuery.isLoading && <p className="text-sm text-slate-400">Loading preferences…</p>}
        {prefsQuery.error && (
          <p className="text-sm text-red-400">
            {prefsQuery.error instanceof ApiError ? prefsQuery.error.message : 'Could not load your preferences.'}
          </p>
        )}

        {draft && (
          <>
            <section className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
              {TYPE_TOGGLES.map(({ key, label, description }) => (
                <label key={key} className="flex items-start gap-3 p-4 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={draft[key]}
                    onChange={() => toggle(key)}
                    className="mt-0.5 h-4 w-4 rounded border-slate-600 bg-slate-800 text-emerald-500"
                  />
                  <span>
                    <span className="block text-sm text-slate-100">{label}</span>
                    <span className="block text-xs text-slate-500">{description}</span>
                  </span>
                </label>
              ))}
            </section>

            <section className="rounded-xl border border-slate-800 bg-slate-900 divide-y divide-slate-800">
              {CHANNEL_TOGGLES.map(({ key, label, description }) => (
                <label key={key} className="flex items-start gap-3 p-4 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={draft[key]}
                    onChange={() => toggle(key)}
                    className="mt-0.5 h-4 w-4 rounded border-slate-600 bg-slate-800 text-emerald-500"
                  />
                  <span>
                    <span className="block text-sm text-slate-100">{label}</span>
                    <span className="block text-xs text-slate-500">{description}</span>
                  </span>
                </label>
              ))}
            </section>

            {saveMutation.error && (
              <p className="text-sm text-red-400">
                {saveMutation.error instanceof ApiError ? saveMutation.error.message : 'Failed to save preferences.'}
              </p>
            )}

            <div className="flex items-center gap-3">
              <button
                type="button"
                disabled={saveMutation.isPending}
                onClick={() => saveMutation.mutate(draft)}
                className="rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
              >
                {saveMutation.isPending ? 'Saving…' : 'Save preferences'}
              </button>
              {savedMessage && <span className="text-xs text-emerald-400">{savedMessage}</span>}
            </div>
          </>
        )}
      </div>
    </main>
  )
}
