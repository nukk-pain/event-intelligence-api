package eventscoutserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHandler_returns_429_with_bounded_retry_after_when_ten_minute_quota_exhausted(t *testing.T) {
	// Given
	clock := fixedTestClock()
	settings := defaultTestHandlerSettings()
	settings.clock = clock
	handler, _ := newTestHandler(t, &stubRunner{}, settings)
	for range 2 {
		request := testJSONRequest{remoteAddr: "198.51.100.50:5000", body: `{"goal":"robotics"}`}
		if recorder := serveJSON(handler, request); recorder.Code != http.StatusOK {
			t.Fatalf("setup request status = %d", recorder.Code)
		}
	}

	// When
	recorder := serveJSON(handler, testJSONRequest{remoteAddr: "198.51.100.50:5001", body: `{"goal":"robotics"}`})

	// Then
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", recorder.Code, recorder.Body.String())
	}
	retryAfter, err := strconv.Atoi(recorder.Header().Get("Retry-After"))
	if err != nil || retryAfter < 1 || retryAfter > 600 {
		t.Fatalf("Retry-After = %q, want integer in [1,600]", recorder.Header().Get("Retry-After"))
	}
	assertErrorCode(t, recorder, "rate_limited")

	clock.Advance(10 * time.Minute)
	if reset := serveJSON(handler, testJSONRequest{
		remoteAddr: "198.51.100.50:5002", body: `{"goal":"robotics"}`,
	}); reset.Code != http.StatusOK {
		t.Fatalf("status after window reset = %d, want 200", reset.Code)
	}
}

func TestHandler_returns_429_when_24_per_day_quota_exhausted(t *testing.T) {
	// Given
	clock := fixedTestClock()
	settings := defaultTestHandlerSettings()
	settings.clock = clock
	handler, _ := newTestHandler(t, &stubRunner{}, settings)
	for request := 0; request < 24; request++ {
		recorder := serveJSON(handler, testJSONRequest{remoteAddr: "198.51.100.60:5000", body: `{"goal":"robotics"}`})
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", request+1, recorder.Code)
		}
		clock.Advance(10 * time.Minute)
	}

	// When
	recorder := serveJSON(handler, testJSONRequest{remoteAddr: "198.51.100.60:5001", body: `{"goal":"robotics"}`})

	// Then
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	retryAfter, err := strconv.Atoi(recorder.Header().Get("Retry-After"))
	if err != nil || retryAfter < 1 || retryAfter > 600 {
		t.Fatalf("Retry-After = %q, want integer in [1,600]", recorder.Header().Get("Retry-After"))
	}
}

func TestHandler_sheds_third_active_job_with_503_without_queueing(t *testing.T) {
	// Given
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	runner := &stubRunner{discover: func(ctx context.Context, goal Goal) (DiscoveryOutput, error) {
		started <- struct{}{}
		select {
		case <-release:
			return DiscoveryOutput{}, nil
		case <-ctx.Done():
			return DiscoveryOutput{}, ctx.Err()
		}
	}}
	handler, _ := newTestHandler(t, runner, defaultTestHandlerSettings())
	var wait sync.WaitGroup
	wait.Add(2)
	for index := range 2 {
		go func() {
			defer wait.Done()
			serveJSON(handler, testJSONRequest{
				remoteAddr: "198.51.100." + strconv.Itoa(70+index) + ":5000", body: `{"goal":"robotics"}`,
			})
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for active discovery job")
		}
	}

	// When
	recorder := serveJSON(handler, testJSONRequest{remoteAddr: "198.51.100.90:5000", body: `{"goal":"robotics"}`})

	// Then
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "server_busy")
	if runner.callCount() != 2 {
		t.Fatalf("runner calls = %d, want 2", runner.callCount())
	}
	close(release)
	wait.Wait()
}

func TestHandler_returns_504_and_cancels_discovery_at_server_deadline(t *testing.T) {
	// Given
	cancelled := make(chan error, 1)
	runner := &stubRunner{discover: func(ctx context.Context, goal Goal) (DiscoveryOutput, error) {
		<-ctx.Done()
		cancelled <- ctx.Err()
		return DiscoveryOutput{}, ctx.Err()
	}}
	settings := defaultTestHandlerSettings()
	settings.timeout = 20 * time.Millisecond
	handler, _ := newTestHandler(t, runner, settings)

	// When
	recorder := serveJSON(handler, testJSONRequest{remoteAddr: "198.51.100.91:5000", body: `{"goal":"robotics"}`})

	// Then
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504; body=%s", recorder.Code, recorder.Body.String())
	}
	select {
	case err := <-cancelled:
		if err != context.DeadlineExceeded {
			t.Fatalf("runner context error = %v, want deadline exceeded", err)
		}
	default:
		t.Fatal("runner did not observe deadline cancellation")
	}
	assertErrorCode(t, recorder, "deadline_exceeded")
}

func TestHandler_propagates_client_cancellation_to_discovery(t *testing.T) {
	// Given
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	runner := &stubRunner{discover: func(ctx context.Context, goal Goal) (DiscoveryOutput, error) {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return DiscoveryOutput{}, ctx.Err()
	}}
	handler, _ := newTestHandler(t, runner, defaultTestHandlerSettings())
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/discover", http.NoBody).WithContext(ctx)
	req.Body = io.NopCloser(strings.NewReader(`{"goal":"robotics"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.92:5000"
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, req)
		close(done)
	}()
	<-started

	// When
	cancel()
	<-done

	// Then
	select {
	case <-cancelled:
	default:
		t.Fatal("runner did not observe client cancellation")
	}
}
