package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResendEmailSender_Send_RequestShape confirms ResendEmailSender POSTs
// to the documented Resend endpoint with the documented Authorization
// header and JSON body shape (confirmed against Resend's API reference —
// see resend.go's DefaultResendURL doc comment).
func TestResendEmailSender_Send_RequestShape(t *testing.T) {
	var gotMethod string
	var gotHeaders http.Header
	var gotBody resendRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeaders = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`))
	}))
	t.Cleanup(server.Close)

	sender := NewResendEmailSender(server.Client(), server.URL, "test-api-key", "Survivor League <notifications@example.com>")
	err := sender.Send(context.Background(), EmailMessage{
		To:      "player@example.com",
		Subject: "You're eliminated",
		Text:    "Week 3 loss.",
		HTML:    "<p>Week 3 loss.</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if auth := gotHeaders.Get("Authorization"); auth != "Bearer test-api-key" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer test-api-key")
	}
	if ct := gotHeaders.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	if gotBody.From != "Survivor League <notifications@example.com>" {
		t.Errorf("From = %q, want the configured sender", gotBody.From)
	}
	if len(gotBody.To) != 1 || gotBody.To[0] != "player@example.com" {
		t.Errorf("To = %v, want [player@example.com]", gotBody.To)
	}
	if gotBody.Subject != "You're eliminated" {
		t.Errorf("Subject = %q, want %q", gotBody.Subject, "You're eliminated")
	}
	if gotBody.Text != "Week 3 loss." {
		t.Errorf("Text = %q, want %q", gotBody.Text, "Week 3 loss.")
	}
	if gotBody.HTML != "<p>Week 3 loss.</p>" {
		t.Errorf("HTML = %q, want %q", gotBody.HTML, "<p>Week 3 loss.</p>")
	}
}

// TestResendEmailSender_Send_FromAndReplyToOverrides confirms
// EmailMessage.From overrides the sender's configured default (used by
// SendLeagueBroadcastEmail, which sends from a distinct noreply address
// rather than RESEND_FROM_EMAIL) and ReplyTo is passed through only when
// set (used by SendFeedbackEmail, so the admin can reply straight to the
// submitter) — omitted from the request entirely otherwise, per
// resendRequest's `omitempty` tag.
func TestResendEmailSender_Send_FromAndReplyToOverrides(t *testing.T) {
	var gotBody resendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	t.Cleanup(server.Close)

	sender := NewResendEmailSender(server.Client(), server.URL, "test-api-key", "Survivor League <notifications@example.com>")
	err := sender.Send(context.Background(), EmailMessage{
		To:      "admin@survivorleague.football",
		Subject: "Survivor League feedback",
		Text:    "Great app!",
		ReplyTo: "player@example.com",
		From:    "Survivor League <noreply@survivorleague.football>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotBody.From != "Survivor League <noreply@survivorleague.football>" {
		t.Errorf("From = %q, want the message-level override", gotBody.From)
	}
	if len(gotBody.ReplyTo) != 1 || gotBody.ReplyTo[0] != "player@example.com" {
		t.Errorf("ReplyTo = %v, want [player@example.com]", gotBody.ReplyTo)
	}

	// A message with no ReplyTo set must omit the field entirely, not
	// send an empty array.
	gotBody = resendRequest{}
	if err := sender.Send(context.Background(), EmailMessage{To: "x@example.com", Subject: "x", Text: "y"}); err != nil {
		t.Fatalf("Send (no ReplyTo): %v", err)
	}
	if gotBody.ReplyTo != nil {
		t.Errorf("ReplyTo = %v, want nil when not set", gotBody.ReplyTo)
	}
	if gotBody.From != "Survivor League <notifications@example.com>" {
		t.Errorf("From = %q, want the sender's configured default when not overridden", gotBody.From)
	}
}

// TestResendEmailSender_Send_ErrorResponse_ReturnsResendError mirrors
// schedule's TestCFBDClient_NonOKStatus_ReturnsCFBDError pattern, using
// Resend's documented error shape ({statusCode, name, message}).
func TestResendEmailSender_Send_ErrorResponse_ReturnsResendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"statusCode":422,"name":"validation_error","message":"Invalid \"to\" field"}`))
	}))
	t.Cleanup(server.Close)

	sender := NewResendEmailSender(server.Client(), server.URL, "bad-key", "Survivor League <notifications@example.com>")
	err := sender.Send(context.Background(), EmailMessage{To: "not-an-email", Subject: "x", Text: "y"})
	if err == nil {
		t.Fatal("Send with a 422 response: got nil error")
	}
	var resendErr *ResendError
	if !errors.As(err, &resendErr) {
		t.Fatalf("error is not a *ResendError: %v (%T)", err, err)
	}
	if resendErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", resendErr.StatusCode)
	}
}

// TestResendEmailSender_Send_NoAPIKey_StillSendsWithoutAuthHeader
// confirms an empty API key (the "flagged but unavailable in this
// environment" state — see .env.example's RESEND_API_KEY) doesn't panic
// or send a malformed Authorization header; it just omits it.
func TestResendEmailSender_Send_NoAPIKey_OmitsAuthHeader(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	t.Cleanup(server.Close)

	sender := NewResendEmailSender(server.Client(), server.URL, "", "Survivor League <notifications@example.com>")
	if err := sender.Send(context.Background(), EmailMessage{To: "x@example.com", Subject: "x", Text: "y"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty (no API key configured)", gotAuth)
	}
}
