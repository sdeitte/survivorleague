import { z } from 'zod';

// Mirrors the backend's validation rules (see
// api/internal/httpapi/validate.go and api/internal/schedule/conferences.go)
// and the web client's copy of the same (web/src/leagues/schemas.ts).
export const createLeagueSchema = z.object({
  name: z.string().trim().min(1, 'League name is required'),
  season_year: z
    .number()
    .int('Season year must be a whole number')
    .min(2000, 'Season year must be 2000 or later')
    .max(2100, 'Season year must be 2100 or earlier'),
  conference: z.string().trim().min(1, 'Select a conference'),
});
export type CreateLeagueFormValues = z.infer<typeof createLeagueSchema>;

export const joinCodeSchema = z.object({
  code: z
    .string()
    .trim()
    .min(1, 'Enter an invite code')
    .transform((v) => v.toUpperCase()),
});
export type JoinCodeFormValues = z.infer<typeof joinCodeSchema>;
