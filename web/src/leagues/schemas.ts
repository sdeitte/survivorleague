import { z } from 'zod'

// Mirrors the backend's validation rules exactly (see
// api/internal/httpapi/validate.go and api/internal/schedule/conferences.go):
// a reasonable season_year range and an exact (case-sensitive) conference
// match, enforced client-side too so a bad selection never round-trips to
// the server only to bounce back as a 400.
export const createLeagueSchema = z.object({
  name: z.string().trim().min(1, 'League name is required'),
  // Plain z.number() (not z.coerce.number()) so the form's input/output
  // types match exactly — the <input type="number"> field uses RHF's
  // valueAsNumber option (see CreateLeaguePage) to hand back a real
  // number, avoiding a coerce-schema's input:unknown/output:number split
  // that zodResolver's generics reject.
  season_year: z
    .number()
    .int('Season year must be a whole number')
    .min(2000, 'Season year must be 2000 or later')
    .max(2100, 'Season year must be 2100 or earlier'),
  conference: z.string().trim().min(1, 'Select a conference'),
})
export type CreateLeagueFormValues = z.infer<typeof createLeagueSchema>

export const joinCodeSchema = z.object({
  code: z
    .string()
    .trim()
    .min(1, 'Enter an invite code')
    .transform((v) => v.toUpperCase()),
})
export type JoinCodeFormValues = z.infer<typeof joinCodeSchema>
