import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { createLeague, listConferences, ApiError } from '../api'
import { createLeagueSchema, type CreateLeagueFormValues } from '../leagues/schemas'

export function CreateLeaguePage() {
  const navigate = useNavigate()
  const [serverError, setServerError] = useState<string | null>(null)

  const { data: conferences, isLoading: conferencesLoading } = useQuery({
    queryKey: ['conferences'],
    queryFn: listConferences,
  })

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<CreateLeagueFormValues>({
    resolver: zodResolver(createLeagueSchema),
    defaultValues: { season_year: new Date().getFullYear() },
  })

  const onSubmit = async (values: CreateLeagueFormValues) => {
    setServerError(null)
    try {
      const league = await createLeague(values)
      navigate(`/leagues/${league.id}`, { replace: true })
    } catch (err) {
      setServerError(err instanceof ApiError ? err.message : 'Failed to create league. Please try again.')
    }
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-6">
      <div className="max-w-sm w-full space-y-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <div>
          <h1 className="text-xl font-semibold">Create a league</h1>
          <p className="text-sm text-slate-400">You'll be its commissioner and a playing contestant.</p>
        </div>

        <form onSubmit={handleSubmit(onSubmit)} className="space-y-3" noValidate>
          <div>
            <label htmlFor="name" className="block text-sm text-slate-300 mb-1">
              League name
            </label>
            <input
              id="name"
              type="text"
              className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              {...register('name')}
            />
            {errors.name && <p className="text-red-400 text-xs mt-1">{errors.name.message}</p>}
          </div>

          <div>
            <label htmlFor="season_year" className="block text-sm text-slate-300 mb-1">
              Season year
            </label>
            <input
              id="season_year"
              type="number"
              className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              {...register('season_year', { valueAsNumber: true })}
            />
            {errors.season_year && <p className="text-red-400 text-xs mt-1">{errors.season_year.message}</p>}
          </div>

          <div>
            <label htmlFor="conference" className="block text-sm text-slate-300 mb-1">
              Conference
            </label>
            <select
              id="conference"
              defaultValue=""
              className="w-full rounded-md bg-slate-800 border border-slate-700 px-3 py-1.5 text-sm text-slate-100"
              disabled={conferencesLoading}
              {...register('conference')}
            >
              <option value="" disabled>
                {conferencesLoading ? 'Loading conferences…' : 'Select a conference'}
              </option>
              {conferences?.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
            {errors.conference && <p className="text-red-400 text-xs mt-1">{errors.conference.message}</p>}
            <p className="text-xs text-slate-500 mt-1">Locked for the league's lifetime once created.</p>
          </div>

          {serverError && <p className="text-red-400 text-sm">{serverError}</p>}

          <button
            type="submit"
            disabled={isSubmitting}
            className="w-full rounded-md bg-slate-100 px-3 py-1.5 text-sm font-medium text-slate-900 disabled:opacity-50"
          >
            {isSubmitting ? 'Creating…' : 'Create league'}
          </button>
        </form>

        <Link to="/" className="block text-sm text-slate-400 underline text-center">
          Cancel
        </Link>
      </div>
    </main>
  )
}
