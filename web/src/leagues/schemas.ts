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
  // Required at creation time for the commissioner's own membership, same
  // rule JoinByCode enforces for every other joiner — see
  // internal/leagues.Service's validateTeamName (60 char cap, mirrored
  // here so a too-long name is caught before it round-trips).
  team_name: z.string().trim().min(1, 'Team name is required').max(60, 'Team name is too long'),
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

// Separate schema/form from joinCodeSchema above — JoinLeaguePage is a
// two-step flow (look up code, then confirm join) using two independent
// useForm instances. Sharing one form/schema across both steps would make
// step 1's submit also validate team_name, a field that isn't even
// rendered yet at that point.
export const confirmJoinSchema = z.object({
  team_name: z.string().trim().min(1, 'Team name is required').max(60, 'Team name is too long'),
})
export type ConfirmJoinFormValues = z.infer<typeof confirmJoinSchema>
