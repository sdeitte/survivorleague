package notify

import (
	"context"
	"net/http"
)

// HTTPDoer is the subset of *http.Client the real Expo/Resend senders
// depend on — mirrors internal/schedule's own HTTPDoer. Accepting this
// interface (rather than a concrete *http.Client) is what makes both
// senders testable against an httptest.Server with zero live network
// calls, same pattern Phase 3 used for CFBDClient.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// PushMessage is one push notification to deliver to one or more of a
// single user's registered devices.
type PushMessage struct {
	Tokens []string
	Title  string
	Body   string
	// Data is carried as the Expo push "data" payload — arbitrary JSON a
	// client can use for deep-linking (e.g. {"league_id": "...",
	// "type": "eliminated"}).
	Data map[string]string
}

// PushSender delivers a push notification. Implementations: ExpoPushSender
// (real, expo.go) for production, a test fake for every test in this
// package and its callers' tests.
type PushSender interface {
	Send(ctx context.Context, msg PushMessage) error
}

// EmailMessage is one transactional email.
type EmailMessage struct {
	To      string
	Subject string
	Text    string
	HTML    string
	// ReplyTo, when set, is passed through to the provider's reply-to
	// header — lets a recipient reply straight to a specific address
	// (e.g. the submitter of a feedback email) instead of whatever To
	// happens to be.
	ReplyTo string
	// From, when set, overrides the sender's configured default From
	// address for this one message — e.g. a commissioner's league-wide
	// broadcast goes out from a distinct noreply address rather than
	// whatever RESEND_FROM_EMAIL is configured to for ordinary
	// transactional email (invites, password reset, etc.).
	From string
}

// EmailSender delivers a transactional email. Implementations:
// ResendEmailSender (real, resend.go) for production, a test fake for
// every test in this package and its callers' tests.
type EmailSender interface {
	Send(ctx context.Context, msg EmailMessage) error
}
