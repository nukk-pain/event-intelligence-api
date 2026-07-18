package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func Test_BackendChat_sets_minimal_reasoning_for_solar_open2(t *testing.T) {
	// Given
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{}}`)); err != nil {
			t.Errorf("write response body: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	backend := Backend{
		Name:    "solar",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "solar-open2",
	}

	// When
	_, _, _, err := backend.Chat(context.Background(), "system", "user", 3000, time.Second)

	// Then
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if got := requestBody["reasoning_effort"]; got != "minimal" {
		t.Fatalf("reasoning_effort = %v, want minimal", got)
	}
}
