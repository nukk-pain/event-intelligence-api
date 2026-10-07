package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/smpain/event-intelligence-api/internal/clientidentity"
)

// jsonMarshal serializes v, falling back to a minimal hardcoded error envelope
// if marshaling fails (which it cannot for the fixed envelope types, but keeps
// the writers infallible).
func jsonMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":{"code":"internal","message":"encoding failed","retry_after_s":null}}`)
	}
	return b
}

// middleware.go implements the cache-first delivery guards for the read API:
//   - ETag / Last-Modified conditional GET (304 on If-None-Match match)
//   - the public-IP keyed two-counter quota (60/min token bucket + 2000/day
//     rolling bucket) with loopback-only canonical proxy identity
//   - a global bounded-concurrency limiter and a hard response-size cap
//   - WriteError: the single error-envelope writer used everywhere
//
// All middlewares are plain func(http.Handler) http.Handler so cmd serve can
// chain them around the chi router.

// ---------------------------------------------------------------------------
// Error envelope
// ---------------------------------------------------------------------------

// retryEnvelope is the full error wire shape including retry_after_s. It
// supersedes events.go's minimal errorEnvelope for the middleware path; the two
// are JSON-compatible (retry_after_s defaults to null).
type retryEnvelope struct {
	Error retryErrorBody `json:"error"`
}

type retryErrorBody struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	RetryAfterS *int   `json:"retry_after_s"`
}

// errorStatus maps a stable error code to its HTTP status. Unknown codes map to
// 500 so a typo surfaces loudly rather than silently 200-ing.
func errorStatus(code string) int {
	switch code {
	case "not_found":
		return http.StatusNotFound
	case "bad_request", "invalid_cursor":
		return http.StatusBadRequest
	case "rate_limited":
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// WriteError writes the canonical error envelope for a stable error code. It is
// the single entry point handlers and middleware use for non-2xx JSON, so the
// wire shape (including the always-present retry_after_s key) stays uniform.
// retry_after_s is null for every code except rate_limited (use
// WriteRateLimited to supply a value).
func WriteError(w http.ResponseWriter, code, msg string) {
	writeErrorWithRetry(w, code, msg, nil)
}

// WriteRateLimited writes a rate_limited error with a populated Retry-After
// header and retry_after_s body field.
func WriteRateLimited(w http.ResponseWriter, msg string, retryAfterS int) {
	if retryAfterS < 1 {
		retryAfterS = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterS))
	writeErrorWithRetry(w, "rate_limited", msg, &retryAfterS)
}

func writeErrorWithRetry(w http.ResponseWriter, code, msg string, retryAfterS *int) {
	status := errorStatus(code)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// Encode manually to guarantee the retry_after_s key is always emitted
	// (even as null) without pulling json.Encoder's trailing newline semantics
	// into the contract.
	body := retryEnvelope{Error: retryErrorBody{Code: code, Message: msg, RetryAfterS: retryAfterS}}
	enc := jsonMarshal(body)
	_, _ = w.Write(enc)
}

// ---------------------------------------------------------------------------
// ETag / Last-Modified conditional GET
// ---------------------------------------------------------------------------

// ETagMiddleware buffers the handler's response and derives a weak-free ETag
// from a SHA-256 of the body. When the client's If-None-Match matches, it
// returns 304 with an empty body and the same ETag; otherwise it forwards the
// buffered body. It also stamps Last-Modified with the response time when the
// handler did not already set one.
//
// Per-event handlers may set their own strong ETag (from content_hash) before
// writing; if an ETag header is already present when buffering completes, it is
// preserved instead of being recomputed.
func ETagMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &bufferRecorder{header: http.Header{}, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			// Only apply conditional semantics to cacheable success responses.
			etag := rec.header.Get("ETag")
			if etag == "" && rec.status == http.StatusOK {
				sum := sha256.Sum256(rec.body)
				etag = `"` + hex.EncodeToString(sum[:]) + `"`
			}

			// Copy buffered headers onto the real writer.
			for k, vs := range rec.header {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			if etag != "" {
				w.Header().Set("ETag", etag)
			}
			if rec.header.Get("Last-Modified") == "" && rec.status == http.StatusOK {
				w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			}

			if etag != "" && rec.status == http.StatusOK && ifNoneMatch(r.Header.Get("If-None-Match"), etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}

			w.WriteHeader(rec.status)
			_, _ = w.Write(rec.body)
		})
	}
}

// ifNoneMatch reports whether the If-None-Match header value matches etag. It
// honors the "*" wildcard and comma-separated lists, and compares ignoring the
// weak ("W/") prefix.
func ifNoneMatch(inm, etag string) bool {
	if inm == "" {
		return false
	}
	if strings.TrimSpace(inm) == "*" {
		return true
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, tok := range strings.Split(inm, ",") {
		tok = strings.TrimSpace(tok)
		if strings.TrimPrefix(tok, "W/") == want {
			return true
		}
	}
	return false
}

// bufferRecorder captures a handler's status, headers, and body so the ETag
// middleware can hash the body and decide on a 304.
type bufferRecorder struct {
	header      http.Header
	status      int
	body        []byte
	wroteHeader bool
}

func (b *bufferRecorder) Header() http.Header { return b.header }

func (b *bufferRecorder) WriteHeader(status int) {
	if b.wroteHeader {
		return
	}
	b.status = status
	b.wroteHeader = true
}

func (b *bufferRecorder) Write(p []byte) (int, error) {
	if !b.wroteHeader {
		b.WriteHeader(http.StatusOK)
	}
	b.body = append(b.body, p...)
	return len(p), nil
}

// ---------------------------------------------------------------------------
// Quota: 60/min token bucket + 2000/day rolling bucket, per public IP
// ---------------------------------------------------------------------------

// MiddlewareConfig configures the quota and resource guards. The zero value is
// not valid; use the per-field defaults documented below.
type MiddlewareConfig struct {
	// PerMinute is the sustained+burst request budget per client per minute
	// (token bucket; refills at PerMinute/60 tokens per second with a burst of
	// PerMinute). Default 60.
	PerMinute int
	// PerDay is the rolling 24h request budget per client. Default 2000.
	PerDay int
	// MaxConcurrent bounds in-flight requests across all clients. Default 100.
	MaxConcurrent int
	// MaxResponseSize caps the bytes a handler may write. Default 1 MiB.
	MaxResponseSize int64
	// MaxTrackedClients bounds the identity map. Default 10000. A full map
	// rejects new identities rather than forgetting an active daily budget.
	MaxTrackedClients int
	// IdleTTL is the cleanup interval, never the lifetime of active quota.
	IdleTTL time.Duration
}

func (c MiddlewareConfig) withDefaults() MiddlewareConfig {
	if c.PerMinute <= 0 {
		c.PerMinute = 60
	}
	if c.PerDay <= 0 {
		c.PerDay = 2000
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 100
	}
	if c.MaxResponseSize <= 0 {
		c.MaxResponseSize = 1 << 20
	}
	if c.IdleTTL <= 0 {
		c.IdleTTL = time.Hour
	}
	if c.MaxTrackedClients <= 0 {
		c.MaxTrackedClients = 10000
	}
	return c
}

// clientBuckets holds one client's two quota counters plus a last-seen stamp
// used by the idle evictor.
type clientBuckets struct {
	minute   *rate.Limiter
	day      *dayBucket
	lastSeen time.Time
}

// dayBucket is a fixed-window-per-rolling-day counter: it counts requests within
// a 24h window anchored at the first request of the window, then resets.
type dayBucket struct {
	mu          sync.Mutex
	count       int
	limit       int
	windowStart time.Time
}

func (d *dayBucket) allow(now time.Time) (ok bool, resetIn time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if now.Sub(d.windowStart) >= 24*time.Hour {
		d.windowStart = now
		d.count = 0
	}
	resetIn = 24*time.Hour - now.Sub(d.windowStart)
	if d.count >= d.limit {
		return false, resetIn
	}
	d.count++
	return true, resetIn
}

// quotaMiddleware is the stateful per-IP limiter.
type quotaMiddleware struct {
	cfg       MiddlewareConfig
	nextSweep time.Time

	mu      sync.Mutex
	clients map[string]*clientBuckets
}

// NewQuotaMiddleware builds the bounded per-IP two-counter quota guard.
func NewQuotaMiddleware(cfg MiddlewareConfig) (func(http.Handler) http.Handler, error) {
	q := &quotaMiddleware{cfg: cfg.withDefaults(), clients: map[string]*clientBuckets{}}
	return q.handler, nil
}

func (q *quotaMiddleware) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := q.clientKey(r)
		now := time.Now()
		cb := q.bucketFor(key, now)
		if cb == nil {
			WriteRateLimited(w, "client quota capacity reached", 60)
			return
		}

		// Day bucket first (cheaper to reject, and the harder cap).
		dayOK, dayReset := cb.day.allow(now)
		if !dayOK {
			emitRateLimitHeaders(w, 0, dayReset)
			WriteRateLimited(w, "daily request quota exceeded", retrySeconds(dayReset))
			return
		}

		// Minute token bucket.
		res := cb.minute.ReserveN(now, 1)
		if !res.OK() || res.DelayFrom(now) > 0 {
			// No token available now: do NOT consume it; reject.
			res.CancelAt(now)
			delay := res.DelayFrom(now)
			if delay <= 0 {
				delay = time.Second
			}
			emitRateLimitHeaders(w, 0, delay)
			WriteRateLimited(w, "rate limit exceeded", retrySeconds(delay))
			return
		}

		emitRateLimitHeaders(w, int(cb.minute.TokensAt(now)), time.Until(now.Add(time.Minute)))
		next.ServeHTTP(w, r)
	})
}

// bucketFor returns (creating if needed) the client's buckets and refreshes its
// last-seen stamp.
func (q *quotaMiddleware) bucketFor(key string, now time.Time) *clientBuckets {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !now.Before(q.nextSweep) {
		q.evictExpired(now)
		q.nextSweep = now.Add(q.cfg.IdleTTL)
	}
	cb, ok := q.clients[key]
	if !ok {
		if len(q.clients) >= q.cfg.MaxTrackedClients {
			return nil
		}
		cb = &clientBuckets{
			minute: rate.NewLimiter(rate.Limit(float64(q.cfg.PerMinute)/60.0), q.cfg.PerMinute),
			day:    &dayBucket{limit: q.cfg.PerDay, windowStart: now},
		}
		q.clients[key] = cb
	}
	cb.lastSeen = now
	return cb
}

// evictExpired is called under q.mu. Both windows must be expired/refilled;
// inactivity alone must never replenish the client's daily quota.
func (q *quotaMiddleware) evictExpired(now time.Time) {
	for k, cb := range q.clients {
		cb.day.mu.Lock()
		expired := !now.Before(cb.day.windowStart.Add(24 * time.Hour))
		cb.day.mu.Unlock()
		if expired && cb.minute.TokensAt(now) >= float64(q.cfg.PerMinute) && !now.Before(cb.lastSeen.Add(q.cfg.IdleTTL)) {
			delete(q.clients, k)
		}
	}
}

func (q *quotaMiddleware) clientKey(r *http.Request) string { return clientidentity.Key(r) }

// retrySeconds rounds a delay UP to whole seconds, with a floor of 1.
func retrySeconds(d time.Duration) int {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}
	return s
}

// emitRateLimitHeaders sets the informational rate-limit headers. remaining is
// the minute-bucket tokens left (clamped at 0); reset is the duration until the
// relevant window refills, surfaced as an epoch-second reset.
func emitRateLimitHeaders(w http.ResponseWriter, remaining int, reset time.Duration) {
	if remaining < 0 {
		remaining = 0
	}
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(reset).Unix(), 10))
}

// ---------------------------------------------------------------------------
// Concurrency limiter + response size cap
// ---------------------------------------------------------------------------

// NewConcurrencyLimiter bounds in-flight requests with a buffered-channel
// semaphore. When the cap is reached it sheds load with 503 + Retry-After
// rather than queueing unboundedly. max <= 0 disables the limiter.
func NewConcurrencyLimiter(max int) func(http.Handler) http.Handler {
	if max <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	sem := make(chan struct{}, max)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				ra := 1
				_, _ = w.Write(jsonMarshal(retryEnvelope{Error: retryErrorBody{
					Code: "rate_limited", Message: "server at capacity", RetryAfterS: &ra,
				}}))
			}
		})
	}
}

// NewResponseSizeCap truncates a handler's response at max bytes, protecting
// against a runaway serialization. Bytes beyond the cap are silently dropped
// (the handler's Write returns the full count so it keeps running, but nothing
// past the cap reaches the client). max <= 0 disables the cap.
func NewResponseSizeCap(max int64) func(http.Handler) http.Handler {
	if max <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cw := &cappedWriter{ResponseWriter: w, remaining: max}
			next.ServeHTTP(cw, r)
		})
	}
}

// cappedWriter is an http.ResponseWriter that drops bytes past a byte budget.
type cappedWriter struct {
	http.ResponseWriter
	remaining int64
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.remaining <= 0 {
		return len(p), nil
	}
	if int64(len(p)) > c.remaining {
		_, err := c.ResponseWriter.Write(p[:c.remaining])
		c.remaining = 0
		if err != nil {
			return 0, err
		}
		return len(p), nil
	}
	n, err := c.ResponseWriter.Write(p)
	c.remaining -= int64(n)
	return n, err
}
