package notify

import (
	"context"
	"log"
)

// LoggingEmailSender logs every email it's asked to send instead of
// actually delivering it — no network call, ever. This exists specifically
// so a local/dev process can never repeat the incident that motivated it:
// a `go run ./cmd/server` left running against a local dev database for
// days, with a real RESEND_API_KEY configured, silently turned every
// email-generating test run's outbox rows (synthetic @example.test
// addresses included) into real Resend API sends and burned through the
// account's quota. See cmd/server/main.go's emailSender construction —
// this is what a non-production APP_ENV gets by default, real delivery
// requires an explicit opt-in.
type LoggingEmailSender struct{}

// NewLoggingEmailSender constructs a LoggingEmailSender.
func NewLoggingEmailSender() *LoggingEmailSender {
	return &LoggingEmailSender{}
}

// Send logs the message and returns nil — success from every caller's
// point of view, since "would have sent" is the entire point in a
// non-production environment.
func (s *LoggingEmailSender) Send(_ context.Context, msg EmailMessage) error {
	log.Printf("notify: [dev, not actually sent] email to=%s subject=%q", msg.To, msg.Subject)
	return nil
}
