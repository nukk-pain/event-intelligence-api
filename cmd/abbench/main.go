// Command abbench is an A/B harness for the Solar-agent task: it measures how
// well different LLM backends extract structured event facts from raw Korean
// event text, and what each extraction costs in tokens. It reuses the shared
// internal/agent extraction (same prompt/client the real agent uses), so the
// benchmark tracks the agent's actual behavior.
//
// Backends and contact-stripping come from internal/agent; configure via env
// (see .env.example). Runs on the local backend today; the solar column appears
// once EVENTSINTEL_SOLAR_API_KEY is set (Open 2 Early Access, from 2026-07-17).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

// scoreFields is what the benchmark grades. hall is extracted by the agent but
// not graded here (it is fuzzy); venue_name is graded venue-only.
var scoreFields = []string{
	"name", "name_en", "start_raw", "end_raw",
	"venue_name", "city", "organizer", "host", "homepage_url",
}

var dateFields = map[string]bool{"start_raw": true, "end_raw": true}
var urlFields = map[string]bool{"homepage_url": true}

type miss struct{ field, want, got string }

func main() {
	fixturesDir := flag.String("fixtures", "cmd/abbench/fixtures", "directory of *.input.txt + *.gold.json cases")
	maxTokens := flag.Int("max-tokens", 3000, "max completion tokens (reasoning models need headroom)")
	timeout := flag.Duration("timeout", 90*time.Second, "per-request timeout")
	verbose := flag.Bool("v", false, "print each wrong field (want vs got)")
	flag.Parse()

	backends := agent.LoadBackends()
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
		correct, total, prompt, comp, runs, fails int
		latency                                   time.Duration
	}
	totals := map[string]*agg{}

	for _, b := range backends {
		totals[b.Name] = &agg{}
		for _, c := range cases {
			facts, u, lat, err := agent.Extract(context.Background(), b, c.input, *maxTokens, *timeout)
			a := totals[b.Name]
			if err != nil {
				a.fails++
				fmt.Printf("  [%s] %-22s ERROR: %v\n", b.Name, c.name, err)
				continue
			}
			correct, total, misses := score(c.gold, facts)
			a.correct += correct
			a.total += total
			a.prompt += u.PromptTokens
			a.comp += u.CompletionTokens
			a.latency += lat
			a.runs++
			fmt.Printf("  [%s] %-22s %d/%d fields  in=%d out=%d  %dms\n",
				b.Name, c.name, correct, total, u.PromptTokens, u.CompletionTokens, lat.Milliseconds())
			if *verbose {
				for _, m := range misses {
					fmt.Printf("        ✗ %-12s want=%q got=%q\n", m.field, m.want, m.got)
				}
			}
		}
	}

	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "backend\tfield acc\tavg in tok\tavg out tok\tavg latency\tfails")
	fmt.Fprintln(w, "-------\t---------\t----------\t-----------\t-----------\t-----")
	for _, b := range backends {
		a := totals[b.Name]
		acc, avgIn, avgOut, avgLat := "-", "-", "-", "-"
		if a.total > 0 {
			acc = fmt.Sprintf("%.1f%%", 100*float64(a.correct)/float64(a.total))
		}
		if a.runs > 0 {
			avgIn = fmt.Sprintf("%d", a.prompt/a.runs)
			avgOut = fmt.Sprintf("%d", a.comp/a.runs)
			avgLat = fmt.Sprintf("%dms", (a.latency / time.Duration(a.runs)).Milliseconds())
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n", b.Name, acc, avgIn, avgOut, avgLat, a.fails)
	}
	w.Flush()
	fmt.Println("\nNote: 'avg out tok' is the per-event cost proxy. Contact info is stripped pre-send.")
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
		raw, err := os.ReadFile(in)
		if err != nil {
			return nil, err
		}
		goldRaw, err := os.ReadFile(filepath.Join(dir, base+".gold.json"))
		if err != nil {
			return nil, fmt.Errorf("%s: missing gold file", base)
		}
		var gold map[string]any
		if err := json.Unmarshal(goldRaw, &gold); err != nil {
			return nil, fmt.Errorf("%s: bad gold json: %w", base, err)
		}
		cases = append(cases, evCase{name: base, input: string(raw), gold: gold})
	}
	return cases, nil
}

func score(gold, out map[string]any) (correct, total int, misses []miss) {
	for _, f := range scoreFields {
		total++
		if fieldMatch(f, gold[f], out[f]) {
			correct++
		} else {
			misses = append(misses, miss{field: f, want: stringify(gold[f]), got: stringify(out[f])})
		}
	}
	return correct, total, misses
}

// fieldMatch compares one field with type-aware normalization. venue_name uses
// containment (gold venue is correct even if the model appended a hall/room).
func fieldMatch(field string, gold, got any) bool {
	w, g := normField(field, gold), normField(field, got)
	if field == "venue_name" && w != "" {
		return strings.Contains(g, w)
	}
	return w == g
}

var digits = regexp.MustCompile(`\d+`)
var ws = regexp.MustCompile(`\s+`)

func normField(field string, v any) string {
	s := stringify(v)
	switch {
	case dateFields[field]:
		return strings.Join(digits.FindAllString(s, -1), "")
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
