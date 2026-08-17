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
  status: 'active'
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
  joined_at: string
}

export interface InviteCodeResponse {
  invite_code: string
}

export interface InvitePreviewResponse {
  league_name: string
  conference: string
  season_year: number
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
  is_locked: boolean
  is_used_elsewhere: boolean
  is_current_pick: boolean
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

export async function getInviteCode(leagueId: string): Promise<InviteCodeResponse> {
  return apiFetch<InviteCodeResponse>(`/leagues/${leagueId}/invite`)
}

export async function regenerateInviteCode(leagueId: string): Promise<InviteCodeResponse> {
  return apiFetch<InviteCodeResponse>(`/leagues/${leagueId}/invite/regenerate`, { method: 'POST' })
}

export async function previewInvite(code: string): Promise<InvitePreviewResponse> {
  const res = await rawFetch(`/invites/${encodeURIComponent(code)}`, { skipAuthRetry: true })
  return parseJsonOrThrow<InvitePreviewResponse>(res)
}

export async function joinLeagueByCode(code: string): Promise<League> {
  return apiFetch<League>(`/invites/${encodeURIComponent(code)}/join`, { method: 'POST' })
}

// --- Schedule / Picks (Phase 4) ---

export async function listWeeks(seasonYear: number): Promise<Week[]> {
  return apiFetch<Week[]>(`/weeks?season_year=${seasonYear}`)
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
