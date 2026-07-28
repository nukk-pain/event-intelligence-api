package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type captureSearch struct {
	query string
}

func (s *captureSearch) Search(_ context.Context, query string) ([]SearchResult, error) {
	s.query = query
	return nil, nil
}

func TestDiscover_strips_contacts_from_goal_before_backend_request(t *testing.T) {
	// Given
	var backendUserMessage string
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode backend request: %v", err)
		}
		for _, message := range request.Messages {
			if message.Role == "user" {
				backendUserMessage = message.Content
			}
		}
		content := `{\"action\":\"done\"}`
		if calls.Add(1) == 1 {
			content = `{\"action\":\"search\",\"query\":\"AI events\"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + content + `"}}]}`))
	}))
	t.Cleanup(server.Close)
	backend := Backend{Name: "test", BaseURL: server.URL, Model: "test-model"}
	search := &captureSearch{}
	goal := "Find events for person@example.com or 010-1234-5678"

	// When
	_, _, err := Discover(context.Background(), backend, goal, search, 1, 100, time.Second)

	// Then
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	for _, contact := range []string{"person@example.com", "010-1234-5678"} {
		if strings.Contains(backendUserMessage, contact) {
			t.Fatalf("backend user message contains contact %q", contact)
		}
	}
	if search.query != "AI events" {
		t.Fatalf("search query = %q, want %q", search.query, "AI events")
	}
}
