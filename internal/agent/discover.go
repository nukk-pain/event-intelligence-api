package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SearchResult is one hit from a search tool.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchTool is the pluggable search backend. A fixture implementation lets the
// discovery loop be tested offline today; a real web-search implementation drops
// in later without touching the agent logic.
type SearchTool interface {
	Search(ctx context.Context, query string) ([]SearchResult, error)
}

// DiscoveredSource is a candidate the agent judged to be a real event-listing
// source worth crawling.
type DiscoveredSource struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

const proposeQueryPrompt = `You are finding online sources that LIST or ANNOUNCE
industry events (AI, robotics, bio, digital health, medical devices) in Korea and
key international benchmarks. Sources are venue calendars, organizer sites, and
conference pages, not one-off news articles.

Given the goal, the queries already tried, and the sources already found, propose
ONE next web-search query likely to surface NEW event-listing sources. Avoid
repeating tried queries. Return ONLY JSON: {"query": "..."} — or {"done": true}
if further searching is unlikely to help.`

const judgePrompt = `Decide, for each search result, whether it is a legitimate
source that LISTS or ANNOUNCES industry events (venue calendar, organizer/event
site, conference page) — NOT a news article about a single event, a personal
blog, or an unrelated page. Return ONLY JSON:
{"sources":[{"url":"...","is_event_source":true,"reason":"..."}]}.`

// Discover runs the autonomous source-discovery loop: the model proposes a
// search query, the tool runs it, the model judges which results are real event
// sources, and the loop repeats (deciding the next action itself) up to
// maxRounds. Discovered sources are de-duplicated by URL.
func Discover(ctx context.Context, be Backend, goal string, tool SearchTool, maxRounds, maxTokens int, timeout time.Duration) ([]DiscoveredSource, Trace, error) {
	var tr Trace
	var tried []string
	found := map[string]DiscoveredSource{}
	var order []string

	for round := 0; round < maxRounds; round++ {
		query, done, u, err := proposeQuery(ctx, be, goal, tried, order, found, maxTokens, timeout)
		tr.Calls++
		tr.Usage.PromptTokens += u.PromptTokens
		tr.Usage.CompletionTokens += u.CompletionTokens
		if err != nil {
			return collect(order, found), tr, fmt.Errorf("propose query: %w", err)
		}
		if done || strings.TrimSpace(query) == "" {
			break
		}
		tried = append(tried, query)

		results, err := tool.Search(ctx, query)
		if err != nil {
			return collect(order, found), tr, fmt.Errorf("search %q: %w", query, err)
		}
		results = dedupeResults(results, found)
		if len(results) == 0 {
			continue
		}

		judged, u2, err := judgeResults(ctx, be, results, maxTokens, timeout)
		tr.Calls++
		tr.Usage.PromptTokens += u2.PromptTokens
		tr.Usage.CompletionTokens += u2.CompletionTokens
		if err != nil {
			return collect(order, found), tr, fmt.Errorf("judge results: %w", err)
		}
		title := map[string]string{}
		for _, r := range results {
			title[r.URL] = r.Title
		}
		for _, j := range judged {
			if !j.IsEventSource {
				continue
			}
			if _, seen := found[j.URL]; seen {
				continue
			}
			found[j.URL] = DiscoveredSource{URL: j.URL, Title: title[j.URL], Reason: j.Reason}
			order = append(order, j.URL)
		}
	}
	return collect(order, found), tr, nil
}

func proposeQuery(ctx context.Context, be Backend, goal string, tried, foundOrder []string, found map[string]DiscoveredSource, maxTokens int, timeout time.Duration) (query string, done bool, u Usage, err error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Goal: %s\n\nQueries already tried:\n", goal)
	if len(tried) == 0 {
		sb.WriteString("(none yet)\n")
	}
	for _, q := range tried {
		fmt.Fprintf(&sb, "- %s\n", q)
	}
	fmt.Fprintf(&sb, "\nSources already found (%d):\n", len(foundOrder))
	for _, url := range foundOrder {
		fmt.Fprintf(&sb, "- %s\n", url)
	}
	content, u, _, err := be.Chat(ctx, proposeQueryPrompt, sb.String(), maxTokens, timeout)
	if err != nil {
		return "", false, u, err
	}
	var out struct {
		Query string `json:"query"`
		Done  bool   `json:"done"`
	}
	if js := LastJSONObject(content); js != "" {
		_ = json.Unmarshal([]byte(js), &out)
	}
	return out.Query, out.Done, u, nil
}

type judged struct {
	URL           string `json:"url"`
	IsEventSource bool   `json:"is_event_source"`
	Reason        string `json:"reason"`
}

func judgeResults(ctx context.Context, be Backend, results []SearchResult, maxTokens int, timeout time.Duration) ([]judged, Usage, error) {
	var sb strings.Builder
	sb.WriteString("Search results:\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n", i+1, r.URL, r.Title, StripContacts(r.Snippet))
	}
	content, u, _, err := be.Chat(ctx, judgePrompt, sb.String(), maxTokens, timeout)
	if err != nil {
		return nil, u, err
	}
	var out struct {
		Sources []judged `json:"sources"`
	}
	if js := LastJSONObject(content); js != "" {
		_ = json.Unmarshal([]byte(js), &out)
	}
	return out.Sources, u, nil
}

func dedupeResults(results []SearchResult, found map[string]DiscoveredSource) []SearchResult {
	var out []SearchResult
	seen := map[string]bool{}
	for _, r := range results {
		if r.URL == "" || seen[r.URL] {
			continue
		}
		if _, ok := found[r.URL]; ok {
			continue
		}
		seen[r.URL] = true
		out = append(out, r)
	}
	return out
}

func collect(order []string, found map[string]DiscoveredSource) []DiscoveredSource {
	out := make([]DiscoveredSource, 0, len(order))
	for _, url := range order {
		out = append(out, found[url])
	}
	return out
}
