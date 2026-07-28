package publicdiscovery

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/fetch"
)

var (
	ErrInvalidAgentPageOpener = errors.New("public discovery: invalid agent page opener")
	// ErrOpenURLNotAllowed marks a target refused at the URL boundary — bad
	// scheme, credentials, or a private/loopback/link-local host. Nothing is
	// resolved or connected for such a URL.
	ErrOpenURLNotAllowed = errors.New("public discovery: page open target is not allowed")
	// ErrOpenUnsupportedContent marks a fetched document that is not HTML. The
	// crawl fetcher admits feeds and sitemaps too; an open only reads pages.
	ErrOpenUnsupportedContent = errors.New("public discovery: page open requires an HTML document")
)

const (
	// maxOpenedTitleRunes and maxOpenedSnippetRunes bound what one opened page
	// may return. The discovery loop bounds these fields again before they reach
	// a prompt; the opener applies its own ceiling so a hostile page cannot hand
	// an arbitrarily large string to any caller.
	maxOpenedTitleRunes   = 300
	maxOpenedSnippetRunes = 1200
	// openFetchTimeout bounds one open, retries and the robots fetch included.
	// It is deliberately far below the crawl job timeout: an open is a single
	// second-hop fetch made inside a live request, not a crawl. The deadline is
	// always derived from the caller's context, so a shorter request deadline
	// still wins.
	openFetchTimeout = 15 * time.Second
)

// AgentPageOpener adapts the public crawl fetcher to agent.PageOpener. It reuses
// the provider's fetcher and candidate boundary verbatim, so an open is subject
// to exactly the SSRF, robots, rate-limit, and body-size policy the crawl obeys.
type AgentPageOpener struct {
	provider *Provider
}

var _ agent.PageOpener = (*AgentPageOpener)(nil)

// NewAgentPageOpener creates a page opener with the production public-crawl
// fetch policy. It builds its own provider so an open never spends the search
// crawl's transport or aggregate-body budget.
func NewAgentPageOpener() (*AgentPageOpener, error) {
	provider, err := New()
	if err != nil {
		return nil, err
	}
	return NewAgentPageOpenerWithProvider(provider)
}

// NewAgentPageOpenerWithProvider wraps an already-created provider, which is how
// deterministic fixtures reach the same fetch policy in tests.
func NewAgentPageOpenerWithProvider(provider *Provider) (*AgentPageOpener, error) {
	if provider == nil {
		return nil, ErrInvalidAgentPageOpener
	}
	return &AgentPageOpener{provider: provider}, nil
}

// Open fetches one candidate page and returns its title and a bounded body
// snippet. Date and Location stay empty: this reads a page, it does not infer
// structure from one, and an invented date is worse than a missing one.
func (opener *AgentPageOpener) Open(ctx context.Context, rawURL string) (agent.OpenedPage, error) {
	if opener == nil || opener.provider == nil {
		return agent.OpenedPage{}, ErrInvalidAgentPageOpener
	}
	canonicalURL, err := opener.allowedTarget(rawURL)
	if err != nil {
		return agent.OpenedPage{}, err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, min(openFetchTimeout, opener.provider.limits.Timeout))
	defer cancel()
	result, err := opener.provider.fetcher.Fetch(fetchCtx, canonicalURL, fetch.Conditional{})
	if err != nil {
		// The fetch error is passed through unchanged so callers keep the
		// classification (robots, size, MIME, status). It carries no page body.
		return agent.OpenedPage{}, fmt.Errorf("open page: %w", err)
	}
	if !isHTMLDocumentMIME(result.ContentType) {
		return agent.OpenedPage{}, fmt.Errorf("%w: %s", ErrOpenUnsupportedContent, canonicalURL)
	}
	title, snippet, err := openedPageText(result.Body)
	if err != nil {
		return agent.OpenedPage{}, fmt.Errorf("open page %s: %w", canonicalURL, err)
	}
	return agent.OpenedPage{Title: title, Snippet: snippet}, nil
}

// allowedTarget applies the crawler's own URL boundary before anything is
// resolved or connected, so a private, loopback, metadata, or credential-bearing
// URL costs no network call at all.
func (opener *AgentPageOpener) allowedTarget(rawURL string) (string, error) {
	canonicalURL, err := CanonicalizeURL(rawURL)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrOpenURLNotAllowed, err)
	}
	if !candidateURLAllowed(canonicalURL, opener.provider.allowLocalCandidates) {
		return "", fmt.Errorf("%w: %s", ErrOpenURLNotAllowed, canonicalURL)
	}
	return canonicalURL, nil
}

func isHTMLDocumentMIME(rawContentType string) bool {
	mediaType, _, err := mime.ParseMediaType(rawContentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "text/html", "application/xhtml+xml":
		return true
	default:
		return false
	}
}

// openedPageText extracts the document title and a bounded plain-text snippet.
// Script, style, and template text is removed first: it is not page content and
// is a convenient place to hide instructions aimed at the model.
func openedPageText(body []byte) (title string, snippet string, err error) {
	document, err := fetch.ParseHTML(string(body))
	if err != nil {
		return "", "", errMalformedDocument
	}
	title = safeOpenedText(document.Find("title").First().Text(), maxOpenedTitleRunes)
	document.Find("script, style, noscript, template").Remove()
	snippet = safeOpenedText(document.Find("body").Text(), maxOpenedSnippetRunes)
	return title, snippet, nil
}

// safeOpenedText normalizes whitespace, redacts contact details, and then
// bounds the result, so the returned string is always within the ceiling and
// always contact-free.
func safeOpenedText(raw string, maxRunes int) string {
	text := agent.StripContacts(normalizeText(raw))
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}
