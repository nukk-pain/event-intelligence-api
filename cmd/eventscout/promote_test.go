package main

import (
	"encoding/json"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

func promoteFixtureSources() []agent.DiscoveredSource {
	return []agent.DiscoveredSource{
		{
			URL:      "https://www.coex.co.kr/exhibitions/some-expo",
			Title:    "이미 허용된 행사",
			Reason:   "official venue page",
			Date:     "2026년 10월 1일",
			Location: "서울",
		},
		{
			URL:    `https://newhost.example.org/conf`,
			Title:  `AI "핵심" 컨퍼런스 \ 특수문자`,
			Reason: "official conference page",
		},
		{
			URL:   "://not-a-url",
			Title: "깨진 URL 소스",
		},
	}
}

func TestWritePromotionFilesCreatesReviewArtifacts(t *testing.T) {
	dir := t.TempDir()
	wrote, err := writePromotionFiles(dir, promoteFixtureSources(), []string{"www.coex.co.kr"})
	if err != nil {
		t.Fatalf("writePromotionFiles: %v", err)
	}
	if !wrote {
		t.Fatalf("wrote = false, want true")
	}

	// seed-candidates.jsonl: one row per accepted source, flags carried.
	raw, err := os.ReadFile(filepath.Join(dir, "seed-candidates.jsonl"))
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatalf("jsonl rows = %d, want 3", len(lines))
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("jsonl row not valid JSON: %v", err)
	}
	if first["source_id"] != "benchmark" || first["scout_reason"] != "official venue page" {
		t.Errorf("first row missing seed/provenance fields: %v", first)
	}
	if !strings.Contains(lines[0], "already_allowlisted") {
		t.Errorf("allowlisted host row should carry already_allowlisted flag: %s", lines[0])
	}
	if !strings.Contains(lines[2], "invalid_url") {
		t.Errorf("broken URL row should carry invalid_url flag: %s", lines[2])
	}

	// catalog-snippet.go.txt: valid Go, escaped title, honest empty fields.
	snippet, err := os.ReadFile(filepath.Join(dir, "catalog-snippet.go.txt"))
	if err != nil {
		t.Fatalf("read snippet: %v", err)
	}
	if _, err := format.Source(snippet); err != nil {
		t.Fatalf("snippet is not valid Go: %v", err)
	}
	if !strings.Contains(string(snippet), `AI \"핵심\" 컨퍼런스 \\ 특수문자`) {
		t.Errorf("snippet missing %%q-escaped title:\n%s", snippet)
	}
	if strings.Contains(string(snippet), "not-a-url") {
		t.Errorf("invalid URL source must not reach the snippet:\n%s", snippet)
	}

	// allowlist-hosts.txt: only hosts not already in the production list.
	hosts, err := os.ReadFile(filepath.Join(dir, "allowlist-hosts.txt"))
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	got := strings.Fields(string(hosts))
	if len(got) != 1 || got[0] != "newhost.example.org" {
		t.Errorf("allowlist-hosts = %v, want [newhost.example.org]", got)
	}
}

func TestWritePromotionFilesSlugCollision(t *testing.T) {
	dir := t.TempDir()
	sources := []agent.DiscoveredSource{
		{URL: "https://a.example.org/x", Title: "Same Name 2027"},
		{URL: "https://b.example.org/y", Title: "Same Name 2027"},
	}
	if _, err := writePromotionFiles(dir, sources, nil); err != nil {
		t.Fatalf("writePromotionFiles: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "seed-candidates.jsonl"))
	if !strings.Contains(string(raw), "slug_collision") {
		t.Errorf("second identical title should carry slug_collision flag:\n%s", raw)
	}
	if strings.Count(string(raw), `"event_id":"benchmark-same-name-2027"`) != 1 {
		t.Errorf("colliding rows must not share one event_id:\n%s", raw)
	}
}

// Deep collisions must count from the same base (foo-2, foo-3), never compound
// suffixes (foo-2-3), and a symbol-only title must not produce a bare
// "benchmark-" ID.
func TestWritePromotionFilesDeepCollisionAndEmptyTitle(t *testing.T) {
	dir := t.TempDir()
	sources := []agent.DiscoveredSource{
		{URL: "https://a.example.org/1", Title: "Same 2027"},
		{URL: "https://a.example.org/2", Title: "Same 2027"},
		{URL: "https://a.example.org/3", Title: "Same 2027"},
		{URL: "https://a.example.org/4", Title: "Same 2027"},
		{URL: "https://b.example.org/x", Title: "!!!"},
	}
	if _, err := writePromotionFiles(dir, sources, nil); err != nil {
		t.Fatalf("writePromotionFiles: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "seed-candidates.jsonl"))
	for _, want := range []string{
		`"event_id":"benchmark-same-2027"`,
		`"event_id":"benchmark-same-2027-a-example-org"`,
		`"event_id":"benchmark-same-2027-a-example-org-2"`,
		`"event_id":"benchmark-same-2027-a-example-org-3"`,
		`"event_id":"benchmark-b-example-org"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), `"event_id":"benchmark-"`) {
		t.Errorf("symbol-only title produced a bare benchmark- id:\n%s", raw)
	}
	if strings.Contains(string(raw), "-2-3") {
		t.Errorf("collision suffixes compounded:\n%s", raw)
	}
}

func TestWritePromotionFilesNoSourcesWritesNothing(t *testing.T) {
	dir := t.TempDir()
	wrote, err := writePromotionFiles(dir, nil, nil)
	if err != nil {
		t.Fatalf("writePromotionFiles: %v", err)
	}
	if wrote {
		t.Fatalf("wrote = true, want false for zero sources")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected no files, found %d", len(entries))
	}
}
