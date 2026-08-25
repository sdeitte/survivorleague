package notify

import (
	"context"
	"testing"
)

// TestLoggingEmailSender_Send_NeverErrors confirms LoggingEmailSender
// always succeeds without touching the network — the whole point of the
// safeguard in cmd/server/main.go (see LoggingEmailSender's doc comment
// for the incident that motivated it).
func TestLoggingEmailSender_Send_NeverErrors(t *testing.T) {
	s := NewLoggingEmailSender()
	if err := s.Send(context.Background(), EmailMessage{To: "x@example.test", Subject: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
}
