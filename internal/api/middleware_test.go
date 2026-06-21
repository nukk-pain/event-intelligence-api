package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/api"
)

// okHandler is a trivial JSON handler the middleware wraps in tests.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"ok"}`))
	})
}

// errBody mirrors the error envelope wire shape for assertions.
type errBody struct {
	Error struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		RetryAfterS *int   `json:"retry_after_s"`
	} `json:"error"`
}

// doReq issues a request directly through the handler with the given RemoteAddr
// and headers, returning the recorder.
func doReq(h http.Handler, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestETag_304OnMatch verifies the ETag middleware emits an ETag and returns 304
// with an empty body when If-None-Match matches.
func TestETag_304OnMatch(t *testing.T) {
	h := api.ETagMiddleware()(okHandler())

	rec1 := doReq(h, "203.0.113.10:1234", nil)
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header on first response")
	}
	if rec1.Code != http.StatusOK {
		t.Fatalf("first response status = %d, want 200", rec1.Code)
	}

	rec2 := doReq(h, "203.0.113.10:1234", map[string]string{"If-None-Match": etag})
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional response status = %d, want 304", rec2.Code)
	}
	if body := rec2.Body.String(); body != "" {
		t.Fatalf("304 body = %q, want empty", body)
	}
	if got := rec2.Header().Get("ETag"); got != etag {
		t.Fatalf("304 ETag = %q, want %q", got, etag)
	}
}

// quotaCfg returns a default quota config for tests.
func testConfig() api.MiddlewareConfig {
	return api.MiddlewareConfig{
		PerMinute:       60,
		PerDay:          2000,
		MaxConcurrent:   100,
		MaxResponseSize: 1 << 20,
		TrustedProxies:  []string{"127.0.0.1/32", "::1/128"},
	}
}

// TestQuota_PerMinuteBreach exhausts the 60/min burst and asserts the next
// request is rejected with 429, a Retry-After header, and the error envelope.
func TestQuota_PerMinuteBreach(t *testing.T) {
	cfg := testConfig()
	mw, err := api.NewQuotaMiddleware(cfg)
	if err != nil {
		t.Fatalf("NewQuotaMiddleware: %v", err)
	}
	h := mw(okHandler())

	ip := "203.0.113.20:5000"
	for i := 0; i < cfg.PerMinute; i++ {
		rec := doReq(h, ip, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, rec.Code)
		}
	}
	rec := doReq(h, ip, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("breach status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on 429")
	}
	if _, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil {
		t.Fatalf("Retry-After not an integer: %q", rec.Header().Get("Retry-After"))
	}

	var eb errBody
	if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if eb.Error.Code != "rate_limited" {
		t.Fatalf("error code = %q, want rate_limited", eb.Error.Code)
	}
	if eb.Error.RetryAfterS == nil || *eb.Error.RetryAfterS <= 0 {
		t.Fatalf("expected positive retry_after_s, got %v", eb.Error.RetryAfterS)
	}
}

// TestQuota_PerDayBreach uses a tiny per-day cap (with a large per-minute cap so
// the minute bucket never trips) and asserts the day bucket returns 429.
func TestQuota_PerDayBreach(t *testing.T) {
	cfg := testConfig()
	cfg.PerMinute = 10000 // never trips
	cfg.PerDay = 3
	mw, err := api.NewQuotaMiddleware(cfg)
	if err != nil {
		t.Fatalf("NewQuotaMiddleware: %v", err)
	}
	h := mw(okHandler())

	ip := "203.0.113.30:6000"
	for i := 0; i < cfg.PerDay; i++ {
		if rec := doReq(h, ip, nil); rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, rec.Code)
		}
	}
	rec := doReq(h, ip, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("day breach status = %d, want 429", rec.Code)
	}
	var eb errBody
	if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if eb.Error.Code != "rate_limited" {
		t.Fatalf("error code = %q, want rate_limited", eb.Error.Code)
	}
}

// TestQuota_SpoofedXFFFromUntrustedPeer asserts that a forged CF-Connecting-IP /
// X-Forwarded-For from an UNtrusted RemoteAddr is ignored: the bucket key is the
// real RemoteAddr, so spoofing a fresh client IP every request does NOT bypass
// the per-IP quota.
func TestQuota_SpoofedXFFFromUntrustedPeer(t *testing.T) {
	cfg := testConfig()
	mw, err := api.NewQuotaMiddleware(cfg)
	if err != nil {
		t.Fatalf("NewQuotaMiddleware: %v", err)
	}
	h := mw(okHandler())

	// Untrusted real peer; each request forges a different CF-Connecting-IP.
	realPeer := "198.51.100.50:7000"
	for i := 0; i < cfg.PerMinute; i++ {
		hdr := map[string]string{
			"CF-Connecting-IP": "10.0.0." + strconv.Itoa(i%250),
			"X-Forwarded-For":  "10.0.1." + strconv.Itoa(i%250),
		}
		if rec := doReq(h, realPeer, hdr); rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, rec.Code)
		}
	}
	// One more from the same real peer (spoofed header again) must be limited:
	// the spoof did NOT create fresh buckets.
	rec := doReq(h, realPeer, map[string]string{"CF-Connecting-IP": "10.9.9.9"})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed-header breach status = %d, want 429 (spoof must not bypass quota)", rec.Code)
	}
}

// TestQuota_TrustedProxyHonorsCFConnectingIP asserts that when the real peer IS
// a trusted proxy, the CF-Connecting-IP is honored, so two different forwarded
// client IPs get independent buckets.
func TestQuota_TrustedProxyHonorsCFConnectingIP(t *testing.T) {
	cfg := testConfig()
	cfg.PerMinute = 2
	mw, err := api.NewQuotaMiddleware(cfg)
	if err != nil {
		t.Fatalf("NewQuotaMiddleware: %v", err)
	}
	h := mw(okHandler())

	trustedPeer := "127.0.0.1:9000"

	// Client A: exhaust then breach.
	for i := 0; i < cfg.PerMinute; i++ {
		if rec := doReq(h, trustedPeer, map[string]string{"CF-Connecting-IP": "203.0.113.111"}); rec.Code != http.StatusOK {
			t.Fatalf("clientA request %d = %d, want 200", i, rec.Code)
		}
	}
	if rec := doReq(h, trustedPeer, map[string]string{"CF-Connecting-IP": "203.0.113.111"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("clientA breach = %d, want 429", rec.Code)
	}

	// Client B (different forwarded IP via the same trusted proxy) is independent.
	if rec := doReq(h, trustedPeer, map[string]string{"CF-Connecting-IP": "203.0.113.222"}); rec.Code != http.StatusOK {
		t.Fatalf("clientB first request = %d, want 200 (independent bucket)", rec.Code)
	}
}

// TestQuota_TwoRealIPsIndependent asserts two distinct real RemoteAddrs have
// independent buckets.
func TestQuota_TwoRealIPsIndependent(t *testing.T) {
	cfg := testConfig()
	cfg.PerMinute = 2
	mw, err := api.NewQuotaMiddleware(cfg)
	if err != nil {
		t.Fatalf("NewQuotaMiddleware: %v", err)
	}
	h := mw(okHandler())

	for i := 0; i < cfg.PerMinute; i++ {
		if rec := doReq(h, "203.0.113.40:1000", nil); rec.Code != http.StatusOK {
			t.Fatalf("ipA request %d = %d, want 200", i, rec.Code)
		}
	}
	if rec := doReq(h, "203.0.113.40:1000", nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("ipA breach = %d, want 429", rec.Code)
	}
	// Different real IP: fresh bucket.
	if rec := doReq(h, "203.0.113.41:1000", nil); rec.Code != http.StatusOK {
		t.Fatalf("ipB first request = %d, want 200 (independent bucket)", rec.Code)
	}
}

// TestQuota_RateLimitHeaders asserts X-RateLimit-Remaining/Reset are emitted.
func TestQuota_RateLimitHeaders(t *testing.T) {
	cfg := testConfig()
	mw, err := api.NewQuotaMiddleware(cfg)
	if err != nil {
		t.Fatalf("NewQuotaMiddleware: %v", err)
	}
	h := mw(okHandler())
	rec := doReq(h, "203.0.113.50:2000", nil)
	if rec.Header().Get("X-RateLimit-Remaining") == "" {
		t.Fatalf("expected X-RateLimit-Remaining header")
	}
	if rec.Header().Get("X-RateLimit-Reset") == "" {
		t.Fatalf("expected X-RateLimit-Reset header")
	}
}

// TestWriteError_EnvelopeShape asserts WriteError maps codes to statuses and the
// retry_after_s field is present (null unless rate_limited).
func TestWriteError_EnvelopeShape(t *testing.T) {
	cases := []struct {
		code       string
		wantStatus int
	}{
		{"not_found", http.StatusNotFound},
		{"bad_request", http.StatusBadRequest},
		{"invalid_cursor", http.StatusBadRequest},
		{"rate_limited", http.StatusTooManyRequests},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		api.WriteError(rec, tc.code, "msg for "+tc.code)
		if rec.Code != tc.wantStatus {
			t.Fatalf("code %q status = %d, want %d", tc.code, rec.Code, tc.wantStatus)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("code %q Content-Type = %q, want application/json", tc.code, ct)
		}
		var eb errBody
		if err := json.Unmarshal(rec.Body.Bytes(), &eb); err != nil {
			t.Fatalf("code %q decode: %v", tc.code, err)
		}
		if eb.Error.Code != tc.code {
			t.Fatalf("error code = %q, want %q", eb.Error.Code, tc.code)
		}
		if eb.Error.Message == "" {
			t.Fatalf("code %q empty message", tc.code)
		}
		// retry_after_s must be present in the JSON for every code (null for
		// non-rate-limited). Re-check the raw payload contains the key.
		if !strings.Contains(rec.Body.String(), "retry_after_s") {
			t.Fatalf("code %q missing retry_after_s key in body: %s", tc.code, rec.Body.String())
		}
	}
}

// TestConcurrencyLimiter asserts the bounded semaphore rejects over-cap
// concurrent requests with 429-style backpressure but allows up to the cap.
func TestConcurrencyLimiter(t *testing.T) {
	cfg := testConfig()
	cfg.MaxConcurrent = 1
	mw := api.NewConcurrencyLimiter(cfg.MaxConcurrent)

	release := make(chan struct{})
	entered := make(chan struct{})
	blocking := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	// Occupy the single slot.
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
		req.RemoteAddr = "203.0.113.60:1000"
		blocking.ServeHTTP(httptest.NewRecorder(), req)
	}()
	<-entered

	// Second concurrent request must be shed.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.RemoteAddr = "203.0.113.61:1000"
	blocking.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-cap status = %d, want 503 or 429", rec.Code)
	}
	close(release)
}

// TestResponseSizeCap asserts the response-size cap truncates/aborts a handler
// that writes more than the configured maximum.
func TestResponseSizeCap(t *testing.T) {
	cap := 16
	mw := api.NewResponseSizeCap(int64(cap))
	big := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", 1000)))
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	big.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if len(body) > cap {
		t.Fatalf("response body = %d bytes, want <= cap %d", len(body), cap)
	}
}
