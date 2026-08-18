import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { BrandWordmark } from '../components/BrandWordmark'
import { loginSchema, type LoginFormValues } from '../auth/schemas'
import { ApiError } from '../api'

// A `?code=` param means this login was reached via an emailed
// league-invite join link (see JoinLeaguePage's doc comment) — in that
// case, success sends them back to the join confirm screen (rather than
// home) with the code still attached, one click from being in the league.
export function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const code = searchParams.get('code')
  const [serverError, setServerError] = useState<string | null>(null)

  const {
    register: registerField,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginFormValues>({ resolver: zodResolver(loginSchema) })

  const onSubmit = async (values: LoginFormValues) => {
    setServerError(null)
    try {
      await login(values.email, values.password)
      navigate(code ? `/leagues/join?code=${encodeURIComponent(code)}` : '/', { replace: true })
    } catch (err) {
      setServerError(err instanceof ApiError ? err.message : 'Failed to log in. Please try again.')
    }
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <div className="flex justify-center">
          <BrandWordmark size={140} />
        </div>

        <h1 className="text-xl font-semibold text-center">Log in</h1>

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
              {...registerField('email')}
            />
            {errors.email && <p className="text-red-400 text-xs mt-1">{errors.email.message}</p>}
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label htmlFor="password" className="block text-sm text-slate-300">
                Password
              </label>
              <Link to="/forgot-password" className="text-xs text-slate-500 underline">
                Forgot password?
              </Link>
            </div>
            <input
              id="password"
              type="password"
              autoComplete="current-password"
              className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              {...registerField('password')}
            />
            {errors.password && <p className="text-red-400 text-xs mt-1">{errors.password.message}</p>}
          </div>

          {serverError && <p className="text-red-400 text-sm">{serverError}</p>}

          <button
            type="submit"
            disabled={isSubmitting}
            className="w-full rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
          >
            {isSubmitting ? 'Logging in…' : 'Log in'}
          </button>
        </form>

        <p className="text-sm text-slate-500">
          No account?{' '}
          <Link to={code ? `/register?code=${encodeURIComponent(code)}` : '/register'} className="text-slate-200 underline">
            Register
          </Link>
        </p>
      </div>
    </main>
  )
}
