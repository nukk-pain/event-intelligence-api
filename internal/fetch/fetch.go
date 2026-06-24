// Package fetch implements the HTTP-first fetcher for the ingestion backend.
//
// It provides: a rate-limited http.Client with retry/backoff, an SSRF egress
// guard (scheme + host allowlist + per-connection destination-IP validation
// that is re-applied after every redirect), a body size cap (LimitReader +
// Content-Length pre-check), best-effort conditional GET, a minimal inline
// robots.txt matcher (per-host fetch + TTL cache, re-evaluated on every
// redirect target, honoring Crawl-delay enforced per host so one venue's delay
// never throttles another), and non-UTF-8 -> UTF-8 transcoding via
// golang.org/x/net/html/charset.
//
// HTTP-first is the rule; a CDP fallback is exposed as an interface hook only
// (see CDPFetcher) and is intentionally left unimplemented in this task.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Sentinel errors returned by Fetch so callers (and tests) can branch on cause.
var (
	// ErrHostNotAllowed means the destination host is not on the allowlist.
	ErrHostNotAllowed = errors.New("fetch: host not allowed")
	// ErrBlockedAddress means the resolved destination IP is private/loopback/
	// link-local/metadata and was rejected by the SSRF guard.
	ErrBlockedAddress = errors.New("fetch: blocked destination address")
	// ErrBadScheme means the URL scheme is not http or https.
	ErrBadScheme = errors.New("fetch: scheme must be http or https")
	// ErrBodyTooLarge means the response body exceeded the configured cap
	// (either declared via Content-Length or observed while streaming).
	ErrBodyTooLarge = errors.New("fetch: response body too large")
	// ErrRobotsDisallowed means robots.txt forbids the configured UA from the
	// requested path.
	ErrRobotsDisallowed = errors.New("fetch: path disallowed by robots.txt")
	// ErrTooManyRedirects means the redirect cap was exceeded.
	ErrTooManyRedirects = errors.New("fetch: too many redirects")
)

// Defaults.
const (
	defaultTimeout      = 20 * time.Second
	defaultPerMinute    = 30
	defaultMaxBodyBytes = 5 << 20 // 5MB
	defaultMaxRetries   = 2
	defaultRetryBackoff = 500 * time.Millisecond
	defaultMaxRedirects = 3
	defaultRobotsTTL    = 24 * time.Hour
)

// Conditional carries best-effort validators for conditional GET plus an
// optional Referer. The caller supplies whatever the server previously handed
// back (ETag / Last-Modified); empty fields are not sent. Referer is an opt-in
// request header some endpoints require (e.g. an AJAX pagination endpoint that
// rejects requests without a same-origin Referer); it defaults to empty so every
// existing caller is byte-for-byte unchanged.
type Conditional struct {
	ETag         string // value to send as If-None-Match
	LastModified string // value to send as If-Modified-Since
	Referer      string // value to send as Referer; empty means no Referer header
}

// Result is a fetched document.
type Result struct {
	URL          string // final URL after redirects
	StatusCode   int
	ContentType  string
	Body         []byte // UTF-8 (transcoded if the source declared another charset)
	ETag         string // ETag from the response, if any
	LastModified string // Last-Modified from the response, if any
	NotModified  bool   // true when the server answered 304
}

// CDPFetcher is the (unimplemented) CDP fallback hook. When a page cannot be
// retrieved deterministically over plain HTTP (JS-rendered content), an
// implementation of this interface would drive a headless browser via Chrome
// DevTools Protocol. Task 1.1 defines the interface only; no implementation is
// wired by default (Fetcher.cdp stays nil). The HTTP-first principle means this
// must never be reached for the static COEX/KINTEX pages.
type CDPFetcher interface {
	FetchRendered(ctx context.Context, rawURL string) (*Result, error)
}

// Fetcher performs rate-limited, SSRF-guarded HTTP GETs.
type Fetcher struct {
	client       *http.Client
	ua           string
	timeout      time.Duration
	maxBodyBytes int64
	maxRetries   int
	retryBackoff time.Duration
	maxRedirects int

	limiterMu sync.Mutex
	perMinute int
	limiters  map[string]*rate.Limiter

	allowed      map[string]struct{} // host allowlist (lowercased)
	allowAnyHost bool
	allowLoopopt bool // allow loopback IPs (tests only)

	robotsTTL      time.Duration
	robotsMu       sync.Mutex
	robots         map[string]*robotsCacheEntry
	robotsInflight map[string]*robotsInflight

	// hostGateMu guards hostGate. Crawl-delay is enforced PER HOST so a slow
	// venue never throttles the other; base request rate limiters are also
	// host-scoped and are never mutated by Crawl-delay.
	hostGateMu sync.Mutex
	hostGate   map[string]*hostThrottle

	// cdp is the optional CDP fallback. Nil unless explicitly wired. Defined as
	// a hook only for this task.
	cdp CDPFetcher
}

// NewFetcher builds a Fetcher. The default host allowlist is the two production
// venues (www.coex.co.kr, www.kintex.com).
func NewFetcher(opts ...Option) (*Fetcher, error) {
	f := &Fetcher{
		ua:           "eventsintel/0.1",
		perMinute:    defaultPerMinute,
		timeout:      defaultTimeout,
		maxBodyBytes: defaultMaxBodyBytes,
		maxRetries:   defaultMaxRetries,
		retryBackoff: defaultRetryBackoff,
		maxRedirects: defaultMaxRedirects,
		robotsTTL:    defaultRobotsTTL,
		allowed: map[string]struct{}{
			"www.coex.co.kr": {},
			"www.kintex.com": {},
		},
		limiters:       map[string]*rate.Limiter{},
		robots:         map[string]*robotsCacheEntry{},
		robotsInflight: map[string]*robotsInflight{},
		hostGate:       map[string]*hostThrottle{},
	}
	for _, o := range opts {
		o(f)
	}

	f.client = f.newHTTPClient()
	return f, nil
}

// hostAllowed reports whether host is on the allowlist.
func (f *Fetcher) hostAllowed(host string) bool {
	if f.allowAnyHost {
		return true
	}
	_, ok := f.allowed[strings.ToLower(host)]
	return ok
}

// validateURL checks scheme and host allowlist (no network).
func (f *Fetcher) validateURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: %q", ErrBadScheme, u.Scheme)
	}
	if !f.hostAllowed(u.Hostname()) {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, u.Hostname())
	}
	return nil
}

// Fetch retrieves rawURL after applying the SSRF guard, rate limit, robots
// gate, and (best-effort) conditional GET. The returned Body is UTF-8.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string, cond Conditional) (*Result, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch: parse url: %w", err)
	}
	if err := f.validateURL(u); err != nil {
		return nil, err
	}

	// robots gate (records per-host Crawl-delay; never mutates the shared limiter).
	allowed, crawlDelay, err := f.robotsAllows(ctx, u)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fmt.Errorf("%w: %s", ErrRobotsDisallowed, u.Path)
	}
	f.recordCrawlDelay(u.Hostname(), crawlDelay)

	// Enforce this host's Crawl-delay (if any) before the request, scoped to the
	// host so a slow venue never throttles the other.
	if err := f.waitHostCrawlDelay(ctx, u.Hostname()); err != nil {
		return nil, err
	}

	return f.doWithRetry(ctx, u, cond)
}

// doWithRetry performs the GET with rate limiting and retry/backoff.
func (f *Fetcher) doWithRetry(ctx context.Context, u *url.URL, cond Conditional) (*Result, error) {
	var lastErr error
	attempts := f.maxRetries + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			backoff := f.retryBackoff << (attempt - 1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		if err := f.waitHostRateLimit(ctx, u); err != nil {
			return nil, err
		}
		res, retryable, err := f.do(ctx, u, cond)
		if err != nil {
			lastErr = err
			// Guard/size/scheme failures are not retryable.
			if errors.Is(err, ErrBlockedAddress) || errors.Is(err, ErrHostNotAllowed) ||
				errors.Is(err, ErrBadScheme) || errors.Is(err, ErrBodyTooLarge) ||
				errors.Is(err, ErrTooManyRedirects) || errors.Is(err, ErrRobotsDisallowed) {
				return nil, err
			}
			if !retryable {
				return nil, err
			}
			continue
		}
		return res, nil
	}
	if lastErr == nil {
		lastErr = errors.New("fetch: exhausted retries")
	}
	return nil, lastErr
}

// do performs a single HTTP GET and reads the (size-capped, transcoded) body.
func (f *Fetcher) do(ctx context.Context, u *url.URL, cond Conditional) (res *Result, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", f.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	// Best-effort conditional GET: only send validators the caller actually has.
	if cond.ETag != "" {
		req.Header.Set("If-None-Match", cond.ETag)
	}
	if cond.LastModified != "" {
		req.Header.Set("If-Modified-Since", cond.LastModified)
	}
	// Opt-in Referer for endpoints that require a same-origin referrer (left unset
	// — and thus identical to prior behavior — for every caller that omits it).
	if cond.Referer != "" {
		req.Header.Set("Referer", cond.Referer)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		// Unwrap guard errors carried through url.Error.
		if errors.Is(err, ErrBlockedAddress) {
			return nil, false, ErrBlockedAddress
		}
		if errors.Is(err, ErrHostNotAllowed) {
			return nil, false, ErrHostNotAllowed
		}
		if errors.Is(err, ErrBadScheme) {
			return nil, false, ErrBadScheme
		}
		if errors.Is(err, ErrTooManyRedirects) {
			return nil, false, ErrTooManyRedirects
		}
		if errors.Is(err, ErrRobotsDisallowed) {
			return nil, false, ErrRobotsDisallowed
		}
		// Network/timeout errors are retryable.
		return nil, true, fmt.Errorf("fetch: do: %w", err)
	}
	defer resp.Body.Close()

	out := &Result{
		URL:          resp.Request.URL.String(),
		StatusCode:   resp.StatusCode,
		ContentType:  resp.Header.Get("Content-Type"),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	if resp.StatusCode == http.StatusNotModified {
		out.NotModified = true
		return out, false, nil
	}

	// Retry on 429 and 5xx.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		// Drain a little so the connection can be reused; ignore errors.
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		return nil, true, fmt.Errorf("fetch: status %d", resp.StatusCode)
	}

	// Reject before reading if Content-Length already exceeds the cap.
	if resp.ContentLength > 0 && resp.ContentLength > f.maxBodyBytes {
		return nil, false, fmt.Errorf("%w: content-length %d > %d", ErrBodyTooLarge, resp.ContentLength, f.maxBodyBytes)
	}

	// Read with a hard cap: read one byte past the cap to detect overflow.
	limited := io.LimitReader(resp.Body, f.maxBodyBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, true, fmt.Errorf("fetch: read body: %w", err)
	}
	if int64(len(raw)) > f.maxBodyBytes {
		return nil, false, fmt.Errorf("%w: streamed > %d", ErrBodyTooLarge, f.maxBodyBytes)
	}

	// Transcode to UTF-8 if the source declared (header/meta) another charset.
	utf8Body, err := toUTF8(raw, out.ContentType)
	if err != nil {
		return nil, false, fmt.Errorf("fetch: charset: %w", err)
	}
	out.Body = utf8Body
	return out, false, nil
}
