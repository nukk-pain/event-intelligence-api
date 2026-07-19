package fetch

import (
	"strings"
	"time"

	"golang.org/x/time/rate"
)

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

// WithStrictPublicCrawl enables fail-closed public crawling and binds every
// robots, redirect, retry, and body read to the caller-owned job budget.
func WithStrictPublicCrawl(budget *CrawlBudget) Option {
	return func(f *Fetcher) {
		f.strictPublicCrawl = true
		f.crawlBudget = budget
		f.maxBodyBytes = defaultPublicMaxBodyBytes
		f.maxRetries = defaultPublicMaxRetries
		f.maxRedirects = defaultPublicMaxRedirects
	}
}

// WithAllowedHosts appends hosts to the allowlist.
func WithAllowedHosts(hosts ...string) Option {
	return func(f *Fetcher) {
		for _, h := range hosts {
			f.allowed[strings.ToLower(h)] = struct{}{}
		}
	}
}

// WithAnyPublicHost permits arbitrary http(s) hosts after the SSRF IP guard.
func WithAnyPublicHost(v bool) Option { return func(f *Fetcher) { f.allowAnyHost = v } }

// WithAllowLoopback permits loopback destination IPs. Intended for httptest
// only; link-local/metadata addresses remain blocked regardless.
func WithAllowLoopback(v bool) Option { return func(f *Fetcher) { f.allowLoopopt = v } }

// WithRobotsTTL sets the robots.txt cache TTL.
func WithRobotsTTL(d time.Duration) Option { return func(f *Fetcher) { f.robotsTTL = d } }

// WithCDPFallback wires an (optional) CDP fallback. Unused in Task 1.1.
func WithCDPFallback(c CDPFetcher) Option { return func(f *Fetcher) { f.cdp = c } }
