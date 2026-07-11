package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LinkRef is a candidate link discovered on the event page: its URL plus text
// used to decide relevance — the fetched page text (fixture mode) or just the
// anchor text (real-crawl mode, where the full page is fetched only if chosen).
type LinkRef struct {
	URL  string
	Text string
}

// LinkFetcher fetches the full text of a chosen link's page. In real-crawl mode
// the agent selects links on cheap anchor text, then fetches only the chosen
// pages via this — deciding what to crawl before spending the request. Nil in
// fixture mode (link Text is already the full page text).
type LinkFetcher func(ctx context.Context, url string) (string, error)

// Brief is the founder-actionable output: the core event facts plus the
// action-oriented facts the agent enriched by reading linked pages.
type Brief struct {
	Facts            Facts    `json:"facts"`
	RegisterURL      *string  `json:"register_url"`
	RegisterDeadline *string  `json:"register_deadline"`
	BoothInfo        *string  `json:"booth_info"`
	StartupProgram   *string  `json:"startup_program"`
	UsedLinks        []string `json:"used_links"`
}

// Trace records how many model calls the run made and their total token cost,
// so callers can report per-event cost.
type Trace struct {
	Calls int
	Usage Usage
}

const selectPrompt = `You help a startup founder decide which links to read.
Given event facts and a numbered list of candidate links (url + snippet), pick
the links most likely to contain: registration/application, registration
deadline, booth/exhibitor info, or a startup program. Return ONLY JSON:
{"pick": [<url>, ...]}. Pick only clearly relevant links; pick none if none fit.`

const enrichPrompt = `From the linked page text below, extract founder-actionable
facts as a JSON object with exactly these keys:
register_url, register_deadline, booth_info, startup_program.
Rules: use only facts present in the text; null if absent; never invent; keep
values short and factual. Output the JSON object only.`

// Run is the agent loop: extract core facts, decide which links to read, read
// the chosen links to enrich with action facts, and assemble a founder brief.
// If linkFetcher is non-nil, chosen links are fetched (real crawl) after the
// selection decision; otherwise each link's Text is used as-is (fixture mode).
func Run(ctx context.Context, be Backend, mainText string, links []LinkRef, linkFetcher LinkFetcher, maxTokens int, timeout time.Duration) (Brief, Trace, error) {
	var tr Trace

	facts, u, _, err := Extract(ctx, be, mainText, maxTokens, timeout)
	if err != nil {
		return Brief{}, tr, fmt.Errorf("extract: %w", err)
	}
	tr.Calls++
	tr.Usage.PromptTokens += u.PromptTokens
	tr.Usage.CompletionTokens += u.CompletionTokens

	brief := Brief{Facts: facts}
	if len(links) == 0 {
		return brief, tr, nil
	}

	chosen, u2, err := selectLinks(ctx, be, facts, links, maxTokens, timeout)
	if err != nil {
		return brief, tr, fmt.Errorf("select links: %w", err)
	}
	tr.Calls++
	tr.Usage.PromptTokens += u2.PromptTokens
	tr.Usage.CompletionTokens += u2.CompletionTokens
	if len(chosen) == 0 {
		return brief, tr, nil
	}

	// Real-crawl mode: fetch the chosen pages now that the agent has decided.
	if linkFetcher != nil {
		for i := range chosen {
			if txt, ferr := linkFetcher(ctx, chosen[i].URL); ferr == nil && strings.TrimSpace(txt) != "" {
				chosen[i].Text = txt
			}
		}
	}

	action, u3, err := enrich(ctx, be, chosen, maxTokens, timeout)
	if err != nil {
		return brief, tr, fmt.Errorf("enrich: %w", err)
	}
	tr.Calls++
	tr.Usage.PromptTokens += u3.PromptTokens
	tr.Usage.CompletionTokens += u3.CompletionTokens

	brief.RegisterURL = strPtr(action["register_url"])
	brief.RegisterDeadline = strPtr(action["register_deadline"])
	brief.BoothInfo = strPtr(action["booth_info"])
	brief.StartupProgram = strPtr(action["startup_program"])
	for _, l := range chosen {
		brief.UsedLinks = append(brief.UsedLinks, l.URL)
	}
	return brief, tr, nil
}

// selectLinks asks the model which candidate links are worth reading.
func selectLinks(ctx context.Context, be Backend, facts Facts, links []LinkRef, maxTokens int, timeout time.Duration) ([]LinkRef, Usage, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Event: %s\n\nCandidate links:\n", stringify(facts["name"]))
	for i, l := range links {
		snippet := l.Text
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		fmt.Fprintf(&sb, "%d. %s\n   %s\n", i+1, l.URL, StripContacts(snippet))
	}
	content, u, _, err := be.Chat(ctx, selectPrompt, sb.String(), maxTokens, timeout)
	if err != nil {
		return nil, u, err
	}
	var picked struct {
		Pick []string `json:"pick"`
	}
	if js := LastJSONObject(content); js != "" {
		_ = json.Unmarshal([]byte(js), &picked)
	}
	want := map[string]bool{}
	for _, p := range picked.Pick {
		want[strings.TrimSpace(p)] = true
	}
	var chosen []LinkRef
	for _, l := range links {
		if want[l.URL] {
			chosen = append(chosen, l)
		}
	}
	return chosen, u, nil
}

// enrich reads the chosen link texts and extracts action facts.
func enrich(ctx context.Context, be Backend, chosen []LinkRef, maxTokens int, timeout time.Duration) (map[string]any, Usage, error) {
	var sb strings.Builder
	for _, l := range chosen {
		fmt.Fprintf(&sb, "URL: %s\n%s\n\n", l.URL, StripContacts(l.Text))
	}
	content, u, _, err := be.Chat(ctx, enrichPrompt, sb.String(), maxTokens, timeout)
	if err != nil {
		return nil, u, err
	}
	out := map[string]any{}
	if js := LastJSONObject(content); js != "" {
		_ = json.Unmarshal([]byte(js), &out)
	}
	return out, u, nil
}

func strPtr(v any) *string {
	s := stringify(v)
	if s == "" {
		return nil
	}
	return &s
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
