// Thin fetch wrapper around the Go API for the mobile client.
//
// Unlike the web client, there is no cookie jar here: both the access
// token and the refresh token are read from / written to expo-secure-store
// (see auth/AuthContext.tsx), and every refresh call sends refresh_token
// explicitly in the JSON body per the API contract.
//
// EXPO_PUBLIC_-prefixed env vars are inlined by Expo at build/start time —
// see https://docs.expo.dev/guides/environment-variables/. Set
// EXPO_PUBLIC_API_BASE_URL in a .env file (see .env.example) to point at a
// non-default API host (e.g. a physical device on the same network).
export const API_BASE_URL: string =
  process.env.EXPO_PUBLIC_API_BASE_URL ?? 'http://localhost:8080';

export interface HealthResponse {
  status: 'ok' | 'error';
  db: 'ok' | 'error';
  error?: string;
}

export interface User {
  id: string;
  email: string;
  display_name: string;
  is_site_admin: boolean;
  // null until POST /auth/verify-email succeeds for this user
  // (post-Phase-10 addition) — see the forgot/reset/verify functions below.
  email_verified_at: string | null;
}

// --- Admin (Phase 8) ---

export interface AdminCommissioner {
  id: string;
  display_name: string;
  email: string;
}

export interface AdminLeague {
  id: string;
  name: string;
  conference: string;
  season_year: number;
  status: 'active';
  commissioner: AdminCommissioner;
  member_count: number;
  created_at: string;
}

export interface AdminLeaguesListResponse {
  leagues: AdminLeague[];
  total: number;
  limit: number;
  offset: number;
}

export interface AdminUser {
  id: string;
  email: string;
  display_name: string;
  is_site_admin: boolean;
  status: 'active' | 'disabled';
  league_count: number;
  created_at: string;
}

export interface AdminUsersListResponse {
  users: AdminUser[];
  total: number;
  limit: number;
  offset: number;
}

export interface FinalizedLeagueWeek {
  league_id: string;
  week_id: string;
  mass_wipeout: boolean;
}

export interface ResyncGameResponse {
  game: Game;
  finalized_league_weeks: FinalizedLeagueWeek[];
}

export interface AuditLogEntry {
  id: string;
  actor_user_id?: string;
  league_id?: string;
  action: string;
  target_type?: string;
  target_id?: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface AuditLogListResponse {
  entries: AuditLogEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface SyncRun {
  id: string;
  kind: 'schedule';
  status: 'running' | 'success' | 'failed';
  started_at: string;
  finished_at?: string;
  error?: string;
  details: Record<string, unknown>;
  created_at: string;
}

export interface SessionResponse {
  access_token: string;
  refresh_token?: string;
  user: User;
}

export interface MembershipSummary {
  id: string;
  role: 'commissioner' | 'player';
  is_contestant: boolean;
  status: 'active' | 'eliminated';
}

export interface League {
  id: string;
  name: string;
  season_year: number;
  conference: string;
  commissioner_user_id: string;
  status: 'active' | 'closed';
  created_at: string;
  membership: MembershipSummary;
}

export interface Member {
  membership_id: string;
  user_id: string;
  display_name: string;
  role: 'commissioner' | 'player';
  is_contestant: boolean;
  status: 'active' | 'eliminated';
  bought_back: boolean;
  joined_at: string;
}

// Membership is the full record returned by the buy-back endpoint —
// richer than Member/MembershipSummary since it surfaces the
// eliminated_*/bought_back_* fields a buy-back response needs to show.
export interface Membership {
  membership_id: string;
  league_id: string;
  user_id: string;
  role: 'commissioner' | 'player';
  is_contestant: boolean;
  status: 'active' | 'eliminated';
  eliminated_week_id?: string;
  eliminated_game_id?: string;
  bought_back: boolean;
  bought_back_at?: string;
  bought_back_by?: string;
}

export interface InviteCodeResponse {
  invite_code: string;
  // False once the league is closed, or once its conference's week 1 has
  // no pickable games left — the commissioner's own invite code/invite-by-
  // email UI hides once this flips, since no one can actually join anymore.
  joinable: boolean;
}

export interface InvitePreviewResponse {
  league_name: string;
  conference: string;
  season_year: number;
  // False once the league is closed, or once its conference's week 1 has
  // no pickable games left (games have started — no new members mid-season).
  joinable: boolean;
}

export interface Week {
  id: string;
  season_year: number;
  week_number: number;
}

export interface GameTeam {
  id: string;
  name: string;
  conference: string;
  logo_url?: string;
}

export interface Game {
  id: string;
  external_id: string;
  week_id: string;
  kickoff_at: string;
  status: 'scheduled' | 'in_progress' | 'final' | 'postponed' | 'canceled';
  home_team: GameTeam;
  away_team: GameTeam;
  home_score?: number;
  away_score?: number;
  winner_team_id?: string;
}

export interface Pick {
  game_id: string;
  team_id: string;
  locked: boolean;
}

export interface AvailableTeam {
  team_id: string;
  team_name: string;
  team_logo_url?: string;
  opponent_team_id: string;
  opponent_name: string;
  opponent_logo_url?: string;
  game_id: string;
  kickoff_at: string;
  is_home: boolean;
  is_locked: boolean;
  is_used_elsewhere: boolean;
  is_current_pick: boolean;
}

export interface AvailableTeamsResponse {
  teams: AvailableTeam[];
  current_pick?: Pick;
}

export interface MemberPickStatus {
  membership_id: string;
  display_name: string;
  has_picked: boolean;
  game_id?: string;
  team_id?: string;
}

// --- Leaderboard (Phase 5) ---

export interface LeaderboardEntry {
  membership_id: string;
  display_name: string;
  status: 'active' | 'eliminated';
  is_contestant: boolean;
  eliminated_week_id?: string;
  bought_back: boolean;
}

// --- Notifications (Phase 7) ---

export interface DeviceToken {
  id: string;
  platform: 'ios' | 'android';
  created_at: string;
}

export interface NotificationPreferences {
  pick_reminder: boolean;
  eliminated: boolean;
  survived: boolean;
  mass_wipeout: boolean;
  buyback: boolean;
  email_enabled: boolean;
  push_enabled: boolean;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  accessToken?: string | null;
}

async function rawFetch(path: string, opts: RequestOptions = {}): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  if (opts.accessToken) headers['Authorization'] = `Bearer ${opts.accessToken}`;

  return fetch(`${API_BASE_URL}${path}`, {
    method: opts.method ?? 'GET',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
}

async function parseJsonOrThrow<T>(res: Response): Promise<T> {
  const text = await res.text();
  const body: unknown = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    const message =
      body && typeof body === 'object' && 'error' in body
        ? String((body as { error: unknown }).error)
        : `request failed with status ${res.status}`;
    throw new ApiError(res.status, message);
  }
  return body as T;
}

export async function fetchHealth(): Promise<HealthResponse> {
  const res = await rawFetch('/health');
  const body = (await res.json()) as HealthResponse;
  if (!res.ok && !body.status) {
    throw new Error(`health check failed with status ${res.status}`);
  }
  return body;
}

export async function register(input: {
  email: string;
  password: string;
  display_name: string;
}): Promise<SessionResponse> {
  const res = await rawFetch('/auth/register', { method: 'POST', body: input });
  return parseJsonOrThrow<SessionResponse>(res);
}

export async function login(input: { email: string; password: string }): Promise<SessionResponse> {
  const res = await rawFetch('/auth/login', { method: 'POST', body: input });
  return parseJsonOrThrow<SessionResponse>(res);
}

export async function refresh(refreshToken: string): Promise<SessionResponse> {
  const res = await rawFetch('/auth/refresh', {
    method: 'POST',
    body: { refresh_token: refreshToken },
  });
  return parseJsonOrThrow<SessionResponse>(res);
}

export async function logout(refreshToken: string | null): Promise<void> {
  await rawFetch('/auth/logout', {
    method: 'POST',
    body: { refresh_token: refreshToken ?? '' },
  });
}

export async function getMe(accessToken: string): Promise<User> {
  const res = await rawFetch('/me', { accessToken });
  return parseJsonOrThrow<User>(res);
}

// --- Password reset / email verification (post-Phase-10 addition) ---
//
// Sent directly through the API's EmailSender, independent of Phase 7's
// notification_outbox — see api/internal/auth/password_reset.go. Mobile
// has no deep-linking set up yet, so the reset/verify screens ask the
// user to paste the token manually (see ResetPasswordScreen/
// VerifyEmailBanner) rather than opening a link.

export interface MessageResponse {
  message: string;
}

// forgotPassword always resolves with the same message whether or not the
// email matches an account — the backend intentionally never leaks
// account existence via this response (POST /auth/forgot-password always
// 202).
export async function forgotPassword(email: string): Promise<MessageResponse> {
  const res = await rawFetch('/auth/forgot-password', { method: 'POST', body: { email } });
  return parseJsonOrThrow<MessageResponse>(res);
}

export async function resetPassword(input: { token: string; new_password: string }): Promise<MessageResponse> {
  const res = await rawFetch('/auth/reset-password', { method: 'POST', body: input });
  return parseJsonOrThrow<MessageResponse>(res);
}

export async function verifyEmail(token: string): Promise<MessageResponse> {
  const res = await rawFetch('/auth/verify-email', { method: 'POST', body: { token } });
  return parseJsonOrThrow<MessageResponse>(res);
}

// resendVerification requires auth — unlike forgotPassword, which is
// inherently for logged-out users.
export async function resendVerification(accessToken: string): Promise<MessageResponse> {
  const res = await rawFetch('/auth/resend-verification', { method: 'POST', accessToken });
  return parseJsonOrThrow<MessageResponse>(res);
}

export async function updateMe(accessToken: string, input: { display_name: string }): Promise<User> {
  const res = await rawFetch('/me', { method: 'PATCH', body: input, accessToken });
  return parseJsonOrThrow<User>(res);
}

export async function listConferences(): Promise<string[]> {
  const res = await rawFetch('/conferences');
  return parseJsonOrThrow<string[]>(res);
}

export async function createLeague(
  accessToken: string,
  input: { name: string; season_year: number; conference: string },
): Promise<League> {
  const res = await rawFetch('/leagues', { method: 'POST', body: input, accessToken });
  return parseJsonOrThrow<League>(res);
}

export async function listLeagues(accessToken: string): Promise<League[]> {
  const res = await rawFetch('/leagues', { accessToken });
  return parseJsonOrThrow<League[]>(res);
}

export async function getLeague(accessToken: string, id: string): Promise<League> {
  const res = await rawFetch(`/leagues/${id}`, { accessToken });
  return parseJsonOrThrow<League>(res);
}

export async function updateLeague(
  accessToken: string,
  id: string,
  input: { name?: string; commissioner_is_contestant?: boolean },
): Promise<League> {
  const res = await rawFetch(`/leagues/${id}`, { method: 'PATCH', body: input, accessToken });
  return parseJsonOrThrow<League>(res);
}

export async function listMembers(accessToken: string, leagueId: string): Promise<Member[]> {
  const res = await rawFetch(`/leagues/${leagueId}/members`, { accessToken });
  return parseJsonOrThrow<Member[]>(res);
}

export async function removeMember(accessToken: string, leagueId: string, membershipId: string): Promise<void> {
  const res = await rawFetch(`/leagues/${leagueId}/members/${membershipId}`, { method: 'DELETE', accessToken });
  await parseJsonOrThrow<void>(res);
}

// closeLeague requires the commissioner to have typed the exact phrase
// "I want to close {league name}" — see the API's handleCloseLeague doc
// comment for why this is checked server-side too, not just gated by the
// confirmation modal's disabled-until-matching button.
export async function closeLeague(accessToken: string, leagueId: string, confirm: string): Promise<League> {
  const res = await rawFetch(`/leagues/${leagueId}`, { method: 'DELETE', accessToken, body: { confirm } });
  return parseJsonOrThrow<League>(res);
}

export async function buyBackMember(accessToken: string, leagueId: string, membershipId: string): Promise<Membership> {
  const res = await rawFetch(`/leagues/${leagueId}/members/${membershipId}/buyback`, { method: 'POST', accessToken });
  return parseJsonOrThrow<Membership>(res);
}

export async function getInviteCode(accessToken: string, leagueId: string): Promise<InviteCodeResponse> {
  const res = await rawFetch(`/leagues/${leagueId}/invite`, { accessToken });
  return parseJsonOrThrow<InviteCodeResponse>(res);
}

export interface InviteSendResult {
  email: string;
  sent: boolean;
  error?: string;
}

// sendInvites emails the league's existing invite code/link to a batch of
// name+email pairs — always resolves (even on partial failure); check each
// entry's `sent` flag rather than a thrown ApiError for per-recipient
// outcomes. See the API's handleSendInvites doc comment for why.
export async function sendInvites(
  accessToken: string,
  leagueId: string,
  invites: { name: string; email: string }[],
): Promise<InviteSendResult[]> {
  const res = await rawFetch(`/leagues/${leagueId}/invite/send`, {
    method: 'POST',
    accessToken,
    body: { invites },
  });
  return parseJsonOrThrow<InviteSendResult[]>(res);
}

export async function regenerateInviteCode(accessToken: string, leagueId: string): Promise<InviteCodeResponse> {
  const res = await rawFetch(`/leagues/${leagueId}/invite/regenerate`, { method: 'POST', accessToken });
  return parseJsonOrThrow<InviteCodeResponse>(res);
}

export async function previewInvite(code: string): Promise<InvitePreviewResponse> {
  const res = await rawFetch(`/invites/${encodeURIComponent(code)}`);
  return parseJsonOrThrow<InvitePreviewResponse>(res);
}

export async function joinLeagueByCode(accessToken: string, code: string): Promise<League> {
  const res = await rawFetch(`/invites/${encodeURIComponent(code)}/join`, { method: 'POST', accessToken });
  return parseJsonOrThrow<League>(res);
}

// --- Schedule / Picks (Phase 4) ---

export async function listWeeks(accessToken: string, seasonYear: number): Promise<Week[]> {
  const res = await rawFetch(`/weeks?season_year=${seasonYear}`, { accessToken });
  return parseJsonOrThrow<Week[]>(res);
}

// getCurrentWeek backs the picks screen's default week selection: the
// week that is "currently occurring" schedule-wise for the league's
// conference. 404s if no schedule data has synced yet.
export async function getCurrentWeek(accessToken: string, leagueId: string): Promise<Week> {
  const res = await rawFetch(`/leagues/${leagueId}/current-week`, { accessToken });
  return parseJsonOrThrow<Week>(res);
}

// Returns null (rather than throwing) on a 404 — "no pick for this week
// yet" is an expected, common state, not an error condition for callers.
export async function getMyPick(accessToken: string, leagueId: string, weekId: string): Promise<Pick | null> {
  const res = await rawFetch(`/leagues/${leagueId}/weeks/${weekId}/picks/me`, { accessToken });
  if (res.status === 404) return null;
  return parseJsonOrThrow<Pick>(res);
}

export async function upsertMyPick(
  accessToken: string,
  leagueId: string,
  weekId: string,
  input: { game_id: string; team_id: string },
): Promise<Pick> {
  const res = await rawFetch(`/leagues/${leagueId}/weeks/${weekId}/picks/me`, {
    method: 'PUT',
    body: input,
    accessToken,
  });
  return parseJsonOrThrow<Pick>(res);
}

export async function getAvailableTeams(
  accessToken: string,
  leagueId: string,
  weekId: string,
): Promise<AvailableTeamsResponse> {
  const res = await rawFetch(`/leagues/${leagueId}/weeks/${weekId}/available-teams`, { accessToken });
  return parseJsonOrThrow<AvailableTeamsResponse>(res);
}

export async function listWeekPicks(
  accessToken: string,
  leagueId: string,
  weekId: string,
): Promise<MemberPickStatus[]> {
  const res = await rawFetch(`/leagues/${leagueId}/weeks/${weekId}/picks`, { accessToken });
  return parseJsonOrThrow<MemberPickStatus[]>(res);
}

export async function getLeaderboard(accessToken: string, leagueId: string): Promise<LeaderboardEntry[]> {
  const res = await rawFetch(`/leagues/${leagueId}/leaderboard`, { accessToken });
  return parseJsonOrThrow<LeaderboardEntry[]>(res);
}

export interface MembershipWeekPick {
  week_number: number;
  has_picked: boolean;
  is_locked: boolean;
  game_id?: string;
  team_id?: string;
  team_name?: string;
  team_logo_url?: string;
  opponent_name?: string;
  opponent_logo_url?: string;
  is_home?: boolean;
  kickoff_at?: string;
  result?: 'pending' | 'win' | 'loss' | 'void';
}

export async function listMembershipPicks(
  accessToken: string,
  leagueId: string,
  membershipId: string,
): Promise<MembershipWeekPick[]> {
  const res = await rawFetch(`/leagues/${leagueId}/members/${membershipId}/picks`, { accessToken });
  return parseJsonOrThrow<MembershipWeekPick[]>(res);
}

// --- Notifications (Phase 7) ---

export async function registerDeviceToken(
  accessToken: string,
  input: { platform: 'ios' | 'android'; expo_push_token: string },
): Promise<DeviceToken> {
  const res = await rawFetch('/me/device-tokens', { method: 'POST', body: input, accessToken });
  return parseJsonOrThrow<DeviceToken>(res);
}

export async function deleteDeviceToken(accessToken: string, expoPushToken: string): Promise<void> {
  const res = await rawFetch('/me/device-tokens', {
    method: 'DELETE',
    body: { expo_push_token: expoPushToken },
    accessToken,
  });
  await parseJsonOrThrow<void>(res);
}

export async function getNotificationPreferences(accessToken: string): Promise<NotificationPreferences> {
  const res = await rawFetch('/me/notification-preferences', { accessToken });
  return parseJsonOrThrow<NotificationPreferences>(res);
}

export async function updateNotificationPreferences(
  accessToken: string,
  prefs: NotificationPreferences,
): Promise<NotificationPreferences> {
  const res = await rawFetch('/me/notification-preferences', { method: 'PUT', body: prefs, accessToken });
  return parseJsonOrThrow<NotificationPreferences>(res);
}

// --- Admin (Phase 8, plus Phase 3's sync-runs endpoints) ---

export async function triggerScheduleSync(accessToken: string, seasonYear: number): Promise<SyncRun> {
  const res = await rawFetch('/admin/sync/schedule', { method: 'POST', body: { season_year: seasonYear }, accessToken });
  return parseJsonOrThrow<SyncRun>(res);
}

export async function listSyncRuns(accessToken: string): Promise<SyncRun[]> {
  const res = await rawFetch('/admin/sync/runs', { accessToken });
  return parseJsonOrThrow<SyncRun[]>(res);
}

export async function listAdminLeagues(accessToken: string, limit: number, offset: number): Promise<AdminLeaguesListResponse> {
  const res = await rawFetch(`/admin/leagues?limit=${limit}&offset=${offset}`, { accessToken });
  return parseJsonOrThrow<AdminLeaguesListResponse>(res);
}

export async function listAdminUsers(accessToken: string, limit: number, offset: number): Promise<AdminUsersListResponse> {
  const res = await rawFetch(`/admin/users?limit=${limit}&offset=${offset}`, { accessToken });
  return parseJsonOrThrow<AdminUsersListResponse>(res);
}

export async function disableUser(accessToken: string, userId: string): Promise<AdminUser> {
  const res = await rawFetch(`/admin/users/${userId}/disable`, { method: 'POST', accessToken });
  return parseJsonOrThrow<AdminUser>(res);
}

export async function enableUser(accessToken: string, userId: string): Promise<AdminUser> {
  const res = await rawFetch(`/admin/users/${userId}/enable`, { method: 'POST', accessToken });
  return parseJsonOrThrow<AdminUser>(res);
}

export async function resyncGame(accessToken: string, gameId: string): Promise<ResyncGameResponse> {
  const res = await rawFetch(`/admin/games/${gameId}/resync`, { method: 'POST', accessToken });
  return parseJsonOrThrow<ResyncGameResponse>(res);
}

export async function listAuditLog(
  accessToken: string,
  limit: number,
  offset: number,
  filters: { action?: string; actor_user_id?: string } = {},
): Promise<AuditLogListResponse> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (filters.action) params.set('action', filters.action);
  if (filters.actor_user_id) params.set('actor_user_id', filters.actor_user_id);
  const res = await rawFetch(`/admin/audit-log?${params.toString()}`, { accessToken });
  return parseJsonOrThrow<AuditLogListResponse>(res);
}
