package eventscoutserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/api"
)

func TestHandler_serves_credentialless_CORS_preflight(t *testing.T) {
	// Given
	handler, _ := newTestHandler(t, &stubRunner{}, defaultTestHandlerSettings())
	req := httptest.NewRequest(http.MethodOptions, "/v1/discover", nil)
	recorder := httptest.NewRecorder()

	// When
	handler.ServeHTTP(recorder, req)

	// Then
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" ||
		recorder.Header().Get("Access-Control-Allow-Methods") != "POST, OPTIONS" ||
		recorder.Header().Get("Access-Control-Allow-Headers") != "Content-Type" {
		t.Fatalf("CORS headers = %#v", recorder.Header())
	}
	if recorder.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("credentialed CORS unexpectedly enabled")
	}
}

func TestHandler_exposes_only_explicit_routes_and_methods(t *testing.T) {
	// Given
	handler, _ := newTestHandler(t, &stubRunner{}, defaultTestHandlerSettings())
	tests := []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodGet, path: "/healthz", status: http.StatusOK},
		{method: http.MethodGet, path: "/readyz", status: http.StatusOK},
		{method: http.MethodGet, path: "/v1/discover", status: http.StatusMethodNotAllowed},
		{method: http.MethodPost, path: "/v1/discover/", status: http.StatusNotFound},
		{method: http.MethodPost, path: "/v1/other", status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, nil)
			recorder := httptest.NewRecorder()

			// When
			handler.ServeHTTP(recorder, req)

			// Then
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.status, recorder.Body.String())
			}
		})
	}
}

func TestReadAPIRouter_does_not_expose_discovery_route(t *testing.T) {
	// Given
	handler, err := api.Router(nil, api.MiddlewareConfig{
		PerMinute: 100, PerDay: 1000, MaxConcurrent: 10, MaxResponseSize: 1 << 20,
		TrustedProxies: []string{"127.0.0.1/32"}, IdleTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("api.Router() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/discover", nil)
	req.RemoteAddr = "198.51.100.40:5000"
	recorder := httptest.NewRecorder()

	// When
	handler.ServeHTTP(recorder, req)

	// Then
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("read API status = %d, want 404", recorder.Code)
	}
}
