// Command abbench is a small A/B harness for the Solar-agent task: it measures
// how well different LLM backends extract structured event facts from raw,
// unstructured Korean event text, and what each extraction costs in tokens.
//
// It is decoupled from the production pipeline on purpose (no internal imports)
// so it can run standalone against any OpenAI-compatible endpoint. Configure
// backends via env (see cmd/abbench/README.md and .env.example):
//
//   - local  : EVENTSINTEL_LOCAL_BASE_URL (default http://127.0.0.1:18900/v1),
//              EVENTSINTEL_LOCAL_MODEL (default qwen36-dwq). Set _BASE_URL=off to skip.
//   - solar  : EVENTSINTEL_SOLAR_BASE_URL (default https://api.upstage.ai/v1),
//              EVENTSINTEL_SOLAR_MODEL, EVENTSINTEL_SOLAR_API_KEY. Included only
//              when the API key is set (i.e. once Open 2 Early Access is live).
//
// Compliance: per the Upstage data policy, contact details (phone/email) are
// stripped from the input BEFORE it is sent to any model. The harness only ever
// scores event facts, never personal contact info.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// fields is the extraction target: the subset of event facts an LLM reads out
// of prose. Order is fixed so scoring and reporting are deterministic.
var fields = []string{
	"name", "name_en", "start_raw", "end_raw",
	"venue_name", "city", "organizer", "host", "homepage_url",
}

// dateFields and urlFields get type-specific normalization during scoring.
var dateFields = map[string]bool{"start_raw": true, "end_raw": true}
var urlFields = map[string]bool{"homepage_url": true}

type backend struct {
	name    string
	baseURL string
	apiKey  string
	model   string
}

type caseResult struct {
	correct   int
	total     int
	promptTok int
	compTok   int
	latency   time.Duration
	err       error
}

func main() {
	fixturesDir := flag.String("fixtures", "cmd/abbench/fixtures", "directory of *.input.txt + *.gold.json cases")
	maxTokens := flag.Int("max-tokens", 3000, "max completion tokens (reasoning models need headroom)")
	timeout := flag.Duration("timeout", 90*time.Second, "per-request timeout")
	flag.Parse()

	backends := loadBackends()
	if len(backends) == 0 {
		fmt.Fprintln(os.Stderr, "no backends configured (set EVENTSINTEL_LOCAL_BASE_URL or EVENTSINTEL_SOLAR_API_KEY)")
		os.Exit(1)
	}
	cases, err := loadCases(*fixturesDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load fixtures:", err)
		os.Exit(1)
	}
	if len(cases) == 0 {
		fmt.Fprintf(os.Stderr, "no cases found in %s\n", *fixturesDir)
		os.Exit(1)
	}

	fmt.Printf("A/B extraction benchmark — %d case(s), %d backend(s)\n\n", len(cases), len(backends))

	type agg struct {
		correct, total, prompt, comp, runs int
		latency                            time.Duration
		fails                              int
	}
	totals := map[string]*agg{}

	for _, b := range backends {
		totals[b.name] = &agg{}
		for _, c := range cases {
			r := runCase(context.Background(), b, c, *maxTokens, *timeout)
			a := totals[b.name]
			if r.err != nil {
				a.fails++
				fmt.Printf("  [%s] %-22s ERROR: %v\n", b.name, c.name, r.err)
				continue
			}
			a.correct += r.correct
			a.total += r.total
			a.prompt += r.promptTok
			a.comp += r.compTok
			a.latency += r.latency
			a.runs++
			fmt.Printf("  [%s] %-22s %d/%d fields  in=%d out=%d  %dms\n",
				b.name, c.name, r.correct, r.total, r.promptTok, r.compTok, r.latency.Milliseconds())
		}
	}

	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "backend\tfield acc\tavg in tok\tavg out tok\tavg latency\tfails")
	fmt.Fprintln(w, "-------\t---------\t----------\t-----------\t-----------\t-----")
	for _, b := range backends {
		a := totals[b.name]
		acc, avgIn, avgOut, avgLat := "-", "-", "-", "-"
		if a.total > 0 {
			acc = fmt.Sprintf("%.1f%%", 100*float64(a.correct)/float64(a.total))
		}
		if a.runs > 0 {
			avgIn = fmt.Sprintf("%d", a.prompt/a.runs)
			avgOut = fmt.Sprintf("%d", a.comp/a.runs)
			avgLat = fmt.Sprintf("%dms", (a.latency / time.Duration(a.runs)).Milliseconds())
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n", b.name, acc, avgIn, avgOut, avgLat, a.fails)
	}
	w.Flush()
	fmt.Println("\nNote: 'avg out tok' is the per-event cost proxy. Multiply by your\nper-token price to get won-per-event. Contact info is stripped pre-send.")
}

func loadBackends() []backend {
	var out []backend

	localURL := getenv("EVENTSINTEL_LOCAL_BASE_URL", "http://127.0.0.1:18900/v1")
	if !strings.EqualFold(localURL, "off") {
		out = append(out, backend{
			name:    "local",
			baseURL: localURL,
			model:   getenv("EVENTSINTEL_LOCAL_MODEL", "qwen36-dwq"),
		})
	}
	if key := os.Getenv("EVENTSINTEL_SOLAR_API_KEY"); key != "" {
		out = append(out, backend{
			name:    "solar",
			baseURL: getenv("EVENTSINTEL_SOLAR_BASE_URL", "https://api.upstage.ai/v1"),
			apiKey:  key,
			model:   getenv("EVENTSINTEL_SOLAR_MODEL", "solar-pro"),
		})
	}
	return out
}

type evCase struct {
	name  string
	input string
	gold  map[string]any
}

func loadCases(dir string) ([]evCase, error) {
	inputs, err := filepath.Glob(filepath.Join(dir, "*.input.txt"))
	if err != nil {
		return nil, err
	}
	sort.Strings(inputs)
	var cases []evCase
	for _, in := range inputs {
		base := strings.TrimSuffix(filepath.Base(in), ".input.txt")
		goldPath := filepath.Join(dir, base+".gold.json")
		raw, err := os.ReadFile(in)
		if err != nil {
			return nil, err
		}
		goldRaw, err := os.ReadFile(goldPath)
		if err != nil {
			return nil, fmt.Errorf("%s: missing gold file %s", base, goldPath)
		}
		var gold map[string]any
		if err := json.Unmarshal(goldRaw, &gold); err != nil {
			return nil, fmt.Errorf("%s: bad gold json: %w", base, err)
		}
		cases = append(cases, evCase{name: base, input: string(raw), gold: gold})
	}
	return cases, nil
}

func runCase(ctx context.Context, b backend, c evCase, maxTokens int, timeout time.Duration) caseResult {
	clean := stripContacts(c.input) // compliance: never send phone/email to the model
	content, promptTok, compTok, lat, err := chat(ctx, b, extractionPrompt, clean, maxTokens, timeout)
	if err != nil {
		return caseResult{err: err}
	}
	out, err := parseEvent(content)
	if err != nil {
		return caseResult{err: fmt.Errorf("parse model json: %w", err), promptTok: promptTok, compTok: compTok, latency: lat}
	}
	correct, total := score(c.gold, out)
	return caseResult{correct: correct, total: total, promptTok: promptTok, compTok: compTok, latency: lat}
}

const extractionPrompt = `You extract structured event facts from Korean event announcements.
Return ONLY a JSON object with exactly these keys:
name, name_en, start_raw, end_raw, venue_name, city, organizer, host, homepage_url.

Rules:
- Use only facts explicitly present in the text. Never guess or invent.
- If a field is absent, set it to null.
- organizer is 주최; host is 주관. Do NOT treat 후원 (sponsor) as organizer or host.
- Keep dates as they appear but resolve the full date when the year/month is clear.
- Output the JSON object only, no prose.`

// --- OpenAI-compatible chat client (stdlib only) ---

func chat(ctx context.Context, b backend, system, user string, maxTokens int, timeout time.Duration) (string, int, int, time.Duration, error) {
	reqBody := map[string]any{
		"model":       b.model,
		"temperature": 0,
		"max_tokens":  maxTokens,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	buf, _ := json.Marshal(reqBody)

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, strings.TrimRight(b.baseURL, "/")+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", 0, 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, 0, 0, err
	}
	defer resp.Body.Close()
	lat := time.Since(start)

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, 0, lat, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", out.Usage.PromptTokens, out.Usage.CompletionTokens, lat, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(out.Error)))
	}
	if len(out.Choices) == 0 {
		return "", out.Usage.PromptTokens, out.Usage.CompletionTokens, lat, fmt.Errorf("no choices in response")
	}
	return out.Choices[0].Message.Content, out.Usage.PromptTokens, out.Usage.CompletionTokens, lat, nil
}

// parseEvent pulls the last balanced JSON object out of the model content
// (reasoning models emit thinking before the final answer) and unmarshals it.
func parseEvent(content string) (map[string]any, error) {
	js := lastJSONObject(content)
	if js == "" {
		return nil, fmt.Errorf("no JSON object found in output")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func lastJSONObject(s string) string {
	depth, start, best := 0, -1, ""
	for i, r := range s {
		switch r {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					best = s[start : i+1] // keep last complete object
				}
			}
		}
	}
	return best
}

// --- scoring ---

func score(gold, out map[string]any) (correct, total int) {
	for _, f := range fields {
		total++
		if normField(f, gold[f]) == normField(f, out[f]) {
			correct++
		}
	}
	return correct, total
}

var digits = regexp.MustCompile(`\d+`)
var ws = regexp.MustCompile(`\s+`)

func normField(field string, v any) string {
	s := stringify(v)
	switch {
	case dateFields[field]:
		return strings.Join(digits.FindAllString(s, -1), "") // compare by digit sequence
	case urlFields[field]:
		return strings.TrimRight(strings.ToLower(strings.TrimSpace(s)), "/")
	default:
		return ws.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), " ")
	}
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

// --- compliance: strip contact info before sending to any model ---

var (
	reEmail = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	rePhone = regexp.MustCompile(`0\d{1,2}[-\s]?\d{3,4}[-\s]?\d{4}`)
)

func stripContacts(s string) string {
	s = reEmail.ReplaceAllString(s, "[email removed]")
	s = rePhone.ReplaceAllString(s, "[phone removed]")
	return s
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
