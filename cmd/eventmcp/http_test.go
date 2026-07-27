package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

func mcpHandler(t *testing.T) http.Handler {
	t.Helper()
	// Route through the same mux serveHTTP builds, minus the listener, by
	// re-declaring the two routes it registers. Keeping this tiny beats
	// refactoring serveHTTP for injectability while it has exactly two routes.
	t.Setenv("EVENTSINTEL_SOLAR_API_KEY", "test-key")
	// ask_events must not reach the real API from a test: point the backend at
	// a local fake and back the query with an in-process fixture.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	t.Cleanup(fake.Close)
	t.Setenv("EVENTSINTEL_SOLAR_BASE_URL", fake.URL)
	t.Setenv("EVENTSINTEL_LOCAL_BASE_URL", "off")
	query = func(agent.Filter) ([]agent.Event, error) { return nil, nil }
	refDateFn = func() string { return "2026-07-27" }
	quota := &askQuota{clients: make(map[string]*askWindows)}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		mcpPost(w, r, quota)
	})
	return mux
}

func postJSON(t *testing.T, h http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	// The deployment shape is Caddy on the same host, so the peer is loopback
	// and the client identity arrives in X-Forwarded-For.
	req.RemoteAddr = "127.0.0.1:9999"
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHTTP_initializeAndToolsListAreStateless(t *testing.T) {
	// Given
	h := mcpHandler(t)

	// When
	init := postJSON(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
	list := postJSON(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)

	// Then
	if init.Code != http.StatusOK || list.Code != http.StatusOK {
		t.Fatalf("status = %d/%d, want 200/200 with no session requirement", init.Code, list.Code)
	}
	if init.Header().Get("Mcp-Session-Id") != "" {
		t.Fatal("a session id was issued; the stateless server must not require one")
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range resp.Result.Tools {
		names[tool.Name] = true
	}
	if !names["search_events"] || !names["ask_events"] {
		t.Fatalf("tools = %v, want both event tools advertised", names)
	}
}

func TestHTTP_notificationAnswers202(t *testing.T) {
	h := mcpHandler(t)
	rec := postJSON(t, h, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 for a notification", rec.Code)
	}
}

func TestHTTP_getIsRejected(t *testing.T) {
	h := mcpHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 for the unsupported stream transport", rec.Code)
	}
}

func TestHTTP_oversizedBodyIsRejected(t *testing.T) {
	h := mcpHandler(t)
	big := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"` +
		strings.Repeat("x", httpMaxBody) + `"}}`
	rec := postJSON(t, h, big, nil)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

// Only ask_events spends the operator's Solar budget, so only it may draw down
// the quota — and clients must not share one bucket.
func TestHTTP_quotaBindsOnlyAskEventsPerClient(t *testing.T) {
	// Given
	h := mcpHandler(t)
	ask := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"ask_events","arguments":{"question":"q"}}}`
	alice := map[string]string{"X-Forwarded-For": "203.0.113.5"}
	bob := map[string]string{"X-Forwarded-For": "203.0.113.9"}

	// When: alice exhausts her ten-minute window.
	var last *httptest.ResponseRecorder
	for range askPerTenMinutes + 1 {
		last = postJSON(t, h, ask, alice)
	}

	// Then
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after the window is spent", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}
	if got := postJSON(t, h, ask, bob).Code; got == http.StatusTooManyRequests {
		t.Fatal("a different client was throttled by alice's quota")
	}
	free := `{"jsonrpc":"2.0","id":10,"method":"tools/list"}`
	if got := postJSON(t, h, free, alice).Code; got != http.StatusOK {
		t.Fatalf("tools/list = %d, want the LLM-free method unaffected by the quota", got)
	}
}

func TestServeHTTP_refusesToStartWithoutKey(t *testing.T) {
	t.Setenv("EVENTSINTEL_SOLAR_API_KEY", "")
	if err := serveHTTP("127.0.0.1:0"); err == nil {
		t.Fatal("serveHTTP() error = nil, want a missing-key refusal")
	}
}

func TestClientKey_trustsForwardedOnlyFromLoopback(t *testing.T) {
	direct := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(nil))
	direct.RemoteAddr = "198.51.100.7:4444"
	direct.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := clientKey(direct); got != "198.51.100.7" {
		t.Fatalf("clientKey = %q, want the direct peer, not its forged header", got)
	}
	proxied := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(nil))
	proxied.RemoteAddr = "127.0.0.1:5555"
	proxied.Header.Set("X-Forwarded-For", "203.0.113.5")
	if got := clientKey(proxied); got != "203.0.113.5" {
		t.Fatalf("clientKey = %q, want the forwarded client behind the local proxy", got)
	}
}
