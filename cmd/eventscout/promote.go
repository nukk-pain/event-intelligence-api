package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/sources/benchmark"
)

// Promotion turns an accepted discovery run into review artifacts, not into
// live configuration: a human moves the snippet into the benchmark catalog and
// the host list through normal code review. Empty fields stay empty on purpose
// — the reviewer fills them from the official page, the tool never guesses.

type seedCandidate struct {
	SourceID    string   `json:"source_id"`
	EventID     string   `json:"event_id"`
	Name        string   `json:"name"`
	OfficialURL string   `json:"official_url"`
	DateText    string   `json:"date_text,omitempty"`
	Location    string   `json:"location,omitempty"`
	ScoutReason string   `json:"scout_reason,omitempty"`
	Flags       []string `json:"flags,omitempty"`
}

// writePromotionFiles writes seed-candidates.jsonl, catalog-snippet.go.txt,
// and allowlist-hosts.txt into dir. It reports whether anything was written:
// zero accepted sources produce no files so an empty run cannot be mistaken
// for an empty-but-successful review packet.
func writePromotionFiles(dir string, sources []agent.DiscoveredSource, existingHosts []string) (bool, error) {
	if len(sources) == 0 {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("create promote dir: %w", err)
	}

	allowed := make(map[string]bool, len(existingHosts))
	for _, h := range existingHosts {
		allowed[strings.ToLower(h)] = true
	}
	usedSlugs := make(map[string]bool)
	for _, id := range benchmark.CatalogEventIDs() {
		usedSlugs[id] = true
	}

	var jsonlRows []string
	var snippetEntries []string
	newHosts := make(map[string]bool)

	for _, s := range sources {
		host, hostOK := candidateHost(s.URL)
		var flags []string
		if !hostOK {
			flags = append(flags, "invalid_url")
		} else if allowed[host] {
			flags = append(flags, "already_allowlisted")
		} else {
			newHosts[host] = true
		}

		base := slugify(s.Title)
		if base == "" {
			// A symbol-only title must still yield a reviewable ID; the host is
			// the only other stable identity we have.
			if hostOK {
				base = slugify(host)
			} else {
				base = "untitled"
			}
		}
		eventID := "benchmark-" + base
		if usedSlugs[eventID] {
			flags = append(flags, "slug_collision")
			if hostOK && !strings.HasSuffix(eventID, "-"+slugify(host)) {
				eventID = eventID + "-" + slugify(host)
			}
			// Count from a fixed stem so deep collisions yield stem-2, stem-3,
			// never compounding stem-2-3.
			stem := eventID
			for n := 2; usedSlugs[eventID]; n++ {
				eventID = fmt.Sprintf("%s-%d", stem, n)
			}
		}
		usedSlugs[eventID] = true

		row, err := json.Marshal(seedCandidate{
			SourceID:    "benchmark",
			EventID:     eventID,
			Name:        s.Title,
			OfficialURL: s.URL,
			DateText:    s.Date,
			Location:    s.Location,
			ScoutReason: s.Reason,
			Flags:       flags,
		})
		if err != nil {
			return false, fmt.Errorf("encode candidate %q: %w", s.URL, err)
		}
		jsonlRows = append(jsonlRows, string(row))

		if hostOK {
			snippetEntries = append(snippetEntries, catalogEntrySnippet(eventID, s))
		}
	}

	snippet, err := renderSnippet(snippetEntries)
	if err != nil {
		return false, err
	}

	hosts := make([]string, 0, len(newHosts))
	for h := range newHosts {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	files := map[string]string{
		"seed-candidates.jsonl":  strings.Join(jsonlRows, "\n") + "\n",
		"catalog-snippet.go.txt": snippet,
		"allowlist-hosts.txt":    strings.Join(hosts, "\n") + "\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return false, fmt.Errorf("write %s: %w", name, err)
		}
	}
	return true, nil
}

// candidateHost extracts the normalized (lowercase, portless) host of a
// candidate URL. Sources whose URL cannot yield a host are kept in the review
// packet but excluded from the snippet and the allowlist diff.
func candidateHost(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	return strings.ToLower(u.Hostname()), true
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func catalogEntrySnippet(eventID string, s agent.DiscoveredSource) string {
	notes := "eventscout: " + s.Reason
	if s.Location != "" {
		notes += "; location " + s.Location
	}
	return fmt.Sprintf(`	{
		EventID:  %q,
		URL:      %q,
		Name:     %q,
		StartRaw: %q,
		Notes:    %q,
	},`, eventID, s.URL, s.Title, s.Date, notes)
}

// renderSnippet assembles a full Go file body and refuses to emit anything
// gofmt cannot parse, so a paste into the catalog can fail only on review
// content, never on syntax.
func renderSnippet(entries []string) (string, error) {
	src := `// Candidate benchmark catalog entries generated by eventscout -promote.
// Review each against its official page before committing: fill missing
// fields from the page, delete candidates that do not belong, and wire the
// var into the catalog append in catalog.go. Empty fields are unknowns, not
// defaults. Do not fill a value the official page does not state.
package benchmark

var promotedCatalog = []catalogEvent{
` + strings.Join(entries, "\n") + `
}
`
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return "", fmt.Errorf("generated snippet is not valid Go: %w", err)
	}
	return string(formatted), nil
}
