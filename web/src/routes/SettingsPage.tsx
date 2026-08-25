import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useMutation } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { BrandWordmark } from '../components/BrandWordmark'
import { settingsSchema, type SettingsFormValues } from '../auth/schemas'
import { updateMe, ApiError } from '../api'

// Account-level settings — currently just the player name (see PATCH /me /
// handlers_me.go, which has always supported this; this page is the first
// UI entry point for it). Team names are per-league and edited from each
// league's own page instead — see LeagueDetailPage's backfill prompt.
export function SettingsPage() {
  const { user, setUser } = useAuth()
  const [savedMessage, setSavedMessage] = useState<string | null>(null)

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<SettingsFormValues>({ resolver: zodResolver(settingsSchema) })

  useEffect(() => {
    if (user) reset({ display_name: user.display_name })
  }, [user, reset])

  const saveMutation = useMutation({
    mutationFn: (values: SettingsFormValues) => updateMe(values),
    onSuccess: (updated) => {
      setUser(updated)
      setSavedMessage('Saved.')
      setTimeout(() => setSavedMessage(null), 2000)
    },
  })

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-6">
      <div className="max-w-lg mx-auto space-y-4">
        <div className="flex justify-center text-lg">
          <BrandWordmark size={200} />
        </div>

        <Link to="/" className="text-xs text-slate-500 underline">
          ← Home
        </Link>

        <div>
          <h1 className="text-xl font-semibold">Settings</h1>
          <p className="text-sm text-slate-500">Manage your account details.</p>
        </div>

        <form
          onSubmit={handleSubmit((values) => saveMutation.mutate(values))}
          className="rounded-xl border border-slate-800 bg-slate-900 p-6 space-y-3"
          noValidate
        >
          <div>
            <label htmlFor="display_name" className="block text-sm text-slate-300 mb-1">
              Player name
            </label>
            <input
              id="display_name"
              type="text"
              autoComplete="name"
              className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              {...register('display_name')}
            />
            {errors.display_name && (
              <p className="text-red-400 text-xs mt-1">{errors.display_name.message}</p>
            )}
          </div>

          {saveMutation.error && (
            <p className="text-sm text-red-400">
              {saveMutation.error instanceof ApiError ? saveMutation.error.message : 'Failed to save.'}
            </p>
          )}

          <div className="flex items-center gap-3">
            <button
              type="submit"
              disabled={isSubmitting || saveMutation.isPending}
              className="rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
            >
              {saveMutation.isPending ? 'Saving…' : 'Save'}
            </button>
            {savedMessage && <span className="text-xs text-emerald-400">{savedMessage}</span>}
          </div>
        </form>
      </div>
    </main>
  )
}
