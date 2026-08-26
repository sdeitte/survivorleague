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
  // Required at creation time for the commissioner's own membership, same
  // rule joining by code enforces for every other joiner.
  team_name: z.string().trim().min(1, 'Team name is required').max(60, 'Team name is too long'),
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

// Separate schema/form from joinCodeSchema above — JoinLeagueScreen is a
// two-step flow (look up code, then confirm join) using two independent
// forms, mirroring web's identical split (see web/src/leagues/schemas.ts's
// comment for why sharing one form/schema across both steps is a bug:
// step 1's submit would also validate team_name, a field not even
// rendered yet at that point).
export const confirmJoinSchema = z.object({
  team_name: z.string().trim().min(1, 'Team name is required').max(60, 'Team name is too long'),
});
export type ConfirmJoinFormValues = z.infer<typeof confirmJoinSchema>;
