import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link, useSearchParams } from 'react-router-dom'
import { resetPasswordSchema, type ResetPasswordFormValues } from '../auth/schemas'
import { BrandWordmark } from '../components/BrandWordmark'
import { resetPassword, ApiError } from '../api'

// Reads the reset token from the URL query param (the placeholder frontend
// link pattern the API contract calls for:
// {WEB_BASE_URL}/reset-password?token=...). There's no deep-linking
// infrastructure in this app yet, so the link a user clicks in their email
// lands here directly.
export function ResetPasswordPage() {
  const [searchParams] = useSearchParams()
  const token = searchParams.get('token') ?? ''
  const [succeeded, setSucceeded] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<ResetPasswordFormValues>({ resolver: zodResolver(resetPasswordSchema) })

  const onSubmit = async (values: ResetPasswordFormValues) => {
    setServerError(null)
    try {
      await resetPassword({ token, new_password: values.new_password })
      setSucceeded(true)
    } catch (err) {
      // Backend deliberately returns the same generic message whether the
      // token is malformed, expired, or already used — see
      // api/internal/auth/password_reset.go.
      setServerError(err instanceof ApiError ? err.message : 'Something went wrong. Please try again.')
    }
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <div className="flex justify-center">
          <BrandWordmark size={140} />
        </div>

        <h1 className="text-xl font-semibold text-center">Reset password</h1>

        {!token ? (
          <p className="text-sm text-red-400">
            This link is missing its reset token. Request a new one from the{' '}
            <Link to="/forgot-password" className="underline">
              forgot password
            </Link>{' '}
            page.
          </p>
        ) : succeeded ? (
          <div className="space-y-3">
            <p className="text-sm text-slate-300">
              Your password has been updated. All of your other sessions have been signed out.
            </p>
            <Link
              to="/login"
              className="block w-full text-center rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900"
            >
              Log in
            </Link>
          </div>
        ) : (
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-3" noValidate>
            <div>
              <label htmlFor="new_password" className="block text-sm text-slate-300 mb-1">
                New password
              </label>
              <input
                id="new_password"
                type="password"
                autoComplete="new-password"
                className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
                {...register('new_password')}
              />
              {errors.new_password && <p className="text-red-400 text-xs mt-1">{errors.new_password.message}</p>}
              <p className="text-xs text-slate-500 mt-1">At least 8 characters.</p>
            </div>

            {serverError && <p className="text-red-400 text-sm">{serverError}</p>}

            <button
              type="submit"
              disabled={isSubmitting}
              className="w-full rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
            >
              {isSubmitting ? 'Resetting…' : 'Reset password'}
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
