package solarenrich

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/PuerkitoBio/goquery"
	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

const (
	// maxActionLinks bounds how many anchors the model may choose from. The
	// agent picks on cheap anchor text and only chosen pages are fetched, so
	// this caps the prompt rather than the crawl.
	maxActionLinks = 40
	// maxPageRunes bounds the page text sent to the model. Venue pages run long
	// and the action signals sit near the top.
	maxPageRunes = 6000
)

var (
	httpURL    = regexp.MustCompile(`^https?://`)
	whitespace = regexp.MustCompile(`\s+`)
)

// PageFetcher reads one linked page. The pipeline supplies its own official
// fetcher, so the enricher inherits the same allowlist, robots policy, and
// rate limits rather than opening a second uncontrolled path to the web.
type PageFetcher func(ctx context.Context, url string) (string, error)

// ActionEnricher fills action signals the deterministic extractor could not
// find, by letting the model follow links from the official event page.
//
// This is where the deployed service spends most of its Solar budget.
// Registration deadlines, booth applications, and startup-program windows are
// what a founder actually opens this service for, and the goquery rules miss
// them on four pages out of five.
type ActionEnricher struct {
	config  Config
	fetcher PageFetcher
	// slots bounds concurrent model work so a full-coverage run stays under
	// Solar's per-minute token ceiling. Nil means unbounded.
	slots     chan struct{}
	calls     atomic.Int64
	filled    atomic.Int64
	throttled atomic.Int64
}

func NewActionEnricher(config Config, fetcher PageFetcher) (*ActionEnricher, error) {
	if config.Backend.BaseURL == "" || config.Backend.Model == "" ||
		config.MaxCalls <= 0 || config.MaxTokens <= 0 || config.CallTimeout <= 0 || fetcher == nil {
		return nil, ErrInvalidConfig
	}
	enricher := &ActionEnricher{config: config, fetcher: fetcher}
	if config.MaxConcurrent > 0 {
		enricher.slots = make(chan struct{}, config.MaxConcurrent)
	}
	return enricher, nil
}

// acquire blocks until a model slot frees or the context ends.
func (a *ActionEnricher) acquire(ctx context.Context) error {
	if a.slots == nil {
		return nil
	}
	select {
	case a.slots <- struct{}{}:
		return nil
	default:
	}
	a.throttled.Add(1)
	select {
	case a.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *ActionEnricher) release() {
	if a.slots != nil {
		<-a.slots
	}
}

// Calls and Filled report what one ingest run spent and gained. Counts only.
func (a *ActionEnricher) Calls() int64  { return a.calls.Load() }
func (a *ActionEnricher) Filled() int64 { return a.filled.Load() }

// Throttled counts attempts that had to wait for a model slot.
func (a *ActionEnricher) Throttled() int64 { return a.throttled.Load() }

// EnrichActions returns only the signals it resolved. Signals the caller
// already holds are left untouched, and an empty return means the model added
// nothing, which is a normal outcome rather than a failure.
func (a *ActionEnricher) EnrichActions(ctx context.Context, pageURL string, body []byte, current sources.ActionSignals) (sources.ActionSignals, error) {
	var out sources.ActionSignals
	if a == nil || len(body) == 0 || !wantsAction(current) {
		return out, nil
	}
	if a.calls.Add(1) > int64(a.config.MaxCalls) {
		// The budget is a ceiling for the whole run, so an over-budget event
		// stays deterministic rather than waiting.
		a.calls.Add(-1)
		return out, nil
	}
	text, links, err := pageTextAndLinks(pageURL, body)
	if err != nil || strings.TrimSpace(text) == "" {
		return out, err
	}
	if err := a.acquire(ctx); err != nil {
		return out, err
	}
	defer a.release()
	callCtx, cancel := context.WithTimeout(ctx, a.config.CallTimeout)
	defer cancel()
	brief, _, err := agent.Run(callCtx, a.config.Backend, text, links,
		agent.LinkFetcher(a.fetcher), a.config.MaxTokens, a.config.CallTimeout)
	if err != nil {
		return out, err
	}

	changed := false
	if current.RegisterURL == nil && validURL(brief.RegisterURL) {
		out.RegisterURL = brief.RegisterURL
		if current.CanRegister == nil {
			out.CanRegister = boolPtr(true)
		}
		changed = true
	}
	// A deadline that is not an ISO date is discarded rather than stored, so a
	// model answering in prose cannot corrupt the event contract.
	if current.RegistrationDeadline == nil && isoDate.MatchString(deref(brief.RegisterDeadline)) {
		out.RegistrationDeadline = brief.RegisterDeadline
		changed = true
	}
	if current.CanExhibit == nil && nonEmpty(brief.BoothInfo) {
		out.CanExhibit = boolPtr(true)
		changed = true
	}
	if current.HasStartupProgram == nil && nonEmpty(brief.StartupProgram) {
		out.HasStartupProgram = boolPtr(true)
		changed = true
	}
	if changed {
		a.filled.Add(1)
	}
	return out, nil
}

// wantsAction reports whether any signal this enricher can resolve is still
// unknown, so a fully extracted event never costs a model call.
func wantsAction(current sources.ActionSignals) bool {
	return current.RegisterURL == nil || current.RegistrationDeadline == nil ||
		current.CanExhibit == nil || current.HasStartupProgram == nil
}

func pageTextAndLinks(pageURL string, body []byte) (string, []agent.LinkRef, error) {
	document, err := fetch.ParseHTML(string(body))
	if err != nil {
		return "", nil, err
	}
	text := collapse(document.Text())
	if runes := []rune(text); len(runes) > maxPageRunes {
		text = string(runes[:maxPageRunes])
	}
	seen := map[string]struct{}{}
	links := make([]agent.LinkRef, 0, maxActionLinks)
	document.Find("a[href]").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		href, ok := selection.Attr("href")
		if !ok {
			return true
		}
		resolved, ok := resolveLink(pageURL, href)
		if !ok {
			return true
		}
		if _, duplicate := seen[resolved]; duplicate {
			return true
		}
		seen[resolved] = struct{}{}
		links = append(links, agent.LinkRef{URL: resolved, Text: collapse(selection.Text())})
		return len(links) < maxActionLinks
	})
	return text, links, nil
}

func resolveLink(pageURL, href string) (string, bool) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", false
	}
	resolved.Fragment = ""
	return resolved.String(), true
}

func collapse(text string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(text, " "))
}

func validURL(value *string) bool { return value != nil && httpURL.MatchString(*value) }

func nonEmpty(value *string) bool { return value != nil && strings.TrimSpace(*value) != "" }

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolPtr(v bool) *bool { return &v }
