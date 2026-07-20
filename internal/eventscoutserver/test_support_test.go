package eventscoutserver

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type stubRunner struct {
	mu       sync.Mutex
	output   DiscoveryOutput
	err      error
	discover func(context.Context, Goal) (DiscoveryOutput, error)
	goals    []string
}

func (runner *stubRunner) Discover(ctx context.Context, goal Goal) (DiscoveryOutput, error) {
	runner.mu.Lock()
	runner.goals = append(runner.goals, goal.String())
	fn := runner.discover
	output := runner.output
	err := runner.err
	runner.mu.Unlock()
	if fn != nil {
		return fn(ctx, goal)
	}
	return output, err
}

func (runner *stubRunner) callCount() int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return len(runner.goals)
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

type testHandlerSettings struct {
	clock             Clock
	trustedProxyCIDRs []string
	timeout           time.Duration
}

func defaultTestHandlerSettings() testHandlerSettings {
	return testHandlerSettings{clock: fixedTestClock(), timeout: time.Minute}
}

func newTestHandler(t *testing.T, runner DiscoveryRunner, settings testHandlerSettings) (http.Handler, *bytes.Buffer) {
	t.Helper()
	options := DefaultHandlerOptions()
	options.RequestTimeout = settings.timeout
	prefixes, err := ParseTrustedProxyCIDRs(settings.trustedProxyCIDRs)
	if err != nil {
		t.Fatalf("ParseTrustedProxyCIDRs() error = %v", err)
	}
	options.TrustedProxies = prefixes
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	handler, err := NewHandler(options, HandlerDependencies{Runner: runner, Clock: settings.clock, Logger: logger})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler, logs
}

type testJSONRequest struct {
	remoteAddr   string
	forwardedFor string
	body         string
}

func serveJSON(handler http.Handler, request testJSONRequest) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/discover", bytes.NewBufferString(request.body))
	req.RemoteAddr = request.remoteAddr
	req.Header.Set("Content-Type", "application/json")
	if request.forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", request.forwardedFor)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func fixedTestClock() *testClock {
	return &testClock{now: time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)}
}
