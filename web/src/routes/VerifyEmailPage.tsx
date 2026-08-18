import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { BrandWordmark } from '../components/BrandWordmark'
import { verifyEmail, ApiError } from '../api'

// Reads the verification token from the URL query param (the placeholder
// frontend link pattern the API contract calls for:
// {WEB_BASE_URL}/verify-email?token=...). There's no deep-linking
// infrastructure in this app yet, so the link a user clicks in their email
// lands here directly. Unlike reset-password, there's no user input needed
// here, so the request fires automatically on mount rather than behind a
// form submit.
export function VerifyEmailPage() {
  const [searchParams] = useSearchParams()
  const token = searchParams.get('token') ?? ''
  const [status, setStatus] = useState<'pending' | 'success' | 'error'>(token ? 'pending' : 'error')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  // The token is single-use server-side, so a second call for the same
  // token legitimately 400s as "already used" — which is exactly what
  // React 18 StrictMode's dev-only double-invoke of this mount effect
  // triggers (fire, succeed/consume, fire again, get rejected), turning a
  // real success into a shown error. This ref persists across that
  // same-instance double-invoke (unlike a `cancelled` closure var per
  // effect run) so only the first call ever goes out.
  const requestedRef = useRef(false)

  useEffect(() => {
    if (!token || requestedRef.current) return
    requestedRef.current = true
    verifyEmail(token)
      .then(() => setStatus('success'))
      .catch((err) => {
        // Backend deliberately returns the same generic message whether the
        // token is malformed, expired, or already used — see
        // api/internal/auth/password_reset.go.
        setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong. Please try again.')
        setStatus('error')
      })
  }, [token])

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <div className="flex justify-center">
          <BrandWordmark size={140} />
        </div>

        <h1 className="text-xl font-semibold text-center">Verify email</h1>

        {status === 'pending' && <p className="text-sm text-slate-300">Verifying your email…</p>}

        {status === 'success' && (
          <div className="space-y-3">
            <p className="text-sm text-slate-300">Your email address has been verified.</p>
            <Link
              to="/"
              className="block w-full text-center rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900"
            >
              Continue
            </Link>
          </div>
        )}

        {status === 'error' && (
          <div className="space-y-3">
            <p className="text-sm text-red-400">
              {token
                ? errorMessage
                : 'This link is missing its verification token.'}
            </p>
            <p className="text-sm text-slate-500">
              You can request a new verification email from the home page once you&apos;re logged in.
            </p>
            <Link to="/login" className="block text-sm text-slate-200 underline">
              Back to log in
            </Link>
          </div>
        )}
      </div>
    </main>
  )
}
