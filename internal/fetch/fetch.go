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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
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

// Conditional carries best-effort validators for conditional GET. The caller
// supplies whatever the server previously handed back (ETag / Last-Modified).
// Empty fields are not sent.
type Conditional struct {
	ETag         string // value to send as If-None-Match
	LastModified string // value to send as If-Modified-Since
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
	allowLoopopt bool                // allow loopback IPs (tests only)

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

// hostThrottle records a host's effective Crawl-delay and the time of its last
// fetch so the next fetch to that host can be gated independently of other
// hosts.
type hostThrottle struct {
	delay     time.Duration
	lastFetch time.Time
}

// Option configures a Fetcher.
type Option func(*Fetcher)

// WithUserAgent sets the User-Agent header and robots match token.
func WithUserAgent(ua string) Option { return func(f *Fetcher) { f.ua = ua } }

// WithPerMinute sets the outbound request rate (requests per minute).
func WithPerMinute(n int) Option {
	return func(f *Fetcher) {
		if n > 0 {
			f.perMinute = n
			f.limiters = map[string]*rate.Limiter{}
		}
	}
}

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) Option { return func(f *Fetcher) { f.timeout = d } }

// WithMaxBodyBytes caps the response body size.
func WithMaxBodyBytes(n int64) Option { return func(f *Fetcher) { f.maxBodyBytes = n } }

// WithMaxRetries sets how many times a retryable failure is retried.
func WithMaxRetries(n int) Option { return func(f *Fetcher) { f.maxRetries = n } }

// WithRetryBackoff sets the base backoff between retries (exponential).
func WithRetryBackoff(d time.Duration) Option { return func(f *Fetcher) { f.retryBackoff = d } }

// WithMaxRedirects sets the redirect cap.
func WithMaxRedirects(n int) Option { return func(f *Fetcher) { f.maxRedirects = n } }

// WithAllowedHosts appends hosts to the allowlist.
func WithAllowedHosts(hosts ...string) Option {
	return func(f *Fetcher) {
		for _, h := range hosts {
			f.allowed[strings.ToLower(h)] = struct{}{}
		}
	}
}

// WithAllowLoopback permits loopback destination IPs. Intended for httptest
// only; link-local/metadata addresses remain blocked regardless.
func WithAllowLoopback(v bool) Option { return func(f *Fetcher) { f.allowLoopopt = v } }

// WithRobotsTTL sets the robots.txt cache TTL.
func WithRobotsTTL(d time.Duration) Option { return func(f *Fetcher) { f.robotsTTL = d } }

// WithCDPFallback wires an (optional) CDP fallback. Unused in Task 1.1.
func WithCDPFallback(c CDPFetcher) Option { return func(f *Fetcher) { f.cdp = c } }

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

	// SSRF-guarded dialer: validate the resolved destination IP on every dial,
	// which re-runs after each redirect (a new connection is dialed per host).
	baseDialer := &net.Dialer{Timeout: f.timeout, KeepAlive: 30 * time.Second}
	guardedDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("fetch: no addresses for %s", host)
		}
		var dialErr error
		for _, ip := range ips {
			if isMetadataOrLinkLocal(ip) {
				return nil, fmt.Errorf("%w: %s (link-local/metadata)", ErrBlockedAddress, ip)
			}
			if isBlockedIP(ip) && !(f.allowLoopopt && ip.IsLoopback()) {
				return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
			}
		}
		// Pin the connection to a validated IP to avoid DNS-rebinding TOCTOU.
		for _, ip := range ips {
			conn, err := baseDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			dialErr = err
		}
		return nil, dialErr
	}

	transport := &http.Transport{
		DialContext:           guardedDial,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	f.client = &http.Client{
		Timeout:   f.timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= f.maxRedirects {
				return fmt.Errorf("%w: %d", ErrTooManyRedirects, len(via))
			}
			// Re-validate scheme + host allowlist on every redirect target.
			// IP-level validation is enforced by guardedDial when the new
			// connection is opened.
			if err := f.validateURL(req.URL); err != nil {
				return err
			}
			// Re-check robots for the redirect target: a same-host redirect can
			// land on a Disallow path that the initial URL did not cover. The
			// in-flight ctx travels on the redirected request (Go copies it from
			// the prior request), so robots fetch/cache uses it.
			allowed, crawlDelay, err := f.robotsAllows(req.Context(), req.URL)
			if err != nil {
				return err
			}
			if !allowed {
				return fmt.Errorf("%w: %s", ErrRobotsDisallowed, req.URL.Path)
			}
			f.recordCrawlDelay(req.URL.Hostname(), crawlDelay)
			if err := f.waitHostCrawlDelay(req.Context(), req.URL.Hostname()); err != nil {
				return err
			}
			if err := f.waitHostRateLimit(req.Context(), req.URL); err != nil {
				return err
			}
			return nil
		},
	}
	return f, nil
}

// hostAllowed reports whether host is on the allowlist.
func (f *Fetcher) hostAllowed(host string) bool {
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

// toUTF8 converts body to UTF-8 using the declared content-type and any meta
// charset declaration. If the content is already UTF-8 it is returned as-is.
func toUTF8(body []byte, contentType string) ([]byte, error) {
	r, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		// Unknown charset: fall back to the original bytes rather than failing.
		return body, nil
	}
	converted, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return converted, nil
}

func (f *Fetcher) waitHostRateLimit(ctx context.Context, u *url.URL) error {
	return f.limiterFor(u.Hostname()).Wait(ctx)
}

func (f *Fetcher) limiterFor(host string) *rate.Limiter {
	key := strings.ToLower(host)
	f.limiterMu.Lock()
	defer f.limiterMu.Unlock()
	limiter, ok := f.limiters[key]
	if ok {
		return limiter
	}
	perMinute := f.perMinute
	if perMinute <= 0 {
		perMinute = defaultPerMinute
	}
	limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(perMinute)), 1)
	f.limiters[key] = limiter
	return limiter
}

// recordCrawlDelay stores (or refreshes) the effective Crawl-delay for host.
// A non-positive delay clears any prior throttle for that host. Base request
// limiters are never mutated, so one host's Crawl-delay cannot bleed into others.
func (f *Fetcher) recordCrawlDelay(host string, d time.Duration) {
	host = strings.ToLower(host)
	f.hostGateMu.Lock()
	defer f.hostGateMu.Unlock()
	g, ok := f.hostGate[host]
	if !ok {
		g = &hostThrottle{}
		f.hostGate[host] = g
	}
	g.delay = d
}

// waitHostCrawlDelay blocks until at least the host's Crawl-delay has elapsed
// since that host's previous fetch, then marks the host as fetched-now. Hosts
// without a Crawl-delay are not gated. Only the named host is affected.
func (f *Fetcher) waitHostCrawlDelay(ctx context.Context, host string) error {
	host = strings.ToLower(host)

	f.hostGateMu.Lock()
	g, ok := f.hostGate[host]
	if !ok || g.delay <= 0 {
		// No throttle for this host: still record the fetch time so a later
		// Crawl-delay update can space from it, then return immediately.
		if !ok {
			g = &hostThrottle{}
			f.hostGate[host] = g
		}
		g.lastFetch = time.Now()
		f.hostGateMu.Unlock()
		return nil
	}
	var wait time.Duration
	if !g.lastFetch.IsZero() {
		if elapsed := time.Since(g.lastFetch); elapsed < g.delay {
			wait = g.delay - elapsed
		}
	}
	// Optimistically reserve this slot so concurrent callers space out too.
	g.lastFetch = time.Now().Add(wait)
	f.hostGateMu.Unlock()

	if wait <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

// ---- SSRF address classification ----

// isBlockedIP reports whether ip is a non-public address that the egress guard
// must reject: loopback, link-local (v4 169.254/16 and v6 fe80::/10),
// unspecified (0.0.0.0/::), RFC1918 private v4, and ULA v6 (fc00::/7).
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsMulticast() {
		return true
	}
	return false
}

// isMetadataOrLinkLocal flags link-local / cloud-metadata addresses that must be
// rejected even when loopback is otherwise permitted (test bypass).
func isMetadataOrLinkLocal(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// 169.254.169.254 is covered by IsLinkLocalUnicast, but assert explicitly.
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}

// ---- minimal inline robots.txt matcher ----

type robotsRules struct {
	disallow   []string
	allow      []string
	crawlDelay time.Duration
}

type robotsCacheEntry struct {
	rules     *robotsRules
	fetchedAt time.Time
}

type robotsInflight struct {
	done  chan struct{}
	rules *robotsRules
	err   error
}

// robotsAllows fetches+caches robots.txt for u's host and reports whether the
// configured UA may fetch u.Path, plus any Crawl-delay.
func (f *Fetcher) robotsAllows(ctx context.Context, u *url.URL) (bool, time.Duration, error) {
	rules, err := f.robotsFor(ctx, u)
	if err != nil {
		// Be permissive on robots fetch failure (network/404): allow.
		return true, 0, nil
	}
	if rules == nil {
		return true, 0, nil
	}
	return rules.allows(u.Path), rules.crawlDelay, nil
}

func (f *Fetcher) robotsFor(ctx context.Context, u *url.URL) (*robotsRules, error) {
	key := u.Scheme + "://" + u.Host

	f.robotsMu.Lock()
	if e, ok := f.robots[key]; ok && time.Since(e.fetchedAt) < f.robotsTTL {
		rules := e.rules
		f.robotsMu.Unlock()
		return rules, nil
	}
	if in, ok := f.robotsInflight[key]; ok {
		done := in.done
		f.robotsMu.Unlock()
		return waitRobotsInflight(ctx, in, done)
	}
	in := &robotsInflight{done: make(chan struct{})}
	f.robotsInflight[key] = in
	f.robotsMu.Unlock()

	robotsURL := *u
	sharedCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), f.timeout)
	go func() {
		defer cancel()
		rules, err := f.fetchRobots(sharedCtx, &robotsURL, key)
		f.robotsMu.Lock()
		if err == nil {
			f.robots[key] = &robotsCacheEntry{rules: rules, fetchedAt: time.Now()}
		}
		in.rules = rules
		in.err = err
		delete(f.robotsInflight, key)
		close(in.done)
		f.robotsMu.Unlock()
	}()
	return waitRobotsInflight(ctx, in, in.done)
}

func waitRobotsInflight(ctx context.Context, in *robotsInflight, done <-chan struct{}) (*robotsRules, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return in.rules, in.err
	}
}

func (f *Fetcher) fetchRobots(ctx context.Context, u *url.URL, key string) (*robotsRules, error) {
	if err := f.waitHostRateLimit(ctx, u); err != nil {
		return nil, err
	}
	robotsURL := key + "/robots.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.ua)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rules *robotsRules
	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10)) // 512KB cap
		rules = parseRobots(body, f.ua)
	} else {
		// No robots / error status -> empty rules (everything allowed).
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		rules = &robotsRules{}
	}

	return rules, nil
}

// allows applies a simple longest-match Disallow/Allow prefix policy. An empty
// Disallow rule ("Disallow:") means allow-all and is ignored.
func (r *robotsRules) allows(path string) bool {
	if path == "" {
		path = "/"
	}
	bestAllow := -1
	for _, p := range r.allow {
		if strings.HasPrefix(path, p) && len(p) > bestAllow {
			bestAllow = len(p)
		}
	}
	bestDisallow := -1
	for _, p := range r.disallow {
		if p == "" {
			continue
		}
		if strings.HasPrefix(path, p) && len(p) > bestDisallow {
			bestDisallow = len(p)
		}
	}
	if bestDisallow == -1 {
		return true
	}
	// Allow wins ties and longer-or-equal matches (Google semantics).
	return bestAllow >= bestDisallow
}

// parseRobots extracts the rules applicable to ua, falling back to the "*"
// group. It honors Disallow, Allow, and Crawl-delay directives.
func parseRobots(body []byte, ua string) *robotsRules {
	uaToken := robotsUAToken(ua)

	type group struct {
		agents []string
		rules  robotsRules
	}
	var groups []*group
	var cur *group
	expectingAgents := false

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		field := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])

		switch field {
		case "user-agent":
			if cur == nil || !expectingAgents {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(value))
			expectingAgents = true
		case "disallow":
			if cur != nil {
				cur.rules.disallow = append(cur.rules.disallow, value)
			}
			expectingAgents = false
		case "allow":
			if cur != nil {
				cur.rules.allow = append(cur.rules.allow, value)
			}
			expectingAgents = false
		case "crawl-delay":
			if cur != nil {
				if secs, err := strconv.ParseFloat(value, 64); err == nil && secs > 0 {
					cur.rules.crawlDelay = time.Duration(secs * float64(time.Second))
				}
			}
			expectingAgents = false
		default:
			expectingAgents = false
		}
	}

	// Pick the most specific matching group; fall back to "*".
	var specific, star *robotsRules
	for _, g := range groups {
		for _, a := range g.agents {
			if a == "*" {
				star = &g.rules
			} else if uaToken != "" && strings.Contains(uaToken, a) {
				specific = &g.rules
			}
		}
	}
	if specific != nil {
		return specific
	}
	if star != nil {
		return star
	}
	return &robotsRules{}
}

// robotsUAToken lowercases the UA and strips the version/comment so a robots
// "User-agent: eventsintel" matches "eventsintel/0.1 (+url)".
func robotsUAToken(ua string) string {
	return strings.ToLower(ua)
}

// ParseHTML parses a UTF-8 HTML body into a goquery document. goquery requires
// UTF-8 input; Fetch already guarantees that for its Result.Body.
func ParseHTML(body string) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(body))
}
