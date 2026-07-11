// Command eventagent runs the Solar-backed event-intelligence agent over one
// case: it extracts core facts from a main event page, decides which linked
// pages to read, enriches with founder-actionable facts, and prints a brief.
//
// A case is a directory:
//
//	<case>/main.txt        the event page text
//	<case>/links/*.txt     one candidate linked page per file; the FIRST line
//	                       is the URL, the rest is the page text
//
// Backend selection is the same as cmd/abbench (EVENTSINTEL_LOCAL_* by default,
// solar once EVENTSINTEL_SOLAR_API_KEY is set). See .env.example.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

func main() {
	caseDir := flag.String("case", "cmd/eventagent/fixtures/ai-conf", "case directory (main.txt + links/*.txt)")
	backendName := flag.String("backend", "", "backend name to use (default: first configured)")
	maxTokens := flag.Int("max-tokens", 3000, "max completion tokens")
	timeout := flag.Duration("timeout", 90*time.Second, "per-request timeout")
	flag.Parse()

	be, err := pickBackend(*backendName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mainText, links, err := loadCase(*caseDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load case:", err)
		os.Exit(1)
	}

	fmt.Printf("agent run — case=%s backend=%s(%s) links=%d\n\n", filepath.Base(*caseDir), be.Name, be.Model, len(links))
	start := time.Now()
	brief, tr, err := agent.Run(context.Background(), be, mainText, links, *maxTokens, *timeout)
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

func loadCase(dir string) (string, []agent.LinkRef, error) {
	mainBytes, err := os.ReadFile(filepath.Join(dir, "main.txt"))
	if err != nil {
		return "", nil, err
	}
	linkFiles, _ := filepath.Glob(filepath.Join(dir, "links", "*.txt"))
	sort.Strings(linkFiles)
	var links []agent.LinkRef
	for _, lf := range linkFiles {
		url, text, err := readLink(lf)
		if err != nil {
			return "", nil, err
		}
		links = append(links, agent.LinkRef{URL: url, Text: text})
	}
	return string(mainBytes), links, nil
}

// readLink reads a link file whose first line is the URL and the rest is text.
func readLink(path string) (url, text string, err error) {
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
			url = strings.TrimSpace(sc.Text())
			first = false
			continue
		}
		body = append(body, sc.Text())
	}
	return url, strings.Join(body, "\n"), sc.Err()
}
