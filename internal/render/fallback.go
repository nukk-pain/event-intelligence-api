// Package render turns a browser-rendered DOM into the same HTML input the
// deterministic ingest parsers already consume. It never performs HTTP fetches:
// callers must first pass the page through the robots-aware fetcher.
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
)

const (
	defaultMaxPages = 30
	defaultTimeout  = 15 * time.Second
	shellTextRunes  = 400
)

var ErrInvalidConfig = errors.New("invalid render config")

// Config controls the bounded renderer used only by one ingest batch.
type Config struct {
	UserAgent       string
	MaxPages        int
	Timeout         time.Duration
	ResourceFetcher ResourceFetcher
}

// Resource is a same-origin browser subresource obtained through the existing
// robots-aware fetch boundary rather than by Chrome itself.
type Resource struct {
	Body        []byte
	ContentType string
}

// ResourceFetcher supplies same-origin scripts and XHR responses to the local
// page origin. It keeps every renderer-originated request inside the caller's
// established SSRF, allowlist, robots, and rate-limit policy.
type ResourceFetcher interface {
	Fetch(ctx context.Context, rawURL string) (Resource, error)
}

// TextSelector turns an eligible static page into its browser-rendered DOM.
// The return value remains HTML so existing extractors can read both text and
// action links without a second parsing contract.
type TextSelector interface {
	Text(ctx context.Context, rawURL string, staticBody []byte) ([]byte, error)
}

// Renderer owns the browser-specific work. Keeping this narrow makes the
// per-run fallback testable without starting Chrome.
type Renderer interface {
	Render(ctx context.Context, rawURL string, staticBody []byte) ([]byte, error)
}

// Fallback applies JS-shell detection, a whole-run attempt cap, and a
// per-page timeout around one renderer instance.
type Fallback struct {
	renderer Renderer
	maxPages int64
	timeout  time.Duration
	used     atomic.Int64
}

// New returns the Chrome-backed selector used by one daily ingest batch.
func New(ctx context.Context, config Config) (*Fallback, error) {
	if config.ResourceFetcher == nil {
		return nil, ErrInvalidConfig
	}
	return NewWithRenderer(config, newChromeRenderer(ctx, config.ResourceFetcher, config.UserAgent))
}

// NewWithRenderer accepts a narrow renderer for deterministic tests.
func NewWithRenderer(config Config, renderer Renderer) (*Fallback, error) {
	if config.UserAgent == "" || config.MaxPages <= 0 || config.Timeout <= 0 || renderer == nil {
		return nil, ErrInvalidConfig
	}
	return &Fallback{renderer: renderer, maxPages: int64(config.MaxPages), timeout: config.Timeout}, nil
}

// Close releases the batch-owned browser process. It is idempotent so callers
// can defer it even when no page met the shell heuristic.
func (f *Fallback) Close() {
	if f == nil {
		return
	}
	if closer, ok := f.renderer.(interface{ Close() }); ok {
		closer.Close()
	}
}

// DefaultConfig returns the production bounds from the ingest handoff.
func DefaultConfig(userAgent string) Config {
	return Config{UserAgent: userAgent, MaxPages: defaultMaxPages, Timeout: defaultTimeout}
}

// Text returns staticBody unless it is a JS shell and a bounded Chrome render
// succeeds. A renderer failure is returned with the original static body so
// callers can safely keep the deterministic ingest path non-fatal.
func (f *Fallback) Text(ctx context.Context, rawURL string, staticBody []byte) ([]byte, error) {
	if f == nil || !isJSShell(rawURL, staticBody) || !f.claimPage() {
		return staticBody, nil
	}
	renderCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	rendered, err := f.renderer.Render(renderCtx, rawURL, staticBody)
	if err != nil {
		return staticBody, fmt.Errorf("render %q: %w", rawURL, err)
	}
	if len(bytes.TrimSpace(rendered)) == 0 {
		return staticBody, fmt.Errorf("render %q: empty DOM", rawURL)
	}
	return rendered, nil
}

func (f *Fallback) claimPage() bool {
	for {
		used := f.used.Load()
		if used >= f.maxPages {
			return false
		}
		if f.used.CompareAndSwap(used, used+1) {
			return true
		}
	}
}

// StaticOrRendered preserves the static page when the optional renderer is
// unavailable, capped, or fails. It is shared by deterministic and Solar page
// reads so rendered DOM receives exactly the same downstream treatment.
func StaticOrRendered(ctx context.Context, selector TextSelector, rawURL string, staticBody []byte) []byte {
	if selector == nil {
		return staticBody
	}
	rendered, err := selector.Text(ctx, rawURL, staticBody)
	if err != nil || len(bytes.TrimSpace(rendered)) == 0 {
		return staticBody
	}
	return rendered
}

func isJSShell(rawURL string, body []byte) bool {
	if strings.Contains(rawURL, "#/") {
		return true
	}
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return false
	}
	if bodyOnlyRoot(document) || hasJavaScriptNoscript(document) {
		return true
	}
	return utf8.RuneCountInString(strings.TrimSpace(document.Text())) < shellTextRunes
}

func hasJavaScriptNoscript(document *goquery.Document) bool {
	text := strings.ToLower(strings.TrimSpace(document.Find("noscript").Text()))
	return strings.Contains(text, "javascript") || strings.Contains(text, "자바스크립트")
}

func bodyOnlyRoot(document *goquery.Document) bool {
	body := document.Find("body").First()
	if body.Length() == 0 || body.ChildrenFiltered("#root, #app").Length() != 1 {
		return false
	}
	return body.ChildrenFiltered(":not(#root):not(#app):not(script):not(noscript)").Length() == 0
}
