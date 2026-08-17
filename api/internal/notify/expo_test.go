package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestExpoPushSender_Send_RequestShape confirms ExpoPushSender POSTs to
// the documented Expo Push endpoint with the documented headers and JSON
// body shape (confirmed against Expo's push notifications documentation —
// see expo.go's DefaultExpoPushURL doc comment) — one message object per
// token, carrying title/body/data/sound/priority.
func TestExpoPushSender_Send_RequestShape(t *testing.T) {
	var gotPath, gotMethod string
	var gotHeaders http.Header
	var gotBody []expoMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotHeaders = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"status":"ok","id":"ticket-1"},{"status":"ok","id":"ticket-2"}]}`))
	}))
	t.Cleanup(server.Close)

	sender := NewExpoPushSender(server.Client(), server.URL, "")
	err := sender.Send(context.Background(), PushMessage{
		Tokens: []string{"ExponentPushToken[aaa]", "ExponentPushToken[bbb]"},
		Title:  "You're eliminated",
		Body:   "Week 3 loss.",
		Data:   map[string]string{"type": "eliminated"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/" {
		// httptest.Server URL has no path component of its own; the client
		// should hit exactly the base URL, not append anything.
		t.Errorf("path = %q, want / (base URL only, per DefaultExpoPushURL = .../--/api/v2/push/send with no extra suffix appended)", gotPath)
	}
	if ct := gotHeaders.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if accept := gotHeaders.Get("Accept"); accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", accept)
	}

	if len(gotBody) != 2 {
		t.Fatalf("len(request body) = %d, want 2 (one message per token)", len(gotBody))
	}
	if gotBody[0].To != "ExponentPushToken[aaa]" {
		t.Errorf("gotBody[0].To = %q, want ExponentPushToken[aaa]", gotBody[0].To)
	}
	if gotBody[0].Title != "You're eliminated" || gotBody[0].Body != "Week 3 loss." {
		t.Errorf("gotBody[0] title/body = %q/%q, want the message's title/body", gotBody[0].Title, gotBody[0].Body)
	}
	if gotBody[0].Data["type"] != "eliminated" {
		t.Errorf("gotBody[0].Data[type] = %q, want eliminated", gotBody[0].Data["type"])
	}
	if gotBody[0].Sound != "default" {
		t.Errorf("gotBody[0].Sound = %q, want default", gotBody[0].Sound)
	}
}

// TestExpoPushSender_Send_EmptyTokensIsNoOp confirms Send never makes an
// HTTP call at all when there are no tokens to deliver to.
func TestExpoPushSender_Send_EmptyTokensIsNoOp(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	sender := NewExpoPushSender(server.Client(), server.URL, "")
	if err := sender.Send(context.Background(), PushMessage{Tokens: nil, Title: "x", Body: "y"}); err != nil {
		t.Fatalf("Send with no tokens: %v", err)
	}
	if called {
		t.Error("Send with no tokens made an HTTP call, want none")
	}
}

// TestExpoPushSender_Send_RequestLevelErrorsReturnError confirms a 200
// response whose top-level `errors` array is non-empty (Expo's documented
// request-level error shape) is surfaced as a Go error.
func TestExpoPushSender_Send_RequestLevelErrorsReturnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":[{"code":"PUSH_TOO_MANY_NOTIFICATIONS","message":"too many"}]}`))
	}))
	t.Cleanup(server.Close)

	sender := NewExpoPushSender(server.Client(), server.URL, "")
	err := sender.Send(context.Background(), PushMessage{Tokens: []string{"ExponentPushToken[aaa]"}, Title: "x", Body: "y"})
	if err == nil {
		t.Fatal("Send with a request-level error response: got nil error")
	}
	var expoErr *ExpoPushError
	if !errors.As(err, &expoErr) {
		t.Fatalf("error is not a *ExpoPushError: %v (%T)", err, err)
	}
}

// TestExpoPushSender_Send_TicketLevelErrorReturnsError confirms a
// per-message "status":"error" ticket (still inside an overall HTTP 200)
// is also surfaced as a Go error, per Expo's documented ticket-level error
// shape (e.g. DeviceNotRegistered).
func TestExpoPushSender_Send_TicketLevelErrorReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"status":"error","message":"\"ExponentPushToken[aaa]\" is not a registered push notification recipient","details":{"error":"DeviceNotRegistered"}}]}`))
	}))
	t.Cleanup(server.Close)

	sender := NewExpoPushSender(server.Client(), server.URL, "")
	err := sender.Send(context.Background(), PushMessage{Tokens: []string{"ExponentPushToken[aaa]"}, Title: "x", Body: "y"})
	if err == nil {
		t.Fatal("Send with a ticket-level error: got nil error")
	}
}

// TestExpoPushSender_Send_NonOKStatus_ReturnsExpoPushError mirrors
// schedule's TestCFBDClient_NonOKStatus_ReturnsCFBDError pattern.
func TestExpoPushSender_Send_NonOKStatus_ReturnsExpoPushError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errors":[{"code":"TOO_MANY_REQUESTS","message":"slow down"}]}`))
	}))
	t.Cleanup(server.Close)

	sender := NewExpoPushSender(server.Client(), server.URL, "")
	err := sender.Send(context.Background(), PushMessage{Tokens: []string{"ExponentPushToken[aaa]"}, Title: "x", Body: "y"})
	if err == nil {
		t.Fatal("Send with a 429 response: got nil error")
	}
	var expoErr *ExpoPushError
	if !errors.As(err, &expoErr) {
		t.Fatalf("error is not a *ExpoPushError: %v (%T)", err, err)
	}
	if expoErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", expoErr.StatusCode)
	}
}
