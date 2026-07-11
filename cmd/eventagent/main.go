// Command eventagent runs the Solar-backed event-intelligence agent over one
// event: it extracts core facts from the main page, decides which linked pages
// to read, enriches with founder-actionable facts, and prints a brief.
//
// Two input modes:
//
//	-url <event page URL>   real crawl: fetch the page (SSRF-guarded, robots-
//	                        respecting via internal/fetch), extract text + links,
//	                        and fetch only the links the agent chooses to read.
//	-case <dir>             fixture: <dir>/main.txt + <dir>/links/*.txt (first
//	                        line of each link file is its URL).
//
// Backend selection is the same as cmd/abbench (EVENTSINTEL_LOCAL_* by default,
// solar once EVENTSINTEL_SOLAR_API_KEY is set). See .env.example.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/fetch"
)

const (
	maxMainTextChars = 8000 // cap page text fed to the model
	maxLinkTextChars = 6000 // cap linked-page text fed to the model
	maxCandidates    = 40   // cap candidate links offered for selection
)

func main() {
	pageURL := flag.String("url", "", "event page URL to crawl (real mode)")
	caseDir := flag.String("case", "", "fixture case dir (main.txt + links/*.txt)")
	backendName := flag.String("backend", "", "backend name to use (default: first configured)")
	maxTokens := flag.Int("max-tokens", 3000, "max completion tokens")
	timeout := flag.Duration("timeout", 90*time.Second, "per-request timeout")
	flag.Parse()

	if *pageURL == "" && *caseDir == "" {
		*caseDir = "cmd/eventagent/fixtures/ai-conf" // default demo
	}

	be, err := pickBackend(*backendName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var (
		mainText    string
		links       []agent.LinkRef
		linkFetcher agent.LinkFetcher
		label       string
	)
	if *pageURL != "" {
		f, ferr := newFetcher()
		if ferr != nil {
			fmt.Fprintln(os.Stderr, "fetcher:", ferr)
			os.Exit(1)
		}
		mainText, links, err = crawlMain(context.Background(), f, *pageURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, "crawl:", err)
			os.Exit(1)
		}
		linkFetcher = func(ctx context.Context, u string) (string, error) { return crawlText(ctx, f, u) }
		label = *pageURL
	} else {
		mainText, links, err = loadCase(*caseDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "load case:", err)
			os.Exit(1)
		}
		label = filepath.Base(*caseDir)
	}

	fmt.Printf("agent run — src=%s backend=%s(%s) candidate-links=%d\n\n", label, be.Name, be.Model, len(links))
	start := time.Now()
	brief, tr, err := agent.Run(context.Background(), be, mainText, links, linkFetcher, *maxTokens, *timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run:", err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(brief, "", "  ")
	fmt.Println(string(out))
	fmt.Printf("\n%d model call(s), %d in + %d out tokens, %dms total\n",
		tr.Calls, tr.Usage.PromptTokens, tr.Usage.CompletionTokens, time.Since(start).Milliseconds())
}

func pickBackend(name string) (agent.Backend, error) {
	backends := agent.LoadBackends()
	if len(backends) == 0 {
		return agent.Backend{}, fmt.Errorf("no backends configured (set EVENTSINTEL_LOCAL_BASE_URL or EVENTSINTEL_SOLAR_API_KEY)")
	}
	if name == "" {
		return backends[0], nil
	}
	for _, b := range backends {
		if b.Name == name {
			return b, nil
		}
	}
	return agent.Backend{}, fmt.Errorf("backend %q not configured", name)
}

// --- real crawl ---

func newFetcher() (*fetch.Fetcher, error) {
	return fetch.NewFetcher(
		fetch.WithUserAgent("eventsintel-agent/0.1 (+https://events.nukk.net)"),
		fetch.WithAnyPublicHost(true), // arbitrary public event pages; SSRF IP guard still applies
		fetch.WithPerMinute(30),
	)
}

func crawlMain(ctx context.Context, f *fetch.Fetcher, u string) (string, []agent.LinkRef, error) {
	res, err := f.Fetch(ctx, u, fetch.Conditional{})
	if err != nil {
		return "", nil, err
	}
	return extractPage(res.Body, res.URL)
}

func crawlText(ctx context.Context, f *fetch.Fetcher, u string) (string, error) {
	res, err := f.Fetch(ctx, u, fetch.Conditional{})
	if err != nil {
		return "", err
	}
	text, _, err := extractPage(res.Body, res.URL)
	if len(text) > maxLinkTextChars {
		text = text[:maxLinkTextChars]
	}
	return text, err
}

var wsRe = regexp.MustCompile(`\s+`)

// extractPage turns fetched HTML into readable text plus candidate links (with
// anchor text), resolving relative URLs against the final page URL.
func extractPage(body []byte, baseURL string) (string, []agent.LinkRef, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	doc.Find("script, style, noscript").Remove()
	// Lead with title/meta — event names often live in <title>/og:title, not body.
	var head []string
	if t := strings.TrimSpace(doc.Find("title").First().Text()); t != "" {
		head = append(head, "TITLE: "+t)
	}
	for _, sel := range []string{`meta[property="og:title"]`, `meta[property="og:description"]`, `meta[name="description"]`} {
		if c := strings.TrimSpace(doc.Find(sel).AttrOr("content", "")); c != "" {
			head = append(head, c)
		}
	}
	bodyText := wsRe.ReplaceAllString(strings.TrimSpace(doc.Find("body").Text()), " ")
	text := strings.TrimSpace(strings.Join(head, "\n") + "\n" + bodyText)
	if len(text) > maxMainTextChars {
		text = text[:maxMainTextChars]
	}

	base, _ := url.Parse(baseURL)
	seen := map[string]bool{}
	var links []agent.LinkRef
	doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		href := strings.TrimSpace(s.AttrOr("href", ""))
		if href == "" || strings.HasPrefix(href, "#") ||
			strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
			return true
		}
		ref, perr := url.Parse(href)
		if perr != nil {
			return true
		}
		abs := ref
		if base != nil {
			abs = base.ResolveReference(ref)
		}
		if abs.Scheme != "http" && abs.Scheme != "https" {
			return true
		}
		u := abs.String()
		if seen[u] {
			return true
		}
		seen[u] = true
		links = append(links, agent.LinkRef{URL: u, Text: wsRe.ReplaceAllString(strings.TrimSpace(s.Text()), " ")})
		return len(links) < maxCandidates
	})
	return text, links, nil
}

// --- fixture case ---

func loadCase(dir string) (string, []agent.LinkRef, error) {
	mainBytes, err := os.ReadFile(filepath.Join(dir, "main.txt"))
	if err != nil {
		return "", nil, err
	}
	linkFiles, _ := filepath.Glob(filepath.Join(dir, "links", "*.txt"))
	sort.Strings(linkFiles)
	var links []agent.LinkRef
	for _, lf := range linkFiles {
		u, text, err := readLink(lf)
		if err != nil {
			return "", nil, err
		}
		links = append(links, agent.LinkRef{URL: u, Text: text})
	}
	return string(mainBytes), links, nil
}

func readLink(path string) (u, text string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	first := true
	var body []string
	for sc.Scan() {
		if first {
			u = strings.TrimSpace(sc.Text())
			first = false
			continue
		}
		body = append(body, sc.Text())
	}
	return u, strings.Join(body, "\n"), sc.Err()
}
