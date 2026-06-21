package sources

import (
	"context"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

// fakeSource is a minimal Source implementation used to prove the interface is
// satisfiable and to exercise the registry round-trip. It must compile against
// the Source interface or the build fails.
type fakeSource struct {
	id      string
	refs    []Ref
	parsed  *ParsedEvent
	discErr error
	parErr  error
}

func (s *fakeSource) ID() string { return s.id }

func (s *fakeSource) Discover(ctx context.Context, f *fetch.Fetcher) ([]Ref, error) {
	return s.refs, s.discErr
}

func (s *fakeSource) Parse(ctx context.Context, raw *fetch.Result) (*ParsedEvent, error) {
	return s.parsed, s.parErr
}

// Compile-time assertion that fakeSource satisfies Source.
var _ Source = (*fakeSource)(nil)

// strptr is a tiny helper for nullable string fields.
func strptr(s string) *string { return &s }

func TestSourceInterfaceSatisfied(t *testing.T) {
	// A *fakeSource must be assignable to Source. The package-level var above
	// already guarantees this at compile time; this test documents intent and
	// exercises the methods at runtime.
	var s Source = &fakeSource{id: "fake"}

	if got := s.ID(); got != "fake" {
		t.Fatalf("ID() = %q, want %q", got, "fake")
	}

	refs, err := s.Discover(context.Background(), nil)
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil", err)
	}
	if refs != nil {
		t.Fatalf("Discover() refs = %v, want nil", refs)
	}

	pe, err := s.Parse(context.Background(), nil)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if pe != nil {
		t.Fatalf("Parse() = %v, want nil", pe)
	}
}

func TestParsedEventRawFields(t *testing.T) {
	// ParsedEvent must hold the RAW extracted fields prior to normalization.
	// This asserts the shape (field names/types) the parsers will populate.
	pe := ParsedEvent{
		SourceID:     "coex",
		EventID:      "coex-some-slug",
		URL:          "https://www.coex.co.kr/exhibitions/some-slug",
		Name:         "어떤 박람회",
		NameKo:       strptr("어떤 박람회"),
		NameEn:       strptr("Some Expo"),
		Edition:      strptr("제5회"),
		StartRaw:     strptr("2024.08.24"),
		EndRaw:       strptr("2024.08.27"),
		VenueName:    strptr("코엑스"),
		Hall:         strptr("Hall A"),
		City:         strptr("서울"),
		Organizer:    strptr("주최사"),
		Host:         strptr("주관사"),
		HomepageURL:  strptr("https://example.com"),
		ClassifyText: "어떤 박람회 주최사",
		RetrievedAt:  "2026-06-21T00:00:00Z",
	}

	if pe.SourceID != "coex" {
		t.Errorf("SourceID = %q, want %q", pe.SourceID, "coex")
	}
	if pe.EventID != "coex-some-slug" {
		t.Errorf("EventID = %q, want %q", pe.EventID, "coex-some-slug")
	}
	if pe.NameEn == nil || *pe.NameEn != "Some Expo" {
		t.Errorf("NameEn = %v, want %q", pe.NameEn, "Some Expo")
	}
	if pe.StartRaw == nil || *pe.StartRaw != "2024.08.24" {
		t.Errorf("StartRaw = %v, want %q", pe.StartRaw, "2024.08.24")
	}
	if pe.ClassifyText != "어떤 박람회 주최사" {
		t.Errorf("ClassifyText = %q, want %q", pe.ClassifyText, "어떤 박람회 주최사")
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	// Save and restore the package-level registry so this test is hermetic.
	saved := registry
	registry = map[string]Source{}
	t.Cleanup(func() { registry = saved })

	coex := &fakeSource{id: "coex"}
	kintex := &fakeSource{id: "kintex"}

	Register(coex)
	Register(kintex)

	got, ok := Get("coex")
	if !ok {
		t.Fatalf("Get(%q) ok = false, want true", "coex")
	}
	if got != Source(coex) {
		t.Fatalf("Get(%q) returned a different instance", "coex")
	}

	if _, ok := Get("kintex"); !ok {
		t.Fatalf("Get(%q) ok = false, want true", "kintex")
	}

	if _, ok := Get("missing"); ok {
		t.Fatalf("Get(%q) ok = true, want false", "missing")
	}

	all := All()
	if len(all) != 2 {
		t.Fatalf("All() len = %d, want 2", len(all))
	}
	if all["coex"] != Source(coex) || all["kintex"] != Source(kintex) {
		t.Fatalf("All() did not contain the registered sources")
	}
}

func TestRegisterOverwrites(t *testing.T) {
	saved := registry
	registry = map[string]Source{}
	t.Cleanup(func() { registry = saved })

	first := &fakeSource{id: "dup"}
	second := &fakeSource{id: "dup"}

	Register(first)
	Register(second)

	got, ok := Get("dup")
	if !ok {
		t.Fatalf("Get(%q) ok = false, want true", "dup")
	}
	if got != Source(second) {
		t.Fatalf("Register did not overwrite the earlier registration")
	}
	if len(All()) != 1 {
		t.Fatalf("All() len = %d, want 1", len(All()))
	}
}
