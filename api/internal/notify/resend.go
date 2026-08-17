package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DefaultResendURL is Resend's production email-send endpoint, confirmed
// against Resend's API reference (https://resend.com/docs/api-reference/emails/send-email):
// POST https://api.resend.com/emails, Authorization: Bearer <api key>,
// JSON body {from, to, subject, html/text, ...}, success response
// {"id": "<uuid>"}, error response {"statusCode", "name", "message"}.
const DefaultResendURL = "https://api.resend.com/emails"

// ResendEmailSender sends transactional email via the Resend API.
type ResendEmailSender struct {
	httpClient HTTPDoer
	baseURL    string
	apiKey     string
	from       string
}

// NewResendEmailSender constructs a ResendEmailSender. baseURL is
// configurable so it can be pointed at an httptest.Server in tests instead
// of the real api.resend.com — there is no real RESEND_API_KEY in this
// environment yet; see api/.env.example. from is the configured sender
// address (e.g. "Survivor League <notifications@yourdomain.com>") — Resend
// requires sending from a domain verified on the account, so this is
// deliberately not hardcoded.
func NewResendEmailSender(httpClient HTTPDoer, baseURL, apiKey, from string) *ResendEmailSender {
	if baseURL == "" {
		baseURL = DefaultResendURL
	}
	return &ResendEmailSender{httpClient: httpClient, baseURL: baseURL, apiKey: apiKey, from: from}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
}

// ResendError wraps a non-2xx response with Resend's documented error
// shape ({statusCode, name, message}) — mirrors schedule.CFBDError's
// pattern of carrying enough context to debug without a live-network
// repro.
type ResendError struct {
	StatusCode int
	Body       string
}

func (e *ResendError) Error() string {
	return fmt.Sprintf("resend: request failed with status %d: %s", e.StatusCode, e.Body)
}

// Send POSTs one Resend email-send request per call.
func (s *ResendEmailSender) Send(ctx context.Context, msg EmailMessage) error {
	body, err := json.Marshal(resendRequest{
		From:    s.from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		HTML:    msg.HTML,
		Text:    msg.Text,
	})
	if err != nil {
		return fmt.Errorf("resend: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("resend: read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ResendError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	return nil
}
