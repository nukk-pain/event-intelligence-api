package eventscoutserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

func TestHandler_returns_discovery_envelope_when_goal_is_valid(t *testing.T) {
	// Given
	runner := &stubRunner{output: DiscoveryOutput{
		Sources:           []agent.DiscoveredSource{{URL: "https://events.example/robotics", Title: "Robotics Expo", Reason: "official"}},
		TruncationReasons: []string{"depth_limit"}, ModelCalls: 3, PromptTokens: 120, CompletionTokens: 45,
	}}
	handler, _ := newTestHandler(t, runner, defaultTestHandlerSettings())

	// When
	recorder := serveJSON(handler, testJSONRequest{
		remoteAddr: "198.51.100.10:43120", body: `{"goal":"official Korean robotics event sources"}`,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response discoverResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(response.Sources) != 1 || response.Meta.Provider != "public" || response.Meta.Profile != agent.DiscoveryProfileEvents {
		t.Fatalf("response = %#v", response)
	}
	if response.Meta.ModelCalls != 3 || response.Meta.PromptTokens != 120 || response.Meta.CompletionTokens != 45 {
		t.Fatalf("model metadata = %#v", response.Meta)
	}
	if len(response.Meta.TruncationReasons) != 1 || response.Meta.TruncationReasons[0] != "depth_limit" {
		t.Fatalf("truncation reasons = %#v", response.Meta.TruncationReasons)
	}
	if runner.callCount() != 1 || runner.goals[0] != "official Korean robotics event sources" {
		t.Fatalf("runner goals = %#v", runner.goals)
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q", recorder.Header().Get("Access-Control-Allow-Origin"))
	}
	if recorder.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("credentialed CORS unexpectedly enabled")
	}
}

func TestHandler_rejects_invalid_goal_documents_with_400(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{name: "empty body", body: "", contentType: "application/json"},
		{name: "malformed JSON", body: `{"goal":`, contentType: "application/json"},
		{name: "blank goal", body: `{"goal":"  "}`, contentType: "application/json"},
		{name: "goal over 800 runes", body: `{"goal":"` + strings.Repeat("가", 801) + `"}`, contentType: "application/json"},
		{name: "body over 4 KiB", body: `{"goal":"` + strings.Repeat("a", 5000) + `"}`, contentType: "application/json"},
		{name: "unknown seed field", body: `{"goal":"x","seed":"https://example.com"}`, contentType: "application/json"},
		{name: "unknown profile field", body: `{"goal":"x","profile":"training"}`, contentType: "application/json"},
		{name: "unknown backend field", body: `{"goal":"x","backend":"local"}`, contentType: "application/json"},
		{name: "unknown budget field", body: `{"goal":"x","budget":{"calls":99}}`, contentType: "application/json"},
		{name: "case-variant Goal field", body: `{"Goal":"x"}`, contentType: "application/json"},
		{name: "duplicate goal field", body: `{"goal":"first","goal":"second"}`, contentType: "application/json"},
		{name: "trailing object", body: `{"goal":"x"}{}`, contentType: "application/json"},
		{name: "array", body: `[{"goal":"x"}]`, contentType: "application/json"},
		{name: "wrong content type", body: `{"goal":"x"}`, contentType: "text/plain"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			runner := &stubRunner{}
			handler, _ := newTestHandler(t, runner, defaultTestHandlerSettings())
			req := httptest.NewRequest(http.MethodPost, "/v1/discover", strings.NewReader(test.body))
			req.RemoteAddr = "198.51.100.20:5000"
			req.Header.Set("Content-Type", test.contentType)
			recorder := httptest.NewRecorder()

			// When
			handler.ServeHTTP(recorder, req)

			// Then
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			if runner.callCount() != 0 {
				t.Fatalf("runner calls = %d, want 0", runner.callCount())
			}
			assertErrorCode(t, recorder, "invalid_request")
		})
	}
}

func TestHandler_accepts_goal_at_800_rune_boundary(t *testing.T) {
	// Given
	runner := &stubRunner{output: DiscoveryOutput{Sources: []agent.DiscoveredSource{}}}
	handler, _ := newTestHandler(t, runner, defaultTestHandlerSettings())

	// When
	recorder := serveJSON(handler, testJSONRequest{
		remoteAddr: "198.51.100.30:5000", body: `{"goal":"` + strings.Repeat("가", 800) + `"}`,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHandler_serializes_empty_discovery_collections_as_arrays(t *testing.T) {
	// Given
	handler, _ := newTestHandler(t, &stubRunner{}, defaultTestHandlerSettings())

	// When
	recorder := serveJSON(handler, testJSONRequest{
		remoteAddr: "198.51.100.31:5000", body: `{"goal":"robotics"}`,
	})

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	for _, required := range []string{`"sources":[]`, `"truncation_reasons":[]`} {
		if !strings.Contains(recorder.Body.String(), required) {
			t.Fatalf("response is missing %s: %s", required, recorder.Body.String())
		}
	}
}

func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	var response errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body=%s", err, recorder.Body.String())
	}
	if response.Error.Code != want {
		t.Fatalf("error code = %q, want %q", response.Error.Code, want)
	}
}
