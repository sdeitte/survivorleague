package aiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GenerateText_Success(t *testing.T) {
	var gotBody messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key header = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicVersion {
			t.Errorf("anthropic-version header = %q, want %q", got, anthropicVersion)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content": [{"type": "text", "text": "Week 3 recap: "}, {"type": "text", "text": "chaos reigned."}]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "test-key", "test-model")
	text, err := client.GenerateText(context.Background(), "write a recap")
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if text != "Week 3 recap: chaos reigned." {
		t.Errorf("text = %q, want concatenation of both text blocks", text)
	}
	if gotBody.Model != "test-model" {
		t.Errorf("request model = %q, want %q", gotBody.Model, "test-model")
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Content != "write a recap" {
		t.Errorf("request messages = %+v, want one user message with the prompt", gotBody.Messages)
	}
}

func TestClient_GenerateText_DefaultsModel(t *testing.T) {
	var gotBody messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content": [{"type": "text", "text": "ok"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "test-key", "") // model="" -> DefaultModel
	if _, err := client.GenerateText(context.Background(), "hi"); err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if gotBody.Model != DefaultModel {
		t.Errorf("request model = %q, want DefaultModel %q", gotBody.Model, DefaultModel)
	}
}

func TestClient_GenerateText_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type": "error", "error": {"type": "authentication_error", "message": "invalid x-api-key"}}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "bad-key", "")
	_, err := client.GenerateText(context.Background(), "hi")
	if err == nil {
		t.Fatal("GenerateText: want error for 401 response, got nil")
	}
	aerr, ok := err.(*AnthropicError)
	if !ok {
		t.Fatalf("err = %T, want *AnthropicError", err)
	}
	if aerr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", aerr.StatusCode)
	}
	if aerr.Body != "invalid x-api-key" {
		t.Errorf("Body = %q, want the parsed error message", aerr.Body)
	}
}

func TestClient_GenerateText_NoTextContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content": []}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "test-key", "")
	if _, err := client.GenerateText(context.Background(), "hi"); err == nil {
		t.Fatal("GenerateText: want error when response has no text content, got nil")
	}
}
