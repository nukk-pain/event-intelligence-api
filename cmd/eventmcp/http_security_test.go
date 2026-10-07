package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

const askRequest = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_events","arguments":{"question":"q"}}}`
const searchRequest = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_events","arguments":{}}}`

func fakeProvider(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	t.Setenv("EVENTSINTEL_LOCAL_BASE_URL", "off")
	t.Setenv("EVENTSINTEL_SOLAR_API_KEY", "test-key")
	t.Setenv("EVENTSINTEL_SOLAR_BASE_URL", s.URL)
	refDateFn = func() string { return "2026-10-07" }
}

func TestProvider200Then201AndFailureNoRefund(t *testing.T) {
	var calls atomic.Int32
	fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"error":{"message":"fake failure"}}`)
	})
	query = func(context.Context, agent.Filter) ([]agent.Event, error) { return nil, nil }
	b := testBudget(t, 200)
	for range 200 {
		if _, rerr, _ := askEvents(context.Background(), "q", b); rerr != nil {
			t.Fatal("reservation failed early")
		}
	}
	_, rerr, _ := askEvents(context.Background(), "q", b)
	if rerr == nil || rerr.status != http.StatusTooManyRequests || calls.Load() != 200 {
		t.Fatalf("provider calls = %d, error = %+v", calls.Load(), rerr)
	}
}

func TestStorageFailureAndRedirectNeverSendUnreservedProviderCall(t *testing.T) {
	var calls atomic.Int32
	fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "/second")
		w.WriteHeader(http.StatusTemporaryRedirect)
		fmt.Fprint(w, `{"error":{}}`)
	})
	query = func(context.Context, agent.Filter) ([]agent.Event, error) { return nil, nil }
	b := testBudget(t, 200)
	if _, rerr, _ := askEvents(context.Background(), "q", b); rerr != nil {
		t.Fatal(rerr)
	}
	if calls.Load() != 1 || b.count != 1 {
		t.Fatal("provider redirect bypassed reservation accounting")
	}
	b.file.Close()
	h := newHTTPHandler(testToolQuota(), b)
	r := postJSON(t, h, askRequest, nil)
	if r.Code != http.StatusServiceUnavailable || r.Header().Get("Retry-After") == "" || calls.Load() != 1 {
		t.Fatal("storage failure failed open")
	}
}

func TestHTTPUpstreamQuotaIsToolError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer s.Close()
	query = func(ctx context.Context, f agent.Filter) ([]agent.Event, error) {
		return agent.QueryEventsBounded(ctx, s.URL, f, 200, time.Second, 2)
	}
	r := postJSON(t, newHTTPHandler(testToolQuota(), testBudget(t, 200)), searchRequest, nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"isError":true`) || !strings.Contains(r.Body.String(), "429") {
		t.Fatal("upstream quota was reported as a successful tool result")
	}
}

func TestHTTPSearchLimitedAndLLMBudgetUnchanged(t *testing.T) {
	h := mcpHandler(t)
	client := map[string]string{"X-Real-Client-IP": "203.0.113.7"}
	for range toolsPerMinute {
		if r := postJSON(t, h, searchRequest, client); r.Code != 200 {
			t.Fatal(r.Code)
		}
	}
	r := postJSON(t, h, searchRequest, client)
	if r.Code != 429 || r.Header().Get("Retry-After") == "" || !strings.Contains(r.Body.String(), `"error"`) {
		t.Fatal("search overload contract failed")
	}
	if r := postJSON(t, h, searchRequest, map[string]string{"X-Real-Client-IP": "203.0.113.8"}); r.Code != 200 {
		t.Fatal("other client throttled")
	}
	// Search cannot even invoke the provider reservation path.
	b := testBudget(t, 0)
	r = postJSON(t, newHTTPHandler(testToolQuota(), b), searchRequest, nil)
	if r.Code != 200 || b.count != 0 {
		t.Fatal("search consumed LLM budget")
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation signal missing")
	}
}

func TestHTTPCancellationReachesProviderAndReadAPI(t *testing.T) {
	for _, tool := range []string{"ask", "search"} {
		for _, reason := range []string{"deadline", "disconnect"} {
			t.Run(tool+"/"+reason, func(t *testing.T) {
				started, cancelled := make(chan struct{}), make(chan struct{})
				slow := func(w http.ResponseWriter, r *http.Request) {
					io.Copy(io.Discard, r.Body)
					close(started)
					<-r.Context().Done()
					close(cancelled)
				}
				body := askRequest
				if tool == "ask" {
					fakeProvider(t, slow)
					query = func(context.Context, agent.Filter) ([]agent.Event, error) {
						t.Error("query ran after provider cancellation")
						return nil, nil
					}
				} else {
					body = searchRequest
					s := httptest.NewServer(http.HandlerFunc(slow))
					t.Cleanup(s.Close)
					query = func(ctx context.Context, f agent.Filter) ([]agent.Event, error) {
						return agent.QueryEventsBounded(ctx, s.URL, f, 200, time.Second, 2)
					}
				}
				b := testBudget(t, 200)
				q := testToolQuota()
				h := newHTTPHandler(q, b)
				done := make(chan struct{})
				if reason == "deadline" {
					ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
					defer cancel()
					r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body)).WithContext(ctx)
					go func() { h.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
				} else {
					s := httptest.NewServer(h)
					t.Cleanup(s.Close)
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					r, _ := http.NewRequestWithContext(ctx, "POST", s.URL+"/mcp", strings.NewReader(body))
					go func() {
						resp, _ := http.DefaultClient.Do(r)
						if resp != nil {
							resp.Body.Close()
						}
						close(done)
					}()
					waitSignal(t, started)
					cancel()
				}
				waitSignal(t, cancelled)
				waitSignal(t, done)
				// The client Do return can precede the server's deferred release.
				for deadline := time.Now().Add(time.Second); ; {
					q.mu.Lock()
					flight := q.inFlight
					q.mu.Unlock()
					if flight == 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("slot leak")
					}
					time.Sleep(time.Millisecond)
				}
				if tool == "ask" && b.count != 1 {
					t.Fatal("cancelled provider reservation refunded")
				}
			})
		}
	}
}

func TestHTTPDeadlineAndStatelessNotificationIsolation(t *testing.T) {
	q := testToolQuota()
	q.cfg.concurrent = 1
	q.cfg.clientConcurrent = 1
	entered := make(chan struct{})
	finish := make(chan struct{})
	query = func(ctx context.Context, _ agent.Filter) ([]agent.Event, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > httpCallTimeout {
			t.Error("30s deadline absent")
		}
		close(entered)
		<-finish
		return nil, nil
	}
	h := newHTTPHandler(q, testBudget(t, 200))
	done := make(chan struct{})
	go func() { postJSON(t, h, searchRequest, nil); close(done) }()
	waitSignal(t, entered)
	// Cancellation does not wait for a tool slot or target another client's ID.
	r := postJSON(t, h, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`, map[string]string{"X-Real-Client-IP": "203.0.113.99"})
	if r.Code != 202 {
		t.Fatal(r.Code)
	}
	if r := postJSON(t, h, searchRequest, nil); r.Code != 429 {
		t.Fatal("active request was cancelled or capacity exceeded")
	}
	close(finish)
	waitSignal(t, done)
	if q.inFlight != 0 {
		t.Fatal("completed call retained slot")
	}
}

func TestHTTPDuplicateIDsHaveIndependentContexts(t *testing.T) {
	q := testToolQuota()
	entered := make(chan struct{}, 2)
	query = func(ctx context.Context, _ agent.Filter) ([]agent.Event, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	h := newHTTPHandler(q, testBudget(t, 200))
	a, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	b, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	doneA, doneB := make(chan struct{}), make(chan struct{})
	start := func(ctx context.Context, done chan struct{}) {
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(searchRequest)).WithContext(ctx)
		r.RemoteAddr = "127.0.0.1:1234"
		go func() { h.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	}
	start(a, doneA)
	start(b, doneB)
	waitSignal(t, entered)
	waitSignal(t, entered)
	for _, id := range []int{1, 999} {
		r := postJSON(t, h, fmt.Sprintf(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":%d}}`, id), nil)
		if r.Code != 202 {
			t.Fatal("stateless notification did not get accepted")
		}
	}
	cancelA()
	waitSignal(t, doneA)
	select {
	case <-doneB:
		t.Fatal("duplicate ID coupled independent requests")
	default:
	}
	cancelB()
	waitSignal(t, doneB)
	if q.inFlight != 0 {
		t.Fatal("duplicate request context leaked slots")
	}
}
