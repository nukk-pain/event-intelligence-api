package eventscoutserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestHandler_sanitizes_internal_errors_and_logs_no_goal_or_secret(t *testing.T) {
	// Given
	goal := "private-goal-marker"
	secret := "operator-secret-marker"
	runner := &stubRunner{err: errors.New("upstream rejected " + secret + " while processing " + goal)}
	handler, logs := newTestHandler(t, runner, defaultTestHandlerSettings())

	// When
	recorder := serveJSON(handler, testJSONRequest{
		remoteAddr: "198.51.100.100:5000", body: `{"goal":"` + goal + `"}`,
	})

	// Then
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "internal_error")
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("response is missing X-Request-ID")
	}
	combined := recorder.Body.String() + logs.String()
	if strings.Contains(combined, goal) || strings.Contains(combined, secret) || strings.Contains(combined, "upstream rejected") {
		t.Fatalf("response/log output leaked sensitive marker: %s", combined)
	}
	for _, required := range []string{`"request_id"`, `"status":500`, `"duration_ms"`, `"active_limit":2`, `"ten_minute_limit":2`, `"day_limit":24`} {
		if !strings.Contains(logs.String(), required) {
			t.Fatalf("structured log missing %s: %s", required, logs.String())
		}
	}
	if strings.Contains(logs.String(), `"request_id":""`) {
		t.Fatalf("structured log has empty request ID: %s", logs.String())
	}
}

func TestHandler_recovers_panic_with_sanitized_500(t *testing.T) {
	// Given
	panicMarker := "panic-secret-marker"
	runner := &stubRunner{discover: func(context.Context, Goal) (DiscoveryOutput, error) {
		panic(panicMarker)
	}}
	handler, logs := newTestHandler(t, runner, defaultTestHandlerSettings())

	// When
	recorder := serveJSON(handler, testJSONRequest{
		remoteAddr: "198.51.100.101:5000", body: `{"goal":"robotics"}`,
	})

	// Then
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	assertErrorCode(t, recorder, "internal_error")
	if strings.Contains(recorder.Body.String()+logs.String(), panicMarker) {
		t.Fatal("panic value leaked")
	}
}
