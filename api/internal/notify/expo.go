package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DefaultExpoPushURL is Expo's production push-send endpoint, confirmed
// against Expo's push notifications documentation
// (https://docs.expo.dev/push-notifications/sending-notifications/):
// POST https://exp.host/--/api/v2/push/send, request body either a single
// message object or an array of up to 100, response
// {"data": [{"status": "ok"|"error", "id"/"message"/"details", ...}, ...]}
// or, for a request-level failure, {"errors": [{"code","message"}, ...]}.
const DefaultExpoPushURL = "https://exp.host/--/api/v2/push/send"

// ExpoPushSender sends push notifications via the Expo Push API. No
// access token is required for Expo push delivery itself (unlike CFBD/
// Resend, which are bearer-token-authenticated) — accessToken here is
// Expo's optional "enhanced security" feature and may be left empty.
type ExpoPushSender struct {
	httpClient  HTTPDoer
	baseURL     string
	accessToken string
}

// NewExpoPushSender constructs an ExpoPushSender. baseURL is configurable
// so it can be pointed at an httptest.Server in tests instead of the real
// exp.host — there is no on-device Expo push token to test against in
// this environment; see the package doc comment.
func NewExpoPushSender(httpClient HTTPDoer, baseURL, accessToken string) *ExpoPushSender {
	if baseURL == "" {
		baseURL = DefaultExpoPushURL
	}
	return &ExpoPushSender{httpClient: httpClient, baseURL: baseURL, accessToken: accessToken}
}

// expoMessage is one element of the request body array — field names/
// casing per Expo's documented request shape.
type expoMessage struct {
	To       string            `json:"to"`
	Title    string            `json:"title,omitempty"`
	Body     string            `json:"body,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
	Sound    string            `json:"sound,omitempty"`
	Priority string            `json:"priority,omitempty"`
}

type expoTicket struct {
	Status  string         `json:"status"`
	ID      string         `json:"id,omitempty"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type expoResponse struct {
	Data   []expoTicket `json:"data,omitempty"`
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// ExpoPushError wraps a non-200 response, or a 200 response whose
// request-level `errors` array is non-empty, with enough context to debug
// without a live-network repro — mirrors schedule.CFBDError's shape.
type ExpoPushError struct {
	StatusCode int
	Body       string
}

func (e *ExpoPushError) Error() string {
	return fmt.Sprintf("expo push: request failed with status %d: %s", e.StatusCode, e.Body)
}

// Send POSTs one Expo push request per call, one message per token in
// msg.Tokens (Expo's documented limit is 100 messages per request; every
// realistic call site here sends to a single user's handful of devices,
// well under that). A no-op (nil error) if msg.Tokens is empty — nothing
// to send to.
func (s *ExpoPushSender) Send(ctx context.Context, msg PushMessage) error {
	if len(msg.Tokens) == 0 {
		return nil
	}

	messages := make([]expoMessage, 0, len(msg.Tokens))
	for _, token := range msg.Tokens {
		messages = append(messages, expoMessage{
			To:       token,
			Title:    msg.Title,
			Body:     msg.Body,
			Data:     msg.Data,
			Sound:    "default",
			Priority: "high",
		})
	}

	body, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("expo push: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("expo push: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Content-Type", "application/json")
	if s.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.accessToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("expo push: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("expo push: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &ExpoPushError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var parsed expoResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("expo push: decode response: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return &ExpoPushError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	// Per-ticket errors (e.g. DeviceNotRegistered) still come back as an
	// overall HTTP 200 — surface them as a Send() failure so the
	// dispatcher's retry/failure accounting applies, rather than silently
	// treating a fully-rejected send as delivered.
	for _, ticket := range parsed.Data {
		if ticket.Status == "error" {
			return &ExpoPushError{StatusCode: resp.StatusCode, Body: string(respBody)}
		}
	}

	return nil
}
