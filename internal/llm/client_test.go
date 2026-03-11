package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emandor/gostudentubl/internal/runner"
)

func TestGetSuggestionSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-model" {
			t.Fatalf("expected model 'test-model', got %q", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(req.Messages))
		}

		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: "Test suggestion content"}},
			},
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 42},
			Model: "test-model",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := &Client{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Model:    "test-model",
		HC:       &http.Client{Timeout: 5 * time.Second},
	}

	resp, err := c.GetSuggestion(context.Background(), runner.SuggestionRequest{
		EventType:  "assignment",
		CourseName: "Test Course",
		ItemName:   "Assignment 1",
		ItemTitle:  "Topic 1",
		Content:    "Some content",
	})
	if err != nil {
		t.Fatalf("GetSuggestion error: %v", err)
	}
	if resp.Suggestion != "Test suggestion content" {
		t.Fatalf("expected suggestion 'Test suggestion content', got %q", resp.Suggestion)
	}
	if resp.TokensUsed != 42 {
		t.Fatalf("expected 42 tokens used, got %d", resp.TokensUsed)
	}
}

func TestGetSuggestionAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer server.Close()

	c := &Client{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Model:    "test-model",
		HC:       &http.Client{Timeout: 5 * time.Second},
	}

	_, err := c.GetSuggestion(context.Background(), runner.SuggestionRequest{
		EventType: "quiz",
		Content:   "content",
	})
	if err == nil {
		t.Fatal("expected error for 429 response, got nil")
	}
}

func TestGetSuggestionEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[],"usage":{"total_tokens":0}}`))
	}))
	defer server.Close()

	c := &Client{
		Endpoint: server.URL,
		APIKey:   "test-key",
		Model:    "test-model",
		HC:       &http.Client{Timeout: 5 * time.Second},
	}

	_, err := c.GetSuggestion(context.Background(), runner.SuggestionRequest{
		EventType: "assignment",
		Content:   "content",
	})
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
}
