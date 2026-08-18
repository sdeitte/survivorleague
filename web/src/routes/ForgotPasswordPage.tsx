import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link } from 'react-router-dom'
import { forgotPasswordSchema, type ForgotPasswordFormValues } from '../auth/schemas'
import { BrandWordmark } from '../components/BrandWordmark'
import { forgotPassword, ApiError } from '../api'

// POST /auth/forgot-password always responds 202 with the same message
// whether or not the email matches an account (see api/internal/auth/
// password_reset.go) — so this page always shows the same confirmation
// state on submit, never a per-email success/failure branch. That's a
// deliberate mirror of the backend's non-leaking behavior, not a missing
// error case.
export function ForgotPasswordPage() {
  const [submitted, setSubmitted] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<ForgotPasswordFormValues>({ resolver: zodResolver(forgotPasswordSchema) })

  const onSubmit = async (values: ForgotPasswordFormValues) => {
    setServerError(null)
    try {
      await forgotPassword(values.email)
      setSubmitted(true)
    } catch (err) {
      // Only a genuine client/network failure reaches here — the backend
      // itself never returns a "no such account" error for this endpoint.
      setServerError(err instanceof ApiError ? err.message : 'Something went wrong. Please try again.')
    }
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <div className="flex justify-center">
          <BrandWordmark size={140} />
        </div>

        <h1 className="text-xl font-semibold text-center">Forgot password</h1>

        {submitted ? (
          <p className="text-sm text-slate-300">
            If an account exists for that email, a password reset link has been sent. Check your inbox.
          </p>
        ) : (
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-3" noValidate>
            <div>
              <label htmlFor="email" className="block text-sm text-slate-300 mb-1">
                Email
              </label>
              <input
                id="email"
                type="email"
                autoComplete="email"
                className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
                {...register('email')}
              />
              {errors.email && <p className="text-red-400 text-xs mt-1">{errors.email.message}</p>}
            </div>

            {serverError && <p className="text-red-400 text-sm">{serverError}</p>}

            <button
              type="submit"
              disabled={isSubmitting}
              className="w-full rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
            >
              {isSubmitting ? 'Sending…' : 'Send reset link'}
            </button>
          </form>
        )}

        <p className="text-sm text-slate-500">
          <Link to="/login" className="text-slate-200 underline">
            Back to log in
          </Link>
        </p>
      </div>
    </main>
  )
}
