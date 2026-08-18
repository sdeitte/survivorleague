import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { BrandWordmark } from '../components/BrandWordmark'
import { previewInvite, joinLeagueByCode, ApiError, type InvitePreviewResponse } from '../api'
import { joinCodeSchema, type JoinCodeFormValues } from '../leagues/schemas'

// Enter a code -> preview the league it invites to -> confirm -> join ->
// navigate to the league. Two steps in one component (rather than two
// routes) since the preview is throwaway state, not something worth a URL.
//
// Also handles a `?code=` query param — the join link sent by
// notify.Service.SendLeagueInviteEmail (commissioner's bulk email-invite
// feature) points here with the code pre-filled, skipping straight to the
// preview step instead of making the recipient re-type it.
//
// This route is deliberately public (see App.tsx) — the preview above uses
// the public GET /invites/{code}, so an anonymous invitee (the common case
// for a fresh email invite) can see what league they're being invited to
// before ever creating an account. Only the actual join step needs auth,
// so an anonymous viewer sees "create an account" / "log in" instead of a
// join button; both carry the code forward (see RegisterPage/LoginPage)
// so it isn't lost on the detour through auth.
export function JoinLeaguePage() {
  const navigate = useNavigate()
  const { user, isLoading: authLoading } = useAuth()
  const [searchParams] = useSearchParams()
  const [preview, setPreview] = useState<(InvitePreviewResponse & { code: string }) | null>(null)
  const [serverError, setServerError] = useState<string | null>(null)
  const [isJoining, setIsJoining] = useState(false)

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<JoinCodeFormValues>({ resolver: zodResolver(joinCodeSchema) })

  const onPreview = async (values: JoinCodeFormValues) => {
    setServerError(null)
    try {
      const result = await previewInvite(values.code)
      setPreview({ ...result, code: values.code })
    } catch (err) {
      setServerError(
        err instanceof ApiError && err.status === 404
          ? 'No league found for that invite code.'
          : err instanceof ApiError
            ? err.message
            : 'Failed to look up invite code. Please try again.',
      )
    }
  }

  useEffect(() => {
    const code = searchParams.get('code')
    if (code) void onPreview({ code })
  }, [searchParams])

  const onConfirmJoin = async () => {
    if (!preview) return
    setServerError(null)
    setIsJoining(true)
    try {
      const league = await joinLeagueByCode(preview.code)
      navigate(`/leagues/${league.id}`, { replace: true })
    } catch (err) {
      setServerError(
        err instanceof ApiError && err.status === 409
          ? "You're already a member of this league."
          : err instanceof ApiError
            ? err.message
            : 'Failed to join league. Please try again.',
      )
    } finally {
      setIsJoining(false)
    }
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <div className="flex justify-center text-base">
          <BrandWordmark size={200} />
        </div>

        <div>
          <h1 className="text-xl font-semibold">Join a league</h1>
          <p className="text-sm text-slate-500">Enter the invite code your commissioner shared.</p>
        </div>

        {!preview ? (
          <form onSubmit={handleSubmit(onPreview)} className="space-y-3" noValidate>
            <div>
              <label htmlFor="code" className="block text-sm text-slate-300 mb-1">
                Invite code
              </label>
              <input
                id="code"
                type="text"
                autoCapitalize="characters"
                className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100 uppercase tracking-widest"
                {...register('code')}
              />
              {errors.code && <p className="text-red-400 text-xs mt-1">{errors.code.message}</p>}
            </div>

            {serverError && <p className="text-red-400 text-sm">{serverError}</p>}

            <button
              type="submit"
              disabled={isSubmitting}
              className="w-full rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
            >
              {isSubmitting ? 'Looking up…' : 'Look up code'}
            </button>
          </form>
        ) : (
          <div className="space-y-3">
            <div className="rounded-lg bg-slate-800/60 p-4 text-sm space-y-1">
              <p className="text-slate-200 font-medium">{preview.league_name}</p>
              <p className="text-slate-500">
                {preview.conference} · {preview.season_year}
              </p>
            </div>

            {serverError && <p className="text-red-400 text-sm">{serverError}</p>}

            {!preview.joinable ? (
              <div className="space-y-2">
                <p className="text-sm text-amber-400">
                  This league has already started and isn't accepting new members — ask your commissioner about
                  other options.
                </p>
                <button
                  type="button"
                  onClick={() => {
                    setPreview(null)
                    setServerError(null)
                  }}
                  className="w-full rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
                >
                  Back
                </button>
              </div>
            ) : authLoading ? (
              <p className="text-sm text-slate-500 text-center">Loading…</p>
            ) : user ? (
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => {
                    setPreview(null)
                    setServerError(null)
                  }}
                  className="flex-1 rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
                >
                  Back
                </button>
                <button
                  type="button"
                  onClick={() => void onConfirmJoin()}
                  disabled={isJoining}
                  className="flex-1 rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
                >
                  {isJoining ? 'Joining…' : 'Join league'}
                </button>
              </div>
            ) : (
              <div className="space-y-2">
                <p className="text-xs text-slate-500">
                  You'll need an account to join. Create one and you'll be added to this league automatically.
                </p>
                <Link
                  to={`/register?code=${encodeURIComponent(preview.code)}`}
                  className="block text-center rounded-md bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-emerald-500"
                >
                  Create an account to join
                </Link>
                <Link
                  to={`/login?code=${encodeURIComponent(preview.code)}`}
                  className="block text-center rounded-md border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
                >
                  Already have an account? Log in
                </Link>
              </div>
            )}
          </div>
        )}

        <Link to="/" className="block text-sm text-slate-500 underline text-center">
          Cancel
        </Link>
      </div>
    </main>
  )
}
