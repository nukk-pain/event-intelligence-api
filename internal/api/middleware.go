package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
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
//     rolling bucket) with trusted-proxy CF-Connecting-IP resolution
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
	// TrustedProxies is the CIDR set whose RemoteAddr is allowed to assert the
	// real client via CF-Connecting-IP. Default: Cloudflare ranges + localhost.
	TrustedProxies []string
	// IdleTTL evicts a client's buckets after this much inactivity. Default 1h.
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
	if len(c.TrustedProxies) == 0 {
		c.TrustedProxies = DefaultTrustedProxies()
	}
	return c
}

// DefaultTrustedProxies returns Cloudflare's published IPv4/IPv6 egress ranges
// plus loopback. Only requests whose RemoteAddr falls in this set may assert a
// client IP via CF-Connecting-IP.
func DefaultTrustedProxies() []string {
	return []string{
		"127.0.0.1/32", "::1/128",
		// Cloudflare IPv4 (https://www.cloudflare.com/ips-v4)
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		// Cloudflare IPv6 (https://www.cloudflare.com/ips-v6)
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	}
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
	cfg     MiddlewareConfig
	trusted []*net.IPNet

	mu      sync.Mutex
	clients map[string]*clientBuckets
}

// NewQuotaMiddleware builds the per-IP two-counter quota guard. It returns an
// error if a configured trusted-proxy CIDR is malformed. The returned value is
// a func(http.Handler) http.Handler suitable for chaining.
func NewQuotaMiddleware(cfg MiddlewareConfig) (func(http.Handler) http.Handler, error) {
	cfg = cfg.withDefaults()
	nets := make([]*net.IPNet, 0, len(cfg.TrustedProxies))
	for _, c := range cfg.TrustedProxies {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy CIDR %q: %w", c, err)
		}
		nets = append(nets, n)
	}
	q := &quotaMiddleware{cfg: cfg, trusted: nets, clients: map[string]*clientBuckets{}}
	go q.evictLoop()
	return q.handler, nil
}

func (q *quotaMiddleware) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := q.clientKey(r)
		now := time.Now()
		cb := q.bucketFor(key, now)

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
	cb, ok := q.clients[key]
	if !ok {
		cb = &clientBuckets{
			minute: rate.NewLimiter(rate.Limit(float64(q.cfg.PerMinute)/60.0), q.cfg.PerMinute),
			day:    &dayBucket{limit: q.cfg.PerDay, windowStart: now},
		}
		q.clients[key] = cb
	}
	cb.lastSeen = now
	return cb
}

func (q *quotaMiddleware) evictLoop() {
	t := time.NewTicker(q.cfg.IdleTTL)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-q.cfg.IdleTTL)
		q.mu.Lock()
		for k, cb := range q.clients {
			if cb.lastSeen.Before(cutoff) {
				delete(q.clients, k)
			}
		}
		q.mu.Unlock()
	}
}

// clientKey derives the quota bucket key from the request. It returns the
// resolved client IP normalized to a /64 for IPv6 (so a single client cannot
// cycle through a /64 to multiply its quota) or the full /32 for IPv4.
func (q *quotaMiddleware) clientKey(r *http.Request) string {
	ip := q.clientIP(r)
	if ip == nil {
		// Unparseable RemoteAddr: fall back to the raw value so it still keys to
		// a single bucket rather than bypassing the quota.
		return "raw:" + r.RemoteAddr
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	// IPv6: mask to /64.
	mask := net.CIDRMask(64, 128)
	return ip.Mask(mask).String() + "/64"
}

// clientIP resolves the trusted client IP: CF-Connecting-IP is honored ONLY
// when the direct peer (RemoteAddr) is itself within the trusted-proxy set;
// otherwise the direct peer IP is used and any forwarded header is ignored.
func (q *quotaMiddleware) clientIP(r *http.Request) net.IP {
	peer := remoteIP(r.RemoteAddr)
	if peer == nil {
		return nil
	}
	if q.isTrusted(peer) {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
			if ip := net.ParseIP(cf); ip != nil {
				return ip
			}
		}
	}
	return peer
}

func (q *quotaMiddleware) isTrusted(ip net.IP) bool {
	for _, n := range q.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// remoteIP extracts the IP from a host:port RemoteAddr, tolerating a bare IP.
func remoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

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
