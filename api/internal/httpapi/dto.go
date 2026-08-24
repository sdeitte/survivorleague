package httpapi

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type updateMeRequest struct {
	DisplayName string `json:"display_name"`
}

type userResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsSiteAdmin bool   `json:"is_site_admin"`
	// EmailVerifiedAt is null until POST /auth/verify-email succeeds for
	// this user. Always present (never omitted) so a client can reliably
	// branch on `email_verified_at === null` to show the verify-email
	// banner — see the post-Phase-10 password-reset/email-verification
	// addition.
	EmailVerifiedAt *string `json:"email_verified_at"`
}

// --- Password reset / email verification (post-Phase-10 addition) ---
//
// Sent directly via internal/notify's EmailSender (internal/auth/
// password_reset.go), independent of Phase 7's notification_outbox — see
// that file's doc comment.

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// messageResponse backs every one of this addition's success responses
// (forgot-password, reset-password, verify-email, resend-verification) —
// forgot-password/reset-password/verify-email deliberately share this
// exact shape (not per-endpoint schemas) so, per the API contract,
// forgot-password's found/not-found cases are byte-for-byte
// indistinguishable in the response body.
type messageResponse struct {
	Message string `json:"message"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

// sessionResponse covers all three token-issuing endpoints. RefreshToken
// is populated on all of them: web clients ignore it (they rely on the
// httpOnly cookie, which is also always set), while mobile clients — which
// have no cookie jar — read it from here and store it via
// expo-secure-store. Deliberate deviation from a literal reading of the
// Phase 1 spec (which only documented refresh_token in the /auth/refresh
// body): without it, mobile would have no way to obtain a refresh token at
// all on initial register/login, since it can't read Set-Cookie.
type sessionResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token,omitempty"`
	User         userResponse `json:"user"`
}

func toUserResponse(u gen.User) userResponse {
	resp := userResponse{
		ID:          db.UUIDString(u.ID),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		IsSiteAdmin: u.IsSiteAdmin,
	}
	if u.EmailVerifiedAt.Valid {
		ts := formatTimestamp(u.EmailVerifiedAt)
		resp.EmailVerifiedAt = &ts
	}
	return resp
}

func formatTimestamp(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

// --- Leagues ---

type createLeagueRequest struct {
	Name       string `json:"name"`
	SeasonYear int32  `json:"season_year"`
	Conference string `json:"conference"`
}

// updateLeagueRequest covers the two fields PATCH /leagues/:id may change.
// conference/season_year are deliberately absent here — the handler
// decodes the body separately as a raw map first specifically to detect
// (and reject with 400) any attempt to set those immutable fields, before
// ever reaching this struct.
type updateLeagueRequest struct {
	Name                     *string `json:"name"`
	CommissionerIsContestant *bool   `json:"commissioner_is_contestant"`
}

// membershipSummary is the requester's own role/status within a league, as
// embedded in leagueResponse.
type membershipSummary struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	IsContestant bool   `json:"is_contestant"`
	Status       string `json:"status"`
}

// leagueResponse is returned by every endpoint that hands back a league
// together with the requester's membership in it (create, list, get,
// update, join). It deliberately omits invite_code — that's only exposed
// via the dedicated GET/POST /leagues/:id/invite... endpoints.
type leagueResponse struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	SeasonYear         int32             `json:"season_year"`
	Conference         string            `json:"conference"`
	CommissionerUserID string            `json:"commissioner_user_id"`
	Status             string            `json:"status"`
	CreatedAt          string            `json:"created_at"`
	Membership         membershipSummary `json:"membership"`
}

func toLeagueResponse(league gen.League, membershipID pgtype.UUID, role string, isContestant bool, status string) leagueResponse {
	return leagueResponse{
		ID:                 db.UUIDString(league.ID),
		Name:               league.Name,
		SeasonYear:         league.SeasonYear,
		Conference:         league.Conference,
		CommissionerUserID: db.UUIDString(league.CommissionerUserID),
		Status:             league.Status,
		CreatedAt:          formatTimestamp(league.CreatedAt),
		Membership: membershipSummary{
			ID:           db.UUIDString(membershipID),
			Role:         role,
			IsContestant: isContestant,
			Status:       status,
		},
	}
}

type memberResponse struct {
	MembershipID string `json:"membership_id"`
	UserID       string `json:"user_id"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	IsContestant bool   `json:"is_contestant"`
	Status       string `json:"status"`
	BoughtBack   bool   `json:"bought_back"`
	JoinedAt     string `json:"joined_at"`
}

// membershipResponse is the full membership record returned by
// POST .../members/:membershipId/buyback (Phase 6) — richer than
// membershipSummary since a client acting on a buy-back needs to see the
// bought_back/bought_back_at/eliminated_* fields it just changed (or, for
// eliminated_week_id/eliminated_game_id, deliberately did NOT change).
type membershipResponse struct {
	MembershipID     string `json:"membership_id"`
	LeagueID         string `json:"league_id"`
	UserID           string `json:"user_id"`
	Role             string `json:"role"`
	IsContestant     bool   `json:"is_contestant"`
	Status           string `json:"status"`
	EliminatedWeekID string `json:"eliminated_week_id,omitempty"`
	EliminatedGameID string `json:"eliminated_game_id,omitempty"`
	BoughtBack       bool   `json:"bought_back"`
	BoughtBackAt     string `json:"bought_back_at,omitempty"`
	BoughtBackBy     string `json:"bought_back_by,omitempty"`
}

func toMembershipResponse(m gen.LeagueMembership) membershipResponse {
	resp := membershipResponse{
		MembershipID:     db.UUIDString(m.ID),
		LeagueID:         db.UUIDString(m.LeagueID),
		UserID:           db.UUIDString(m.UserID),
		Role:             m.Role,
		IsContestant:     m.IsContestant,
		Status:           m.Status,
		EliminatedWeekID: pgUUIDStringOrEmpty(m.EliminatedWeekID),
		EliminatedGameID: pgUUIDStringOrEmpty(m.EliminatedGameID),
		BoughtBack:       m.BoughtBack,
		BoughtBackBy:     pgUUIDStringOrEmpty(m.BoughtBackBy),
	}
	if m.BoughtBackAt.Valid {
		resp.BoughtBackAt = formatTimestamp(m.BoughtBackAt)
	}
	return resp
}

// leaderboardEntryResponse is one row of GET /leagues/:id/leaderboard.
// bought_back is always false for now — Phase 6 hasn't been built yet —
// but the field is included from day one for forward compatibility.
type leaderboardEntryResponse struct {
	MembershipID     string `json:"membership_id"`
	DisplayName      string `json:"display_name"`
	Role             string `json:"role"`
	Status           string `json:"status"`
	IsContestant     bool   `json:"is_contestant"`
	EliminatedWeekID string `json:"eliminated_week_id,omitempty"`
	BoughtBack       bool   `json:"bought_back"`
}

func toLeaderboardEntryResponse(row gen.ListLeaderboardForLeagueRow) leaderboardEntryResponse {
	return leaderboardEntryResponse{
		MembershipID:     db.UUIDString(row.MembershipID),
		DisplayName:      row.DisplayName,
		Role:             row.Role,
		Status:           row.Status,
		IsContestant:     row.IsContestant,
		EliminatedWeekID: pgUUIDStringOrEmpty(row.EliminatedWeekID),
		BoughtBack:       row.BoughtBack,
	}
}

// weekRecapResponse is GET /leagues/:id/recap's body — the most recently
// generated AI weekly recap (see internal/recap.Service). Plain text
// (body), not markdown/HTML — see recap.buildPrompt's instruction to the
// model.
type weekRecapResponse struct {
	Body        string `json:"body"`
	GeneratedAt string `json:"generated_at"`
}

type inviteCodeResponse struct {
	InviteCode string `json:"invite_code"`
	// Joinable mirrors invitePreviewResponse's field of the same name — see
	// API.isLeagueJoinable's doc comment. Lets the commissioner's own league
	// page hide the invite code/invite-by-email UI once new members can no
	// longer actually join, instead of only failing at send/regenerate time.
	Joinable bool `json:"joinable"`
}

// inviteSendResultResponse is one entry of POST /leagues/:id/invite/send's
// per-recipient response array — see handleSendInvites' doc comment for
// why this is best-effort-per-recipient rather than all-or-nothing.
type inviteSendResultResponse struct {
	Email string `json:"email"`
	Sent  bool   `json:"sent"`
	Error string `json:"error,omitempty"`
}

type invitePreviewResponse struct {
	LeagueName string `json:"league_name"`
	Conference string `json:"conference"`
	SeasonYear int32  `json:"season_year"`
	// Joinable is false once the league is closed, or once its conference's
	// week 1 has no pickable games left — see API.isLeagueJoinable's doc
	// comment. Lets the join UI show why before the user even tries.
	Joinable bool `json:"joinable"`
}

// --- Schedule (Phase 3) ---

type teamResponse struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Conference string `json:"conference"`
	LogoURL    string `json:"logo_url,omitempty"`
}

func toTeamResponse(t gen.Team) teamResponse {
	return teamResponse{
		ID:         db.UUIDString(t.ID),
		ExternalID: t.ExternalID,
		Name:       t.Name,
		Conference: t.Conference,
		LogoURL:    t.LogoUrl.String,
	}
}

type weekResponse struct {
	ID         string `json:"id"`
	SeasonYear int32  `json:"season_year"`
	WeekNumber int32  `json:"week_number"`
}

func toWeekResponse(w gen.Week) weekResponse {
	return weekResponse{
		ID:         db.UUIDString(w.ID),
		SeasonYear: w.SeasonYear,
		WeekNumber: w.WeekNumber,
	}
}

// gameTeamResponse is a game's home/away team, trimmed to what a picks
// screen needs — embedded directly in gameResponse so clients never have to
// make a second request to resolve team names/conference/logo per the
// GET /weeks/:id/games contract ("include team names/conference so clients
// don't need N+1 lookups").
type gameTeamResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Conference string `json:"conference"`
	LogoURL    string `json:"logo_url,omitempty"`
}

type gameResponse struct {
	ID           string           `json:"id"`
	ExternalID   string           `json:"external_id"`
	WeekID       string           `json:"week_id"`
	KickoffAt    string           `json:"kickoff_at"`
	Status       string           `json:"status"`
	HomeTeam     gameTeamResponse `json:"home_team"`
	AwayTeam     gameTeamResponse `json:"away_team"`
	HomeScore    *int32           `json:"home_score,omitempty"`
	AwayScore    *int32           `json:"away_score,omitempty"`
	WinnerTeamID string           `json:"winner_team_id,omitempty"`
}

func pgInt4Ptr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	value := v.Int32
	return &value
}

func pgUUIDStringOrEmpty(v pgtype.UUID) string {
	if !v.Valid {
		return ""
	}
	return db.UUIDString(v)
}

// toGameResponseFromListRow and toGameResponseFromGetRow map sqlc's two
// separately-named (but field-for-field identical) joined-game row types —
// gen.ListGamesByWeekWithTeamsRow (GET /weeks/:id/games) and
// gen.GetGameByIDWithTeamsRow (GET /games/:id) — to the same gameResponse
// shape. Kept as two explicit functions rather than one generic/converted
// helper so a future column added to only one of the two underlying queries
// doesn't silently misalign a shared struct.
func toGameResponseFromListRow(g gen.ListGamesByWeekWithTeamsRow) gameResponse {
	return gameResponse{
		ID:         db.UUIDString(g.ID),
		ExternalID: g.ExternalID,
		WeekID:     db.UUIDString(g.WeekID),
		KickoffAt:  formatTimestamp(g.KickoffAt),
		Status:     g.Status,
		HomeTeam: gameTeamResponse{
			ID:         db.UUIDString(g.HomeTeamID),
			Name:       g.HomeTeamName,
			Conference: g.HomeTeamConference,
			LogoURL:    g.HomeTeamLogoUrl.String,
		},
		AwayTeam: gameTeamResponse{
			ID:         db.UUIDString(g.AwayTeamID),
			Name:       g.AwayTeamName,
			Conference: g.AwayTeamConference,
			LogoURL:    g.AwayTeamLogoUrl.String,
		},
		HomeScore:    pgInt4Ptr(g.HomeScore),
		AwayScore:    pgInt4Ptr(g.AwayScore),
		WinnerTeamID: pgUUIDStringOrEmpty(g.WinnerTeamID),
	}
}

func toGameResponseFromGetRow(g gen.GetGameByIDWithTeamsRow) gameResponse {
	return gameResponse{
		ID:         db.UUIDString(g.ID),
		ExternalID: g.ExternalID,
		WeekID:     db.UUIDString(g.WeekID),
		KickoffAt:  formatTimestamp(g.KickoffAt),
		Status:     g.Status,
		HomeTeam: gameTeamResponse{
			ID:         db.UUIDString(g.HomeTeamID),
			Name:       g.HomeTeamName,
			Conference: g.HomeTeamConference,
			LogoURL:    g.HomeTeamLogoUrl.String,
		},
		AwayTeam: gameTeamResponse{
			ID:         db.UUIDString(g.AwayTeamID),
			Name:       g.AwayTeamName,
			Conference: g.AwayTeamConference,
			LogoURL:    g.AwayTeamLogoUrl.String,
		},
		HomeScore:    pgInt4Ptr(g.HomeScore),
		AwayScore:    pgInt4Ptr(g.AwayScore),
		WinnerTeamID: pgUUIDStringOrEmpty(g.WinnerTeamID),
	}
}

// --- Picks (Phase 4) ---

// upsertPickRequest is the body of PUT .../picks/me.
type upsertPickRequest struct {
	GameID string `json:"game_id"`
	TeamID string `json:"team_id"`
}

// pickResponse is a single pick, with `locked` computed live against the
// backing game's kickoff_at rather than stored — see internal/picks'
// package doc comment. Backs GET .../picks/me and the response of
// PUT .../picks/me.
type pickResponse struct {
	GameID string `json:"game_id"`
	TeamID string `json:"team_id"`
	Locked bool   `json:"locked"`
}

func toPickResponse(p gen.Pick, locked bool) pickResponse {
	return pickResponse{
		GameID: db.UUIDString(p.GameID),
		TeamID: db.UUIDString(p.TeamID),
		Locked: locked,
	}
}

// availableTeamResponse is one row of GET .../available-teams.
type availableTeamResponse struct {
	TeamID          string `json:"team_id"`
	TeamName        string `json:"team_name"`
	TeamLogoURL     string `json:"team_logo_url,omitempty"`
	OpponentTeamID  string `json:"opponent_team_id"`
	OpponentName    string `json:"opponent_name"`
	OpponentLogoURL string `json:"opponent_logo_url,omitempty"`
	GameID          string `json:"game_id"`
	KickoffAt       string `json:"kickoff_at"`
	IsHome          bool   `json:"is_home"`
	IsLocked        bool   `json:"is_locked"`
	IsUsedElsewhere bool   `json:"is_used_elsewhere"`
	IsCurrentPick   bool   `json:"is_current_pick"`

	// Matchup-predictor decision-support data — see picks.AvailableTeam's
	// doc comment. Not lock-gated (shown while a member is still
	// deciding), and nullable since CFBD's win-probability model isn't
	// published until close to kickoff.
	WinProbability *float64 `json:"win_probability,omitempty"`
	Spread         *float64 `json:"spread,omitempty"`
	SPPlusRank     *int32   `json:"sp_plus_rank,omitempty"`
	OpponentSPRank *int32   `json:"opponent_sp_plus_rank,omitempty"`
	PickCount      int32    `json:"pick_count"`
}

// availableTeamsResponse is the full response of GET .../available-teams:
// every pickable team for the week, plus the requester's current pick for
// that week (if any) surfaced at the top level too, so the UI can render
// the current selection without a second round-trip.
type availableTeamsResponse struct {
	Teams       []availableTeamResponse `json:"teams"`
	CurrentPick *pickResponse           `json:"current_pick,omitempty"`
}

// memberPickStatusResponse is one row of GET .../picks. game_id/team_id
// are omitted (via `omitempty` on a string that's left "" by the handler)
// for any OTHER member's pick whose game hasn't kicked off yet — the
// privacy rule from the API contract. The requester's own row always
// carries them when has_picked is true.
type memberPickStatusResponse struct {
	MembershipID string `json:"membership_id"`
	DisplayName  string `json:"display_name"`
	HasPicked    bool   `json:"has_picked"`
	GameID       string `json:"game_id,omitempty"`
	TeamID       string `json:"team_id,omitempty"`
}

// membershipWeekPickResponse is one row of GET
// .../members/{membershipId}/picks: one week of the season, with
// pick-identifying fields present only when the caller is entitled to see
// them (own membership always; another member's pick only once its game
// has kicked off) — every one of TeamID/TeamName/OpponentName/IsHome/
// Result is omitted together as a bundle, not just TeamID, so a
// not-yet-revealed pick never leaks even indirectly (e.g. via a
// team-specific result). Mirrors memberPickStatusResponse's privacy
// treatment, just across a season of weeks for one membership instead of
// one week for every membership.
type membershipWeekPickResponse struct {
	WeekNumber      int32  `json:"week_number"`
	HasPicked       bool   `json:"has_picked"`
	GameID          string `json:"game_id,omitempty"`
	TeamID          string `json:"team_id,omitempty"`
	TeamName        string `json:"team_name,omitempty"`
	TeamLogoURL     string `json:"team_logo_url,omitempty"`
	OpponentName    string `json:"opponent_name,omitempty"`
	OpponentLogoURL string `json:"opponent_logo_url,omitempty"`
	IsHome          bool   `json:"is_home,omitempty"`
	KickoffAt       string `json:"kickoff_at,omitempty"`
	Result          string `json:"result,omitempty"`
	IsLocked        bool   `json:"is_locked"`
}

// --- Notifications (Phase 7) ---

// registerDeviceTokenRequest is the body of POST /me/device-tokens.
type registerDeviceTokenRequest struct {
	Platform      string `json:"platform"`
	ExpoPushToken string `json:"expo_push_token"`
}

// deleteDeviceTokenRequest is the body of DELETE /me/device-tokens.
type deleteDeviceTokenRequest struct {
	ExpoPushToken string `json:"expo_push_token"`
}

type deviceTokenResponse struct {
	ID        string `json:"id"`
	Platform  string `json:"platform"`
	CreatedAt string `json:"created_at"`
}

func toDeviceTokenResponse(t gen.DeviceToken) deviceTokenResponse {
	return deviceTokenResponse{
		ID:        db.UUIDString(t.ID),
		Platform:  t.Platform,
		CreatedAt: formatTimestamp(t.CreatedAt),
	}
}

// notificationPreferencesResponse backs both GET and PUT
// /me/notification-preferences.
type notificationPreferencesResponse struct {
	PickReminder bool `json:"pick_reminder"`
	Eliminated   bool `json:"eliminated"`
	Survived     bool `json:"survived"`
	MassWipeout  bool `json:"mass_wipeout"`
	Buyback      bool `json:"buyback"`
	WeeklyRecap  bool `json:"weekly_recap"`
	EmailEnabled bool `json:"email_enabled"`
	PushEnabled  bool `json:"push_enabled"`
}

func toNotificationPreferencesResponse(p gen.NotificationPreference) notificationPreferencesResponse {
	return notificationPreferencesResponse{
		PickReminder: p.PickReminder,
		Eliminated:   p.Eliminated,
		Survived:     p.Survived,
		MassWipeout:  p.MassWipeout,
		Buyback:      p.Buyback,
		WeeklyRecap:  p.WeeklyRecap,
		EmailEnabled: p.EmailEnabled,
		PushEnabled:  p.PushEnabled,
	}
}

// updateNotificationPreferencesRequest is the body of PUT
// /me/notification-preferences — a full-replace update (every field
// required), matching UpsertNotificationPreferences' full-row upsert.
type updateNotificationPreferencesRequest struct {
	PickReminder bool `json:"pick_reminder"`
	Eliminated   bool `json:"eliminated"`
	Survived     bool `json:"survived"`
	MassWipeout  bool `json:"mass_wipeout"`
	Buyback      bool `json:"buyback"`
	WeeklyRecap  bool `json:"weekly_recap"`
	EmailEnabled bool `json:"email_enabled"`
	PushEnabled  bool `json:"push_enabled"`
}

// --- Admin (Phase 3) ---

type triggerScheduleSyncRequest struct {
	SeasonYear *int32 `json:"season_year"`
}

type syncRunResponse struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	StartedAt  string          `json:"started_at"`
	FinishedAt string          `json:"finished_at,omitempty"`
	Error      string          `json:"error,omitempty"`
	Details    json.RawMessage `json:"details"`
	CreatedAt  string          `json:"created_at"`
}

func toSyncRunResponse(r gen.SyncRun) syncRunResponse {
	resp := syncRunResponse{
		ID:        db.UUIDString(r.ID),
		Kind:      r.Kind,
		Status:    r.Status,
		StartedAt: formatTimestamp(r.StartedAt),
		Error:     r.Error.String,
		Details:   json.RawMessage(r.Details),
		CreatedAt: formatTimestamp(r.CreatedAt),
	}
	if r.FinishedAt.Valid {
		resp.FinishedAt = formatTimestamp(r.FinishedAt)
	}
	return resp
}

// --- Admin (Phase 8) ---

// paginationResponse is embedded in every paginated admin list response.
type paginationResponse struct {
	Total  int64 `json:"total"`
	Limit  int32 `json:"limit"`
	Offset int32 `json:"offset"`
}

// adminCommissionerResponse is the trimmed commissioner identity embedded
// in adminLeagueResponse — id/display_name/email, per the API contract.
type adminCommissionerResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// adminLeagueResponse is one row of GET /admin/leagues — every league in
// the system, unscoped to the requester (unlike leagueResponse, which
// always carries the requester's own membership).
type adminLeagueResponse struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Conference   string                    `json:"conference"`
	SeasonYear   int32                     `json:"season_year"`
	Status       string                    `json:"status"`
	Commissioner adminCommissionerResponse `json:"commissioner"`
	MemberCount  int64                     `json:"member_count"`
	CreatedAt    string                    `json:"created_at"`
}

func toAdminLeagueResponse(row gen.ListLeaguesAdminRow) adminLeagueResponse {
	return adminLeagueResponse{
		ID:         db.UUIDString(row.ID),
		Name:       row.Name,
		Conference: row.Conference,
		SeasonYear: row.SeasonYear,
		Status:     row.Status,
		Commissioner: adminCommissionerResponse{
			ID:          db.UUIDString(row.CommissionerUserID),
			DisplayName: row.CommissionerDisplayName,
			Email:       row.CommissionerEmail,
		},
		MemberCount: row.MemberCount,
		CreatedAt:   formatTimestamp(row.CreatedAt),
	}
}

type adminLeaguesListResponse struct {
	Leagues []adminLeagueResponse `json:"leagues"`
	paginationResponse
}

// adminUserResponse is one row of GET /admin/users — every user in the
// system.
type adminUserResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsSiteAdmin bool   `json:"is_site_admin"`
	Status      string `json:"status"`
	LeagueCount int64  `json:"league_count"`
	CreatedAt   string `json:"created_at"`
}

func toAdminUserResponse(row gen.ListUsersAdminRow) adminUserResponse {
	return adminUserResponse{
		ID:          db.UUIDString(row.ID),
		Email:       row.Email,
		DisplayName: row.DisplayName,
		IsSiteAdmin: row.IsSiteAdmin,
		Status:      row.Status,
		LeagueCount: row.LeagueCount,
		CreatedAt:   formatTimestamp(row.CreatedAt),
	}
}

type adminUsersListResponse struct {
	Users []adminUserResponse `json:"users"`
	paginationResponse
}

// adminUserDetailResponse is the response of POST /admin/users/:id/disable
// and .../enable — the updated user record in full (status is the field
// that just changed).
type adminUserDetailResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsSiteAdmin bool   `json:"is_site_admin"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

func toAdminUserDetailResponse(u gen.User) adminUserDetailResponse {
	return adminUserDetailResponse{
		ID:          db.UUIDString(u.ID),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		IsSiteAdmin: u.IsSiteAdmin,
		Status:      u.Status,
		CreatedAt:   formatTimestamp(u.CreatedAt),
	}
}

// finalizedLeagueWeekResponse is one entry of resyncGameResponse's
// finalized_league_weeks — a league-week that the resync's downstream
// grading pass actually finalized.
type finalizedLeagueWeekResponse struct {
	LeagueID    string `json:"league_id"`
	WeekID      string `json:"week_id"`
	MassWipeout bool   `json:"mass_wipeout"`
}

// resyncGameResponse is the response of POST /admin/games/:id/resync.
type resyncGameResponse struct {
	Game                 gameResponse                  `json:"game"`
	FinalizedLeagueWeeks []finalizedLeagueWeekResponse `json:"finalized_league_weeks"`
}

// toGameResponsePlain maps a plain (unjoined) gen.Game — as returned by a
// resync, which has no need for the joined team names GetGameByIDWithTeams
// carries — to gameResponse. HomeTeam/AwayTeam are left as their bare IDs
// (Name/Conference/LogoURL empty) since a resync response's caller already
// knows which teams are playing; a client wanting the full joined shape can
// follow up with GET /games/:id.
func toGameResponsePlain(g gen.Game) gameResponse {
	return gameResponse{
		ID:           db.UUIDString(g.ID),
		ExternalID:   g.ExternalID,
		WeekID:       db.UUIDString(g.WeekID),
		KickoffAt:    formatTimestamp(g.KickoffAt),
		Status:       g.Status,
		HomeTeam:     gameTeamResponse{ID: db.UUIDString(g.HomeTeamID)},
		AwayTeam:     gameTeamResponse{ID: db.UUIDString(g.AwayTeamID)},
		HomeScore:    pgInt4Ptr(g.HomeScore),
		AwayScore:    pgInt4Ptr(g.AwayScore),
		WinnerTeamID: pgUUIDStringOrEmpty(g.WinnerTeamID),
	}
}

// auditLogEntryResponse is one row of GET /admin/audit-log.
type auditLogEntryResponse struct {
	ID          string          `json:"id"`
	ActorUserID string          `json:"actor_user_id,omitempty"`
	LeagueID    string          `json:"league_id,omitempty"`
	Action      string          `json:"action"`
	TargetType  string          `json:"target_type,omitempty"`
	TargetID    string          `json:"target_id,omitempty"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   string          `json:"created_at"`
}

func toAuditLogEntryResponse(row gen.AuditLog) auditLogEntryResponse {
	return auditLogEntryResponse{
		ID:          db.UUIDString(row.ID),
		ActorUserID: pgUUIDStringOrEmpty(row.ActorUserID),
		LeagueID:    pgUUIDStringOrEmpty(row.LeagueID),
		Action:      row.Action,
		TargetType:  row.TargetType.String,
		TargetID:    pgUUIDStringOrEmpty(row.TargetID),
		Metadata:    json.RawMessage(row.Metadata),
		CreatedAt:   formatTimestamp(row.CreatedAt),
	}
}

type auditLogListResponse struct {
	Entries []auditLogEntryResponse `json:"entries"`
	paginationResponse
}
