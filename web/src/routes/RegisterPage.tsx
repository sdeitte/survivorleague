import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { BrandWordmark } from '../components/BrandWordmark'
import { registerSchema, type RegisterFormValues } from '../auth/schemas'
import { ApiError, joinLeagueByCode } from '../api'

// A `?code=` param means this registration was reached via an emailed
// league-invite join link (see JoinLeaguePage's doc comment) — in that
// case, joining is folded directly into account creation so a brand-new
// invitee lands straight in the league instead of a separate confirm
// click. Any failure in that join step (network blip, code since
// regenerated, etc.) falls back to /leagues/join?code=... — the account
// still exists, so they can retry the join manually from there.
export function RegisterPage() {
  const { register: registerAccount } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const code = searchParams.get('code')
  const [serverError, setServerError] = useState<string | null>(null)

  const {
    register: registerField,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<RegisterFormValues>({ resolver: zodResolver(registerSchema) })

  const onSubmit = async (values: RegisterFormValues) => {
    setServerError(null)
    try {
      await registerAccount(values.email, values.password, values.display_name)
    } catch (err) {
      setServerError(err instanceof ApiError ? err.message : 'Failed to register. Please try again.')
      return
    }

    if (!code) {
      navigate('/', { replace: true })
      return
    }
    try {
      const league = await joinLeagueByCode(code)
      navigate(`/leagues/${league.id}`, { replace: true })
    } catch {
      // Account creation already succeeded — send them to the confirm
      // screen to retry the join rather than losing the account.
      navigate(`/leagues/join?code=${encodeURIComponent(code)}`, { replace: true })
    }
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <div className="flex justify-center">
          <BrandWordmark size={140} />
        </div>

        <div>
          <h1 className="text-xl font-semibold text-center">Create an account</h1>
          {code && (
            <p className="text-sm text-slate-500 text-center">
              You'll join your league automatically after creating your account.
            </p>
          )}
        </div>

        <form onSubmit={handleSubmit(onSubmit)} className="space-y-3" noValidate>
          <div>
            <label htmlFor="display_name" className="block text-sm text-slate-300 mb-1">
              Display name
            </label>
            <input
              id="display_name"
              type="text"
              autoComplete="name"
              className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              {...registerField('display_name')}
            />
            {errors.display_name && (
              <p className="text-red-400 text-xs mt-1">{errors.display_name.message}</p>
            )}
          </div>

          <div>
            <label htmlFor="email" className="block text-sm text-slate-300 mb-1">
              Email
            </label>
            <input
              id="email"
              type="email"
              autoComplete="email"
              className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              {...registerField('email')}
            />
            {errors.email && <p className="text-red-400 text-xs mt-1">{errors.email.message}</p>}
          </div>

          <div>
            <label htmlFor="password" className="block text-sm text-slate-300 mb-1">
              Password
            </label>
            <input
              id="password"
              type="password"
              autoComplete="new-password"
              className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              {...registerField('password')}
            />
            {errors.password && <p className="text-red-400 text-xs mt-1">{errors.password.message}</p>}
            <p className="text-xs text-slate-500 mt-1">At least 8 characters.</p>
          </div>

          {serverError && <p className="text-red-400 text-sm">{serverError}</p>}

          <button
            type="submit"
            disabled={isSubmitting}
            className="w-full rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
          >
            {isSubmitting ? 'Creating account…' : 'Create account'}
          </button>
        </form>

        <p className="text-sm text-slate-500">
          Already have an account?{' '}
          <Link to={code ? `/login?code=${encodeURIComponent(code)}` : '/login'} className="text-slate-200 underline">
            Log in
          </Link>
        </p>
      </div>
    </main>
  )
}
