// Thin fetch wrapper around the Go API. Auth calls (register/login/
// refresh/logout) hit the backend directly; everything else goes through
// apiFetch, which attaches `Authorization: Bearer <access_token>`, always
// sends `credentials: 'include'` (so the httpOnly refresh_token cookie
// round-trips), and on a 401 attempts one silent POST /auth/refresh before
// retrying the original request once.
//
// Once the OpenAPI spec grows further, this can be replaced by the
// generated client in packages/api-client — kept hand-rolled for now to
// match Phase 1's small surface.
export const API_BASE_URL: string =
  import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080'

export interface HealthResponse {
  status: 'ok' | 'error'
  db: 'ok' | 'error'
  error?: string
}

export interface User {
  id: string
  email: string
  display_name: string
  is_site_admin: boolean
  // null until POST /auth/verify-email succeeds for this user — see
  // MessageResponse/the forgot-password/reset-password/verify-email
  // functions below (post-Phase-10 addition).
  email_verified_at: string | null
}

// --- Admin (Phase 8) ---

export interface AdminCommissioner {
  id: string
  display_name: string
  email: string
}

export interface AdminLeague {
  id: string
  name: string
  conference: string
  season_year: number
  status: 'active'
  commissioner: AdminCommissioner
  member_count: number
  created_at: string
}

export interface AdminLeaguesListResponse {
  leagues: AdminLeague[]
  total: number
  limit: number
  offset: number
}

export interface AdminUser {
  id: string
  email: string
  display_name: string
  is_site_admin: boolean
  status: 'active' | 'disabled'
  league_count: number
  created_at: string
}

export interface AdminUsersListResponse {
  users: AdminUser[]
  total: number
  limit: number
  offset: number
}

export interface FinalizedLeagueWeek {
  league_id: string
  week_id: string
  mass_wipeout: boolean
}

export interface ResyncGameResponse {
  game: Game
  finalized_league_weeks: FinalizedLeagueWeek[]
}

export interface AuditLogEntry {
  id: string
  actor_user_id?: string
  league_id?: string
  action: string
  target_type?: string
  target_id?: string
  metadata: Record<string, unknown>
  created_at: string
}

export interface AuditLogListResponse {
  entries: AuditLogEntry[]
  total: number
  limit: number
  offset: number
}

export interface SyncRun {
  id: string
  kind: 'schedule'
  status: 'running' | 'success' | 'failed'
  started_at: string
  finished_at?: string
  error?: string
  details: Record<string, unknown>
  created_at: string
}

export interface SessionResponse {
  access_token: string
  refresh_token?: string
  user: User
}

export interface MembershipSummary {
  id: string
  role: 'commissioner' | 'player'
  is_contestant: boolean
  status: 'active' | 'eliminated'
}

export interface League {
  id: string
  name: string
  season_year: number
  conference: string
  commissioner_user_id: string
  status: 'active' | 'closed'
  created_at: string
  membership: MembershipSummary
}

export interface Member {
  membership_id: string
  user_id: string
  display_name: string
  role: 'commissioner' | 'player'
  is_contestant: boolean
  status: 'active' | 'eliminated'
  bought_back: boolean
  joined_at: string
}

// Membership is the full record returned by the buy-back endpoint —
// richer than Member/MembershipSummary since it surfaces the
// eliminated_*/bought_back_* fields a buy-back response needs to show.
export interface Membership {
  membership_id: string
  league_id: string
  user_id: string
  role: 'commissioner' | 'player'
  is_contestant: boolean
  status: 'active' | 'eliminated'
  eliminated_week_id?: string
  eliminated_game_id?: string
  bought_back: boolean
  bought_back_at?: string
  bought_back_by?: string
}

export interface InviteCodeResponse {
  invite_code: string
  // False once the league is closed, or once its conference's week 1 has
  // no pickable games left — the commissioner's own invite code/invite-by-
  // email UI hides once this flips, since no one can actually join anymore.
  joinable: boolean
}

export interface InvitePreviewResponse {
  league_name: string
  conference: string
  season_year: number
  // False once the league is closed, or once its conference's week 1 has
  // no pickable games left (games have started — no new members mid-season).
  joinable: boolean
}

export interface Week {
  id: string
  season_year: number
  week_number: number
}

export interface GameTeam {
  id: string
  name: string
  conference: string
  logo_url?: string
}

export interface Game {
  id: string
  external_id: string
  week_id: string
  kickoff_at: string
  status: 'scheduled' | 'in_progress' | 'final' | 'postponed' | 'canceled'
  home_team: GameTeam
  away_team: GameTeam
  home_score?: number
  away_score?: number
  winner_team_id?: string
}

export interface Pick {
  game_id: string
  team_id: string
  locked: boolean
}

export interface AvailableTeam {
  team_id: string
  team_name: string
  team_logo_url?: string
  opponent_team_id: string
  opponent_name: string
  opponent_logo_url?: string
  game_id: string
  kickoff_at: string
  is_home: boolean
  is_locked: boolean
  is_used_elsewhere: boolean
  is_current_pick: boolean
  // Matchup-predictor decision-support data — shown while a member is
  // still deciding, not lock-gated. win_probability/spread are absent
  // until CFBD publishes them (usually within ~1 week of kickoff);
  // sp_plus_rank/opponent_sp_plus_rank are the season-long fallback
  // signal, available earlier.
  win_probability?: number
  spread?: number
  sp_plus_rank?: number
  opponent_sp_plus_rank?: number
  pick_count: number
}

export interface AvailableTeamsResponse {
  teams: AvailableTeam[]
  current_pick?: Pick
}

export interface MemberPickStatus {
  membership_id: string
  display_name: string
  has_picked: boolean
  game_id?: string
  team_id?: string
}

// --- Leaderboard (Phase 5) ---

export interface LeaderboardEntry {
  membership_id: string
  display_name: string
  status: 'active' | 'eliminated'
  is_contestant: boolean
  eliminated_week_id?: string
  bought_back: boolean
}

// --- Notifications (Phase 7) ---
//
// Web has no push capability per the plan's stack (Expo Push is
// mobile-only) — only the preferences resource is used here, not
// device-token registration.

export interface NotificationPreferences {
  pick_reminder: boolean
  eliminated: boolean
  survived: boolean
  mass_wipeout: boolean
  buyback: boolean
  weekly_recap: boolean
  email_enabled: boolean
  push_enabled: boolean
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

// The access token lives in memory only (module-level variable backing the
// AuthContext's React state) — never localStorage/sessionStorage, to limit
// the blast radius of an XSS token theft. It's lost on reload; getMe()'s
// 401->refresh->retry path re-establishes it from the refresh cookie.
let accessToken: string | null = null

export function setAccessToken(token: string | null): void {
  accessToken = token
}

export function getAccessToken(): string | null {
  return accessToken
}

type AuthFailureHandler = () => void
let onAuthFailure: AuthFailureHandler | null = null

// Registered by AuthProvider so apiFetch can clear React auth state when a
// 401 survives a refresh attempt (session truly ended).
export function setOnAuthFailure(handler: AuthFailureHandler | null): void {
  onAuthFailure = handler
}

interface RequestOptions {
  method?: string
  body?: unknown
  /** Skip the 401->refresh->retry dance (used by auth endpoints themselves). */
  skipAuthRetry?: boolean
}

async function rawFetch(path: string, opts: RequestOptions = {}): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json'
  if (accessToken) headers['Authorization'] = `Bearer ${accessToken}`

  return fetch(`${API_BASE_URL}${path}`, {
    method: opts.method ?? 'GET',
    headers,
    credentials: 'include',
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  })
}

async function parseJsonOrThrow<T>(res: Response): Promise<T> {
  const text = await res.text()
  const body: unknown = text ? JSON.parse(text) : undefined
  if (!res.ok) {
    const message =
      body && typeof body === 'object' && 'error' in body
        ? String((body as { error: unknown }).error)
        : `request failed with status ${res.status}`
    throw new ApiError(res.status, message)
  }
  return body as T
}

async function tryRefresh(): Promise<boolean> {
  try {
    const res = await rawFetch('/auth/refresh', { method: 'POST', skipAuthRetry: true })
    if (!res.ok) return false
    const body = (await res.json()) as SessionResponse
    accessToken = body.access_token
    return true
  } catch {
    return false
  }
}

/** Authenticated fetch wrapper — see module doc comment. */
export async function apiFetch<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  let res = await rawFetch(path, opts)

  if (res.status === 401 && !opts.skipAuthRetry) {
    const refreshed = await tryRefresh()
    if (refreshed) {
      res = await rawFetch(path, opts)
    } else {
      accessToken = null
      onAuthFailure?.()
    }
  }

  return parseJsonOrThrow<T>(res)
}

export async function fetchHealth(): Promise<HealthResponse> {
  const res = await rawFetch('/health', { skipAuthRetry: true })
  const body = (await res.json()) as HealthResponse
  if (!res.ok && !body.status) {
    throw new Error(`health check failed with status ${res.status}`)
  }
  return body
}

export async function register(input: {
  email: string
  password: string
  display_name: string
}): Promise<SessionResponse> {
  const res = await rawFetch('/auth/register', { method: 'POST', body: input, skipAuthRetry: true })
  return parseJsonOrThrow<SessionResponse>(res)
}

export async function login(input: { email: string; password: string }): Promise<SessionResponse> {
  const res = await rawFetch('/auth/login', { method: 'POST', body: input, skipAuthRetry: true })
  return parseJsonOrThrow<SessionResponse>(res)
}

export async function logout(): Promise<void> {
  await rawFetch('/auth/logout', { method: 'POST', skipAuthRetry: true })
}

export async function getMe(): Promise<User> {
  return apiFetch<User>('/me')
}

// --- Password reset / email verification (post-Phase-10 addition) ---
//
// Sent directly through the API's EmailSender, independent of Phase 7's
// notification_outbox — see api/internal/auth/password_reset.go.

export interface MessageResponse {
  message: string
}

// forgotPassword always resolves with the same message whether or not the
// email matches an account — the backend intentionally never leaks account
// existence via this response (POST /auth/forgot-password always 202).
export async function forgotPassword(email: string): Promise<MessageResponse> {
  const res = await rawFetch('/auth/forgot-password', { method: 'POST', body: { email }, skipAuthRetry: true })
  return parseJsonOrThrow<MessageResponse>(res)
}

export async function resetPassword(input: { token: string; new_password: string }): Promise<MessageResponse> {
  const res = await rawFetch('/auth/reset-password', { method: 'POST', body: input, skipAuthRetry: true })
  return parseJsonOrThrow<MessageResponse>(res)
}

export async function verifyEmail(token: string): Promise<MessageResponse> {
  const res = await rawFetch('/auth/verify-email', { method: 'POST', body: { token }, skipAuthRetry: true })
  return parseJsonOrThrow<MessageResponse>(res)
}

// resendVerification requires auth — unlike forgotPassword, which is
// inherently for logged-out users.
export async function resendVerification(): Promise<MessageResponse> {
  return apiFetch<MessageResponse>('/auth/resend-verification', { method: 'POST' })
}

export async function updateMe(input: { display_name: string }): Promise<User> {
  return apiFetch<User>('/me', { method: 'PATCH', body: input })
}

export async function listConferences(): Promise<string[]> {
  const res = await rawFetch('/conferences', { skipAuthRetry: true })
  return parseJsonOrThrow<string[]>(res)
}

export async function createLeague(input: {
  name: string
  season_year: number
  conference: string
}): Promise<League> {
  return apiFetch<League>('/leagues', { method: 'POST', body: input })
}

export async function listLeagues(): Promise<League[]> {
  return apiFetch<League[]>('/leagues')
}

export async function getLeague(id: string): Promise<League> {
  return apiFetch<League>(`/leagues/${id}`)
}

export async function updateLeague(
  id: string,
  input: { name?: string; commissioner_is_contestant?: boolean },
): Promise<League> {
  return apiFetch<League>(`/leagues/${id}`, { method: 'PATCH', body: input })
}

export async function listMembers(leagueId: string): Promise<Member[]> {
  return apiFetch<Member[]>(`/leagues/${leagueId}/members`)
}

export async function removeMember(leagueId: string, membershipId: string): Promise<void> {
  await apiFetch<void>(`/leagues/${leagueId}/members/${membershipId}`, { method: 'DELETE' })
}

// closeLeague requires the commissioner to have typed the exact phrase
// "I want to close {league name}" — see handleCloseLeague's doc comment on
// the API side for why this is checked server-side too, not just gated by
// the confirmation modal's disabled-until-matching button.
export async function closeLeague(leagueId: string, confirm: string): Promise<League> {
  return apiFetch<League>(`/leagues/${leagueId}`, { method: 'DELETE', body: { confirm } })
}

export async function buyBackMember(leagueId: string, membershipId: string): Promise<Membership> {
  return apiFetch<Membership>(`/leagues/${leagueId}/members/${membershipId}/buyback`, { method: 'POST' })
}

export async function getInviteCode(leagueId: string): Promise<InviteCodeResponse> {
  return apiFetch<InviteCodeResponse>(`/leagues/${leagueId}/invite`)
}

export async function regenerateInviteCode(leagueId: string): Promise<InviteCodeResponse> {
  return apiFetch<InviteCodeResponse>(`/leagues/${leagueId}/invite/regenerate`, { method: 'POST' })
}

export interface InviteSendResult {
  email: string
  sent: boolean
  error?: string
}

// sendInvites emails the league's existing invite code/link to a batch of
// name+email pairs — always resolves (even on partial failure); check each
// entry's `sent` flag rather than a thrown ApiError for per-recipient
// outcomes. See the API's handleSendInvites doc comment for why.
export async function sendInvites(
  leagueId: string,
  invites: { name: string; email: string }[],
): Promise<InviteSendResult[]> {
  return apiFetch<InviteSendResult[]>(`/leagues/${leagueId}/invite/send`, { method: 'POST', body: { invites } })
}

export async function previewInvite(code: string): Promise<InvitePreviewResponse> {
  const res = await rawFetch(`/invites/${encodeURIComponent(code)}`, { skipAuthRetry: true })
  return parseJsonOrThrow<InvitePreviewResponse>(res)
}

export async function joinLeagueByCode(code: string): Promise<League> {
  return apiFetch<League>(`/invites/${encodeURIComponent(code)}/join`, { method: 'POST' })
}

// --- Schedule / Picks (Phase 4) ---

// conference is optional: pass a league's conference so weeks that are a
// no-op for it (e.g. a standalone late-season game in another conference,
// which still occupies its own global week row) never show up as
// selectable-but-empty. Admin tooling omits it deliberately to see every
// week regardless of conference.
export async function listWeeks(seasonYear: number, conference?: string): Promise<Week[]> {
  const query = conference
    ? `season_year=${seasonYear}&conference=${encodeURIComponent(conference)}`
    : `season_year=${seasonYear}`
  return apiFetch<Week[]>(`/weeks?${query}`)
}

// getCurrentWeek backs the picks screen's default week selection: the
// week that is "currently occurring" schedule-wise for the league's
// conference (see the API's own doc comment for the exact rule). 404s if
// no schedule data has synced yet for this league's conference/season.
export async function getCurrentWeek(leagueId: string): Promise<Week> {
  return apiFetch<Week>(`/leagues/${leagueId}/current-week`)
}

// listWeekGames backs the admin "resync a game" picker (Phase 8) — lets an
// admin browse a week's games (with status) rather than needing to already
// know a game's internal UUID.
export async function listWeekGames(weekId: string): Promise<Game[]> {
  return apiFetch<Game[]>(`/weeks/${weekId}/games`)
}

// Returns null (rather than throwing) on a 404 — "no pick for this week
// yet" is an expected, common state, not an error condition for callers.
export async function getMyPick(leagueId: string, weekId: string): Promise<Pick | null> {
  try {
    return await apiFetch<Pick>(`/leagues/${leagueId}/weeks/${weekId}/picks/me`)
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null
    throw err
  }
}

export async function upsertMyPick(
  leagueId: string,
  weekId: string,
  input: { game_id: string; team_id: string },
): Promise<Pick> {
  return apiFetch<Pick>(`/leagues/${leagueId}/weeks/${weekId}/picks/me`, { method: 'PUT', body: input })
}

export async function getAvailableTeams(leagueId: string, weekId: string): Promise<AvailableTeamsResponse> {
  return apiFetch<AvailableTeamsResponse>(`/leagues/${leagueId}/weeks/${weekId}/available-teams`)
}

export async function listWeekPicks(leagueId: string, weekId: string): Promise<MemberPickStatus[]> {
  return apiFetch<MemberPickStatus[]>(`/leagues/${leagueId}/weeks/${weekId}/picks`)
}

export async function getLeaderboard(leagueId: string): Promise<LeaderboardEntry[]> {
  return apiFetch<LeaderboardEntry[]>(`/leagues/${leagueId}/leaderboard`)
}

export interface WeekRecap {
  body: string
  generated_at: string
}

// Throws ApiError with status 404 if no week has finalized yet for this
// league — callers should treat that as "nothing to show", not an error.
export async function getLatestRecap(leagueId: string): Promise<WeekRecap> {
  return apiFetch<WeekRecap>(`/leagues/${leagueId}/recap`)
}

export interface MembershipWeekPick {
  week_number: number
  has_picked: boolean
  is_locked: boolean
  game_id?: string
  team_id?: string
  team_name?: string
  team_logo_url?: string
  opponent_name?: string
  opponent_logo_url?: string
  is_home?: boolean
  kickoff_at?: string
  result?: 'pending' | 'win' | 'loss' | 'void'
}

export async function listMembershipPicks(leagueId: string, membershipId: string): Promise<MembershipWeekPick[]> {
  return apiFetch<MembershipWeekPick[]>(`/leagues/${leagueId}/members/${membershipId}/picks`)
}

// --- Notifications (Phase 7) ---

export async function getNotificationPreferences(): Promise<NotificationPreferences> {
  return apiFetch<NotificationPreferences>('/me/notification-preferences')
}

export async function updateNotificationPreferences(
  prefs: NotificationPreferences,
): Promise<NotificationPreferences> {
  return apiFetch<NotificationPreferences>('/me/notification-preferences', { method: 'PUT', body: prefs })
}

// --- Admin (Phase 8, plus Phase 3's sync-runs endpoints) ---
//
// Client-side gating on user.is_site_admin (see SiteAdminRoute) is UX
// only — every one of these hits a requireSiteAdmin route, so the real
// enforcement is server-side regardless of what the UI shows.

export async function triggerScheduleSync(seasonYear: number): Promise<SyncRun> {
  return apiFetch<SyncRun>('/admin/sync/schedule', { method: 'POST', body: { season_year: seasonYear } })
}

export async function listSyncRuns(): Promise<SyncRun[]> {
  return apiFetch<SyncRun[]>('/admin/sync/runs')
}

export async function listAdminLeagues(limit: number, offset: number): Promise<AdminLeaguesListResponse> {
  return apiFetch<AdminLeaguesListResponse>(`/admin/leagues?limit=${limit}&offset=${offset}`)
}

export async function listAdminUsers(limit: number, offset: number): Promise<AdminUsersListResponse> {
  return apiFetch<AdminUsersListResponse>(`/admin/users?limit=${limit}&offset=${offset}`)
}

export async function disableUser(userId: string): Promise<AdminUser> {
  return apiFetch<AdminUser>(`/admin/users/${userId}/disable`, { method: 'POST' })
}

export async function enableUser(userId: string): Promise<AdminUser> {
  return apiFetch<AdminUser>(`/admin/users/${userId}/enable`, { method: 'POST' })
}

export async function resyncGame(gameId: string): Promise<ResyncGameResponse> {
  return apiFetch<ResyncGameResponse>(`/admin/games/${gameId}/resync`, { method: 'POST' })
}

export async function listAuditLog(
  limit: number,
  offset: number,
  filters: { action?: string; actor_user_id?: string } = {},
): Promise<AuditLogListResponse> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (filters.action) params.set('action', filters.action)
  if (filters.actor_user_id) params.set('actor_user_id', filters.actor_user_id)
  return apiFetch<AuditLogListResponse>(`/admin/audit-log?${params.toString()}`)
}
