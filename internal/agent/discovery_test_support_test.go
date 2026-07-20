package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type modelReply struct {
	content          string
	completionTokens int
}

type capturedChatRequest struct {
	MaxTokens int `json:"max_tokens"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type scriptedModel struct {
	mu       sync.Mutex
	replies  []modelReply
	requests []capturedChatRequest
}

func newScriptedBackend(t *testing.T, replies ...modelReply) (Backend, *scriptedModel) {
	t.Helper()
	model := &scriptedModel{replies: append([]modelReply(nil), replies...)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request capturedChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		model.mu.Lock()
		index := len(model.requests)
		model.requests = append(model.requests, request)
		if index >= len(model.replies) {
			model.mu.Unlock()
			http.Error(w, "unexpected call", http.StatusInternalServerError)
			return
		}
		reply := model.replies[index]
		model.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": reply.content}}},
			"usage":   map[string]int{"completion_tokens": reply.completionTokens},
		})
	}))
	t.Cleanup(server.Close)
	return Backend{Name: "test", BaseURL: server.URL, Model: "test-model"}, model
}

func (m *scriptedModel) capturedRequests() []capturedChatRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]capturedChatRequest(nil), m.requests...)
}

type staticSearch struct {
	mu      sync.Mutex
	results []SearchResult
	queries []string
}

type sequenceSearch struct {
	mu      sync.Mutex
	batches [][]SearchResult
}

func (s *sequenceSearch) Search(ctx context.Context, _ string) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.batches) == 0 {
		return nil, nil
	}
	batch := append([]SearchResult(nil), s.batches[0]...)
	s.batches = s.batches[1:]
	return batch, nil
}

func (s *staticSearch) Search(ctx context.Context, query string) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, query)
	return append([]SearchResult(nil), s.results...), nil
}

func proposal(query string) modelReply {
	return modelReply{content: fmt.Sprintf(`{"query":%q}`, query), completionTokens: 1}
}

func doneProposal() modelReply {
	return modelReply{content: `{"done":true}`, completionTokens: 1}
}

func judgment(profile DiscoveryProfile, candidates ...string) modelReply {
	sources := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		sources = append(sources, map[string]any{
			"url": candidate, profile.OutputLabel: true, "reason": "profile match",
		})
	}
	body, _ := json.Marshal(map[string]any{"sources": sources})
	return modelReply{content: string(body), completionTokens: 1}
}

func testOptions(t *testing.T, name DiscoveryProfileName) DiscoverOptions {
	t.Helper()
	profile, err := NamedDiscoveryProfile(name)
	if err != nil {
		t.Fatalf("profile %q: %v", name, err)
	}
	options := DefaultDiscoverOptions(profile)
	options.MaxRounds = 1
	return options
}

func containsTruncation(metadata DiscoveryMetadata, reason TruncationReason) bool {
	for _, got := range metadata.TruncationReasons {
		if got == reason {
			return true
		}
	}
	return false
}
