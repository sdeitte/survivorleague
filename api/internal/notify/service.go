package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
)

// The notification types.
const (
	TypePickReminder = "pick_reminder"
	TypeEliminated   = "eliminated"
	TypeSurvived     = "survived"
	TypeMassWipeout  = "mass_wipeout"
	TypeBuyback      = "buyback"
	// TypeWeeklyRecap is the AI-generated weekly recap — email-only (see
	// EnqueueWeeklyRecap), unlike every other type above which sends push
	// too.
	TypeWeeklyRecap = "weekly_recap"
)

// The two delivery channels.
const (
	ChannelPush  = "push"
	ChannelEmail = "email"
)

// DefaultMaxAttempts is the retry cap the plan calls for ("a reasonable
// cap like 5") before a failing send is marked permanently 'failed'
// instead of retried on the next dispatcher tick.
const DefaultMaxAttempts = 5

// DefaultBatchSize is how many pending rows one Dispatcher tick claims —
// plenty of headroom for this app's scale (friends-and-family leagues),
// small enough to keep the claiming transaction (see
// ClaimPendingNotifications' query comment) short-lived.
const DefaultBatchSize = 25

// Service implements enqueueing (the five Enqueue* methods, called from
// the real trigger sites — grading, leagues, and this package's own
// reminder scan) and dispatch (DispatchBatch, called by Dispatcher's
// ticker loop) on top of the sqlc-generated queries.
type Service struct {
	queries     *gen.Queries
	pool        *pgxpool.Pool
	pushSender  PushSender
	emailSender EmailSender
	maxAttempts int32
	webBaseURL  string
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithWebBaseURL sets the frontend base URL used to build the join link in
// SendLeagueInviteEmail (mirrors internal/auth.WithWebBaseURL's identical
// role for password-reset/verification links). Omitted in tests that don't
// exercise that email.
func WithWebBaseURL(url string) Option {
	return func(s *Service) { s.webBaseURL = url }
}

// NewService constructs a Service. pushSender/emailSender are almost
// always the real ExpoPushSender/ResendEmailSender in production, and a
// test fake everywhere else — see the package doc comment.
func NewService(queries *gen.Queries, pool *pgxpool.Pool, pushSender PushSender, emailSender EmailSender, opts ...Option) *Service {
	s := &Service{
		queries:     queries,
		pool:        pool,
		pushSender:  pushSender,
		emailSender: emailSender,
		maxAttempts: DefaultMaxAttempts,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// notificationPayload is the shape written to notification_outbox.payload
// (and read back by the dispatcher) — rendered once at enqueue time (when
// the caller has the league/week/etc. context in hand) so the dispatcher
// itself stays generic across all five types.
type notificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// --- Enqueueing ---

// enqueueEvent inserts one notification_outbox row per channel, each with
// its own dedupe key (dedupeBase + ":" + channel) so e.g. an "eliminated"
// event's push row and email row are independently deduped — see
// notifications_log's UNIQUE(dedupe_key) column this mirrors. A conflict
// on an already-enqueued dedupe_key is treated as success, not an error:
// every Enqueue* method here is meant to be safely callable more than once
// for the same logical event.
func (s *Service) enqueueEvent(ctx context.Context, userID, leagueID, weekID pgtype.UUID, notifType string, channels []string, dedupeBase, title, body string) error {
	payload, err := json.Marshal(notificationPayload{Title: title, Body: body})
	if err != nil {
		return fmt.Errorf("notify: marshal payload: %w", err)
	}

	for _, channel := range channels {
		_, err := s.queries.EnqueueNotification(ctx, gen.EnqueueNotificationParams{
			UserID:    userID,
			LeagueID:  leagueID,
			WeekID:    weekID,
			Type:      notifType,
			Channel:   channel,
			Payload:   payload,
			DedupeKey: dedupeBase + ":" + channel,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue // already enqueued for this channel — not an error.
			}
			return fmt.Errorf("notify: enqueue %s/%s for user %s: %w", notifType, channel, db.UUIDString(userID), err)
		}
	}
	return nil
}

// EnqueueEliminated enqueues push+email rows for a membership that was
// just eliminated. Called from internal/grading.Service.TryFinalizeLeagueWeek
// (via the grading.Notifier interface Service satisfies structurally)
// after its finalization transaction commits.
func (s *Service) EnqueueEliminated(ctx context.Context, membershipID, leagueID, weekID pgtype.UUID) error {
	membership, league, week, err := s.loadLeagueWeekEventContext(ctx, membershipID, leagueID, weekID)
	if err != nil {
		return err
	}
	title := "You're eliminated"
	body := fmt.Sprintf("You were eliminated from %s in week %d. Better luck next season — or ask your commissioner about a buy-back.", league.Name, week.WeekNumber)
	dedupeBase := fmt.Sprintf("%s:%s:%s", TypeEliminated, db.UUIDString(membershipID), db.UUIDString(weekID))
	return s.enqueueEvent(ctx, membership.UserID, leagueID, weekID, TypeEliminated, []string{ChannelPush, ChannelEmail}, dedupeBase, title, body)
}

// EnqueueSurvived enqueues a push-only row (per the plan: "survived" is
// push-only, opt-out) for a membership whose pick won in a week that did
// NOT mass-wipe-out. Called from the same TryFinalizeLeagueWeek site as
// EnqueueEliminated.
func (s *Service) EnqueueSurvived(ctx context.Context, membershipID, leagueID, weekID pgtype.UUID) error {
	membership, league, week, err := s.loadLeagueWeekEventContext(ctx, membershipID, leagueID, weekID)
	if err != nil {
		return err
	}
	title := "You survived!"
	body := fmt.Sprintf("Your pick held up in %s, week %d. You're still alive!", league.Name, week.WeekNumber)
	dedupeBase := fmt.Sprintf("%s:%s:%s", TypeSurvived, db.UUIDString(membershipID), db.UUIDString(weekID))
	return s.enqueueEvent(ctx, membership.UserID, leagueID, weekID, TypeSurvived, []string{ChannelPush}, dedupeBase, title, body)
}

// EnqueueMassWipeout enqueues push+email rows for a membership whose
// league-week finalized with mass_wipeout=true (every active contestant
// lost or missed — per the plan's rule, nobody is eliminated that week).
// Called once per active contestant from the same TryFinalizeLeagueWeek
// site as EnqueueEliminated/EnqueueSurvived.
func (s *Service) EnqueueMassWipeout(ctx context.Context, membershipID, leagueID, weekID pgtype.UUID) error {
	membership, league, week, err := s.loadLeagueWeekEventContext(ctx, membershipID, leagueID, weekID)
	if err != nil {
		return err
	}
	title := "Mass wipeout!"
	body := fmt.Sprintf("Everyone in %s lost (or missed) their pick in week %d — nobody's eliminated this week. Fresh start next week.", league.Name, week.WeekNumber)
	dedupeBase := fmt.Sprintf("%s:%s:%s", TypeMassWipeout, db.UUIDString(membershipID), db.UUIDString(weekID))
	return s.enqueueEvent(ctx, membership.UserID, leagueID, weekID, TypeMassWipeout, []string{ChannelPush, ChannelEmail}, dedupeBase, title, body)
}

// EnqueueBuyback enqueues push+email rows for a member the commissioner
// just reinstated. Called from internal/leagues.Service.BuyBackMember (via
// the leagues.Notifier interface Service satisfies structurally) on
// success. Buy-back is a one-time-ever lifeline per membership (enforced
// by internal/leagues, not here), so a bare membership_id dedupe key
// (no week_id — a buy-back isn't scoped to one) is sufficient.
func (s *Service) EnqueueBuyback(ctx context.Context, membershipID, leagueID pgtype.UUID) error {
	membership, err := s.queries.GetLeagueMembershipByID(ctx, membershipID)
	if err != nil {
		return fmt.Errorf("notify: load membership %s: %w", db.UUIDString(membershipID), err)
	}
	league, err := s.queries.GetLeagueByID(ctx, leagueID)
	if err != nil {
		return fmt.Errorf("notify: load league %s: %w", db.UUIDString(leagueID), err)
	}
	title := "You're back in!"
	body := fmt.Sprintf("Your commissioner bought you back into %s. Your previously-used teams stay locked, but you're alive again.", league.Name)
	dedupeBase := fmt.Sprintf("%s:%s", TypeBuyback, db.UUIDString(membershipID))
	return s.enqueueEvent(ctx, membership.UserID, leagueID, pgtype.UUID{}, TypeBuyback, []string{ChannelPush, ChannelEmail}, dedupeBase, title, body)
}

// EnqueuePickReminder enqueues push+email rows for a membership approaching
// a pick deadline. window is "24h" or "3h" — folded into the dedupe key
// (per the plan: "a dedupe key incorporating the specific window ... so
// each window fires at most once") so calling this every hour the
// condition still holds is safe; only the first call per window actually
// inserts anything. Called from ScanPickReminders (reminder.go).
func (s *Service) EnqueuePickReminder(ctx context.Context, membershipID, leagueID, weekID pgtype.UUID, window string) error {
	membership, err := s.queries.GetLeagueMembershipByID(ctx, membershipID)
	if err != nil {
		return fmt.Errorf("notify: load membership %s: %w", db.UUIDString(membershipID), err)
	}
	league, err := s.queries.GetLeagueByID(ctx, leagueID)
	if err != nil {
		return fmt.Errorf("notify: load league %s: %w", db.UUIDString(leagueID), err)
	}
	windowLabel := window
	switch window {
	case "24h":
		windowLabel = "about 24 hours"
	case "3h":
		windowLabel = "about 3 hours"
	}
	title := "Pick reminder"
	body := fmt.Sprintf("You haven't made your pick in %s yet — your deadline is in %s.", league.Name, windowLabel)
	dedupeBase := fmt.Sprintf("%s:%s:%s:%s", TypePickReminder, window, db.UUIDString(membershipID), db.UUIDString(weekID))
	return s.enqueueEvent(ctx, membership.UserID, leagueID, weekID, TypePickReminder, []string{ChannelPush, ChannelEmail}, dedupeBase, title, body)
}

// EnqueueWeeklyRecap enqueues one email-only row per active (non-removed)
// member of leagueID for the AI-generated weekly recap — unlike every
// other Enqueue* method above, this is per-LEAGUE, not per-membership
// (called once, from internal/recap.Service.GenerateWeekRecap right after
// it stores the recap, via the recap.EmailNotifier interface Service
// satisfies structurally), so it fans out to every member itself rather
// than the caller doing it. recapBody is used as-is as the email body —
// see GenerateWeekRecap's doc comment for how that text is constrained to
// real facts only.
func (s *Service) EnqueueWeeklyRecap(ctx context.Context, leagueID, weekID pgtype.UUID, recapBody string) error {
	league, err := s.queries.GetLeagueByID(ctx, leagueID)
	if err != nil {
		return fmt.Errorf("notify: load league %s: %w", db.UUIDString(leagueID), err)
	}
	week, err := s.queries.GetWeekByID(ctx, weekID)
	if err != nil {
		return fmt.Errorf("notify: load week %s: %w", db.UUIDString(weekID), err)
	}
	members, err := s.queries.ListActiveMembersWithUser(ctx, leagueID)
	if err != nil {
		return fmt.Errorf("notify: list members for league %s: %w", db.UUIDString(leagueID), err)
	}

	title := fmt.Sprintf("%s — Week %d recap", league.Name, week.WeekNumber)
	var firstErr error
	for _, m := range members {
		dedupeBase := fmt.Sprintf("%s:%s:%s", TypeWeeklyRecap, db.UUIDString(m.MembershipID), db.UUIDString(weekID))
		if err := s.enqueueEvent(ctx, m.UserID, leagueID, weekID, TypeWeeklyRecap, []string{ChannelEmail}, dedupeBase, title, recapBody); err != nil {
			log.Printf("notify: enqueue weekly recap for membership %s: %v", db.UUIDString(m.MembershipID), err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// SendLeagueClosedEmail sends a direct, synchronous transactional email
// informing a member that their league was closed by its commissioner.
// Deliberately bypasses the outbox/preferences/dedupe machinery the five
// Enqueue* event types use — this is a one-off commissioner action with no
// retry-worthy dedupe key. Errors are the caller's to log, not fail on —
// see internal/leagues.Service.CloseLeague's callers in httpapi.
func (s *Service) SendLeagueClosedEmail(ctx context.Context, toEmail, toDisplayName, leagueName string) error {
	body := fmt.Sprintf(
		"Hi %s, your commissioner has closed the league %q. No new picks or changes can be made, "+
			"but the league and its full history are still there if you want to look back.",
		toDisplayName, leagueName,
	)
	return s.emailSender.Send(ctx, EmailMessage{
		To:      toEmail,
		Subject: fmt.Sprintf("%s has been closed", leagueName),
		Text:    body,
		HTML:    "<p>" + html.EscapeString(body) + "</p>",
	})
}

// SendLeagueInviteEmail sends a direct, synchronous transactional email
// inviting someone (who may not have an account yet) to join a league via
// its existing shareable invite code — backs the commissioner's bulk
// "invite by email" action (POST .../invite/send). Bypasses the outbox/
// preferences/dedupe machinery like SendLeagueClosedEmail: the recipient
// has no user account yet, so there's no notification_preferences row to
// check and no dedupe key worth tracking — a commissioner re-sending the
// same invite twice is expected, not a bug to guard against.
func (s *Service) SendLeagueInviteEmail(ctx context.Context, toEmail, toDisplayName, leagueName, conference string, seasonYear int32, inviteCode string) error {
	greeting := "Hi there"
	if toDisplayName != "" {
		greeting = "Hi " + toDisplayName
	}
	joinURL := s.webBaseURL + "/leagues/join?code=" + url.QueryEscape(inviteCode)
	text := fmt.Sprintf(
		"%s, you've been invited to join %q (%s, %d) on Survivor League. Use invite code %s to join, or open %s.",
		greeting, leagueName, conference, seasonYear, inviteCode, joinURL,
	)
	htmlBody := fmt.Sprintf(
		"<p>%s, you've been invited to join <strong>%s</strong> (%s, %d) on Survivor League.</p>"+
			"<p>Invite code: <strong style=\"font-size:1.2em;letter-spacing:2px\">%s</strong></p>"+
			"<p><a href=\"%s\">Join the league</a></p>",
		html.EscapeString(greeting), html.EscapeString(leagueName), html.EscapeString(conference), seasonYear,
		html.EscapeString(inviteCode), html.EscapeString(joinURL),
	)
	return s.emailSender.Send(ctx, EmailMessage{
		To:      toEmail,
		Subject: fmt.Sprintf("You're invited to join %s", leagueName),
		Text:    text,
		HTML:    htmlBody,
	})
}

// loadLeagueWeekEventContext resolves the membership/league/week rows
// shared by EnqueueEliminated/EnqueueSurvived/EnqueueMassWipeout.
func (s *Service) loadLeagueWeekEventContext(ctx context.Context, membershipID, leagueID, weekID pgtype.UUID) (gen.LeagueMembership, gen.League, gen.Week, error) {
	membership, err := s.queries.GetLeagueMembershipByID(ctx, membershipID)
	if err != nil {
		return gen.LeagueMembership{}, gen.League{}, gen.Week{}, fmt.Errorf("notify: load membership %s: %w", db.UUIDString(membershipID), err)
	}
	league, err := s.queries.GetLeagueByID(ctx, leagueID)
	if err != nil {
		return gen.LeagueMembership{}, gen.League{}, gen.Week{}, fmt.Errorf("notify: load league %s: %w", db.UUIDString(leagueID), err)
	}
	week, err := s.queries.GetWeekByID(ctx, weekID)
	if err != nil {
		return gen.LeagueMembership{}, gen.League{}, gen.Week{}, fmt.Errorf("notify: load week %s: %w", db.UUIDString(weekID), err)
	}
	return membership, league, week, nil
}

// --- Preferences / device tokens (backing the /me endpoints) ---

// GetPreferences returns userID's notification preferences, lazily
// creating a default row (every type on, per the schema's column
// defaults) if none exists yet.
func (s *Service) GetPreferences(ctx context.Context, userID pgtype.UUID) (gen.NotificationPreference, error) {
	return s.queries.GetOrCreateNotificationPreferences(ctx, userID)
}

// UpdatePreferences replaces userID's notification preferences in full.
func (s *Service) UpdatePreferences(ctx context.Context, userID pgtype.UUID, prefs gen.UpsertNotificationPreferencesParams) (gen.NotificationPreference, error) {
	prefs.UserID = userID
	return s.queries.UpsertNotificationPreferences(ctx, prefs)
}

// RegisterDeviceToken upserts a device token for userID. See
// UpsertDeviceToken's query comment for why re-registering an existing
// token reassigns it rather than erroring.
func (s *Service) RegisterDeviceToken(ctx context.Context, userID pgtype.UUID, token, platform string) (gen.DeviceToken, error) {
	return s.queries.UpsertDeviceToken(ctx, gen.UpsertDeviceTokenParams{UserID: userID, Token: token, Platform: platform})
}

// DeleteDeviceToken removes a device token for userID (e.g. on logout/
// uninstall). Not an error if the token doesn't exist / belongs to another
// user — DELETE affects zero rows silently, matching the API contract's
// "remove a token" framing (idempotent).
func (s *Service) DeleteDeviceToken(ctx context.Context, userID pgtype.UUID, token string) error {
	return s.queries.DeleteDeviceToken(ctx, gen.DeleteDeviceTokenParams{UserID: userID, Token: token})
}

// --- Dispatch ---

// DispatchBatch claims up to batchSize pending rows (FOR UPDATE SKIP
// LOCKED — see ClaimPendingNotifications' query comment) and processes
// each one to a terminal or retryable outcome, all inside one transaction.
// Returns the number of rows claimed (processed, whether sent/skipped/
// failed/retried) this call. Safe to call concurrently — see the package
// tests for the concurrency proof.
func (s *Service) DispatchBatch(ctx context.Context, batchSize int32) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("notify: begin dispatch tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once committed

	qtx := s.queries.WithTx(tx)

	rows, err := qtx.ClaimPendingNotifications(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("notify: claim pending notifications: %w", err)
	}

	for _, row := range rows {
		if err := s.processRow(ctx, qtx, row); err != nil {
			log.Printf("notify: process outbox row %s (type=%s channel=%s): %v", db.UUIDString(row.ID), row.Type, row.Channel, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("notify: commit dispatch batch: %w", err)
	}
	return len(rows), nil
}

// processRow resolves one claimed row to an outcome: 'skipped' (opted
// out, or nothing to deliver to), 'sent', or a retry/'failed' via
// MarkNotificationFailedOrRetry.
func (s *Service) processRow(ctx context.Context, qtx *gen.Queries, row gen.NotificationOutbox) error {
	prefs, err := qtx.GetOrCreateNotificationPreferences(ctx, row.UserID)
	if err != nil {
		return fmt.Errorf("load preferences: %w", err)
	}

	if !typeEnabled(prefs, row.Type) || !channelEnabled(prefs, row.Channel) {
		return s.finishSkipped(ctx, qtx, row)
	}

	var payload notificationPayload
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	var sendErr error
	switch row.Channel {
	case ChannelPush:
		tokens, err := qtx.ListDeviceTokensForUser(ctx, row.UserID)
		if err != nil {
			return fmt.Errorf("list device tokens: %w", err)
		}
		if len(tokens) == 0 {
			// Nothing to deliver to — not a transient failure a retry
			// would fix any time soon, so terminal-skip rather than churn
			// through the retry cap.
			return s.finishSkipped(ctx, qtx, row)
		}
		tokenStrings := make([]string, len(tokens))
		for i, t := range tokens {
			tokenStrings[i] = t.Token
		}
		sendErr = s.pushSender.Send(ctx, PushMessage{
			Tokens: tokenStrings,
			Title:  payload.Title,
			Body:   payload.Body,
			Data:   map[string]string{"type": row.Type},
		})
	case ChannelEmail:
		user, err := qtx.GetUserByID(ctx, row.UserID)
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		sendErr = s.emailSender.Send(ctx, EmailMessage{
			To:      user.Email,
			Subject: payload.Title,
			Text:    payload.Body,
			HTML:    "<p>" + html.EscapeString(payload.Body) + "</p>",
		})
	default:
		sendErr = fmt.Errorf("unknown channel %q", row.Channel)
	}

	if sendErr == nil {
		return s.finishSent(ctx, qtx, row)
	}
	return s.finishFailedOrRetry(ctx, qtx, row, sendErr)
}

func typeEnabled(p gen.NotificationPreference, notifType string) bool {
	switch notifType {
	case TypePickReminder:
		return p.PickReminder
	case TypeEliminated:
		return p.Eliminated
	case TypeSurvived:
		return p.Survived
	case TypeMassWipeout:
		return p.MassWipeout
	case TypeBuyback:
		return p.Buyback
	case TypeWeeklyRecap:
		return p.WeeklyRecap
	default:
		return true
	}
}

func channelEnabled(p gen.NotificationPreference, channel string) bool {
	switch channel {
	case ChannelPush:
		return p.PushEnabled
	case ChannelEmail:
		return p.EmailEnabled
	default:
		return true
	}
}

func (s *Service) finishSkipped(ctx context.Context, qtx *gen.Queries, row gen.NotificationOutbox) error {
	if err := qtx.MarkNotificationSkipped(ctx, row.ID); err != nil {
		return fmt.Errorf("mark skipped: %w", err)
	}
	if err := qtx.UpsertNotificationLog(ctx, gen.UpsertNotificationLogParams{
		UserID:    row.UserID,
		LeagueID:  row.LeagueID,
		WeekID:    row.WeekID,
		Type:      row.Type,
		Channel:   row.Channel,
		Status:    "skipped",
		DedupeKey: row.DedupeKey,
	}); err != nil {
		return fmt.Errorf("upsert notifications_log (skipped): %w", err)
	}
	return nil
}

func (s *Service) finishSent(ctx context.Context, qtx *gen.Queries, row gen.NotificationOutbox) error {
	if err := qtx.MarkNotificationSent(ctx, row.ID); err != nil {
		return fmt.Errorf("mark sent: %w", err)
	}
	if err := qtx.UpsertNotificationLog(ctx, gen.UpsertNotificationLogParams{
		UserID:    row.UserID,
		LeagueID:  row.LeagueID,
		WeekID:    row.WeekID,
		Type:      row.Type,
		Channel:   row.Channel,
		Status:    "sent",
		DedupeKey: row.DedupeKey,
		SentAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		return fmt.Errorf("upsert notifications_log (sent): %w", err)
	}
	return nil
}

// finishFailedOrRetry increments the row's attempt count. Once
// s.maxAttempts is reached the row (and, only then, the notifications_log
// record) moves to the terminal 'failed' status; otherwise it's left
// 'pending' for the next Dispatcher tick to retry, and notifications_log
// is deliberately NOT written yet — there's no confirmed terminal outcome
// to audit until either a send succeeds or the retry cap is hit.
func (s *Service) finishFailedOrRetry(ctx context.Context, qtx *gen.Queries, row gen.NotificationOutbox, sendErr error) error {
	updated, err := qtx.MarkNotificationFailedOrRetry(ctx, gen.MarkNotificationFailedOrRetryParams{
		MaxAttempts: s.maxAttempts,
		ID:          row.ID,
	})
	if err != nil {
		return fmt.Errorf("mark failed/retry: %w", err)
	}
	if updated.Status != "failed" {
		// Still retryable — leave notifications_log untouched for now.
		return fmt.Errorf("send failed (attempt %d/%d, will retry): %w", updated.Attempts, s.maxAttempts, sendErr)
	}
	if err := qtx.UpsertNotificationLog(ctx, gen.UpsertNotificationLogParams{
		UserID:    row.UserID,
		LeagueID:  row.LeagueID,
		WeekID:    row.WeekID,
		Type:      row.Type,
		Channel:   row.Channel,
		Status:    "failed",
		DedupeKey: row.DedupeKey,
	}); err != nil {
		return fmt.Errorf("upsert notifications_log (failed): %w", err)
	}
	return fmt.Errorf("send permanently failed after %d attempts: %w", updated.Attempts, sendErr)
}
