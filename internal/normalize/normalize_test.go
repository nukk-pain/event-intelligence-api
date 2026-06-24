package normalize

import (
	"context"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/classify"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/sources"
	"github.com/smpain/event-intelligence-api/internal/sources/coex"
)

const now = "2026-06-21T00:00:00Z"

func ptr(s string) *string { return &s }

// minimal ParsedEvent that normalizes cleanly: industry category, full
// when/where, one parseable date pair, organizer + homepage present.
func validParsed() *sources.ParsedEvent {
	return &sources.ParsedEvent{
		SourceID:     "coex",
		EventID:      "coex-ai-expo-2026",
		URL:          "https://www.coex.co.kr/exhibitions/ai-expo-2026/",
		Name:         "AI EXPO KOREA 2026",
		StartRaw:     ptr("2026.05.01"),
		EndRaw:       ptr("2026.05.03"),
		VenueName:    ptr("코엑스"),
		Hall:         ptr("Hall C"),
		City:         ptr("서울"),
		Organizer:    ptr("AI산업협회"),
		HomepageURL:  ptr("https://aiexpo.example.org"),
		SummaryText:  ptr("인공지능 산업의 최신 기술과 실제 비즈니스 적용 사례를 공유하는 전문 전시회입니다."),
		ClassifyText: "AI EXPO KOREA 2026 AI산업협회 인공지능",
		RetrievedAt:  "2026-06-21T00:00:00Z",
	}
}

// An aggregator source (showala) carrying a KINTEX event must resolve venue_id
// to "kintex" by venue NAME, so its cross-source dedup key aligns with the kintex
// adapter's own row.
func TestNormalize_AggregatorVenueIDByName(t *testing.T) {
	p := validParsed()
	p.SourceID = "showala"
	p.EventID = "showala-3219"
	p.URL = "https://showala.com/ex/ex_detail.php?idx=3219"
	p.VenueName = ptr("킨텍스 (KINTEX)")
	p.City = nil

	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.Venue == nil || e.Venue.VenueID == nil {
		t.Fatalf("venue_id not set for showala KINTEX event")
	}
	if *e.Venue.VenueID != "kintex" {
		t.Fatalf("venue_id = %q, want kintex", *e.Venue.VenueID)
	}
}

// Regression: a single-venue source still maps venue_id by its adapter id (not by
// the scraped name), regardless of how the venue name reads.
func TestNormalize_SingleVenueSourceMapsByID(t *testing.T) {
	p := validParsed() // SourceID "coex"
	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.Venue == nil || e.Venue.VenueID == nil || *e.Venue.VenueID != "coex" {
		t.Fatalf("coex venue_id mapping regressed: %+v", e.Venue)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Golden: real COEX page -> COEX parser -> normalize -> assert full struct
// ---------------------------------------------------------------------------

func TestNormalize_GoldenCOEX(t *testing.T) {
	body, err := os.ReadFile("testdata/coex-detail.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	raw := &fetch.Result{
		URL:         "https://www.coex.co.kr/exhibitions/글로벌헬스케어&메디컬코리아-2011/",
		StatusCode:  200,
		ContentType: "text/html; charset=utf-8",
		Body:        body,
	}
	pe, err := (&coex.Source{}).Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("coex parse: %v", err)
	}
	// The COEX adapter leaves RetrievedAt empty; normalize must fall back to now.
	if pe.RetrievedAt != "" {
		t.Fatalf("precondition: expected COEX parser to leave RetrievedAt empty, got %q", pe.RetrievedAt)
	}

	e, err := Normalize(pe, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if e.SchemaVersion != model.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", e.SchemaVersion, model.SchemaVersion)
	}
	if e.EventID != pe.EventID {
		t.Errorf("event_id = %q, want %q", e.EventID, pe.EventID)
	}
	if e.Name != "글로벌헬스케어&메디컬코리아 2011" {
		t.Errorf("name = %q", e.Name)
	}
	if e.Country != "KR" {
		t.Errorf("country = %q, want KR", e.Country)
	}
	if e.StartDate == nil || *e.StartDate != "2011-04-12" {
		t.Errorf("start_date = %v, want 2011-04-12", e.StartDate)
	}
	if e.EndDate == nil || *e.EndDate != "2011-04-14" {
		t.Errorf("end_date = %v, want 2011-04-14", e.EndDate)
	}
	if e.DateConfidence != "high" {
		t.Errorf("date_confidence = %q, want high (dates parsed)", e.DateConfidence)
	}
	if e.Venue == nil {
		t.Fatal("venue is nil")
	}
	if e.Venue.VenueID == nil || *e.Venue.VenueID != "coex" {
		t.Errorf("venue.venue_id = %v, want coex", e.Venue.VenueID)
	}
	if e.Venue.Name != "코엑스" || e.Venue.City != "서울" {
		t.Errorf("venue name/city = %q/%q", e.Venue.Name, e.Venue.City)
	}
	if e.Venue.Hall == nil || *e.Venue.Hall != "그랜드볼룸" {
		t.Errorf("venue.hall = %v, want 그랜드볼룸", e.Venue.Hall)
	}
	if !reflect.DeepEqual(e.Categories, []string{classify.CategoryDigitalHealth}) {
		t.Errorf("categories = %v, want [digital-health]", e.Categories)
	}
	if e.Confidence != "high" {
		t.Errorf("confidence = %q, want high", e.Confidence)
	}
	if e.Excluded {
		t.Error("excluded = true, want false")
	}
	if e.HomepageURL == nil || *e.HomepageURL != "http://www.medical-korea.org" {
		t.Errorf("homepage_url = %v", e.HomepageURL)
	}
	if e.UpdateState != "" {
		t.Errorf("update_state = %q, want empty (set by store/diff, not normalize)", e.UpdateState)
	}
	if e.LastCheckedAt != now {
		t.Errorf("last_checked_at = %q, want %q", e.LastCheckedAt, now)
	}

	if e.Summary != nil {
		t.Fatalf("summary = %q, want nil because the fixture has no source description field", *e.Summary)
	}

	// Sources: exactly one venue source with the 4 required sub-fields, and
	// retrieved_at falls back to `now` because the parser left it empty.
	if len(e.Sources) != 1 {
		t.Fatalf("sources len = %d, want 1", len(e.Sources))
	}
	src := e.Sources[0]
	if !strings.HasPrefix(src.URL, "http") {
		t.Errorf("source url not http(s): %q", src.URL)
	}
	if src.Type != "venue" {
		t.Errorf("source type = %q, want venue", src.Type)
	}
	if src.Publisher == "" {
		t.Error("source publisher empty")
	}
	if src.RetrievedAt != now {
		t.Errorf("source retrieved_at = %q, want now %q", src.RetrievedAt, now)
	}

	// Action fields are absent on the venue page -> every actions.* key plus the
	// *_url / *_deadline keys must be in missing_fields (honesty rule).
	for _, want := range []string{
		"actions.can_register", "actions.can_exhibit", "actions.can_sponsor",
		"actions.has_matchmaking", "actions.has_startup_program",
		"register_url", "exhibit_url", "registration_deadline", "exhibitor_deadline",
	} {
		if !contains(e.MissingFields, want) {
			t.Errorf("missing_fields should contain %q; got %v", want, e.MissingFields)
		}
	}
	if !contains(e.MissingFields, "summary") {
		t.Errorf("source summary absent; missing_fields must contain summary: %v", e.MissingFields)
	}

	// The normalized record must pass the v0.1 validator (rules 1-5).
	if err := Validate(e); err != nil {
		t.Fatalf("golden record failed validation: %v", err)
	}
}

// ---------------------------------------------------------------------------
// actions honesty: fully-missing-action page -> all actions.* in missing_fields
// ---------------------------------------------------------------------------

func TestNormalize_ActionsAllMissing(t *testing.T) {
	e, err := Normalize(validParsed(), now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	for _, k := range []string{
		"actions.can_register", "actions.can_exhibit", "actions.can_sponsor",
		"actions.has_matchmaking", "actions.has_startup_program",
		"register_url", "exhibit_url", "registration_deadline", "exhibitor_deadline",
	} {
		if !contains(e.MissingFields, k) {
			t.Errorf("missing_fields missing %q: %v", k, e.MissingFields)
		}
	}
	// All action booleans default false (unknown).
	if e.Actions != (model.Actions{}) {
		t.Errorf("actions should all be false default, got %+v", e.Actions)
	}
}

func TestNormalize_UsesOnlySourceSummaryText(t *testing.T) {
	e, err := Normalize(validParsed(), now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.Summary == nil {
		t.Fatal("summary is nil; want source summary text")
	}
	if got, want := *e.Summary, "인공지능 산업의 최신 기술과 실제 비즈니스 적용 사례를 공유하는 전문 전시회입니다."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"2026-05-01", "2026-05-03", " @ ", "주최"} {
		if strings.Contains(*e.Summary, forbidden) {
			t.Errorf("summary contains generated template part %q: %q", forbidden, *e.Summary)
		}
	}
	if contains(e.MissingFields, "summary") {
		t.Errorf("summary was produced; must not be in missing_fields: %v", e.MissingFields)
	}
}

func TestNormalize_MissingSourceSummaryLeavesSummaryNull(t *testing.T) {
	p := validParsed()
	p.SummaryText = nil

	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.Summary != nil {
		t.Fatalf("summary = %q, want nil when source text is absent", *e.Summary)
	}
	if !contains(e.MissingFields, "summary") {
		t.Fatalf("missing_fields = %v, want summary", e.MissingFields)
	}
}

// ---------------------------------------------------------------------------
// unparseable date -> null + missing_fields + date_confidence=low
// ---------------------------------------------------------------------------

func TestNormalize_UnparseableDate(t *testing.T) {
	p := validParsed()
	p.StartRaw = ptr("미정")
	p.EndRaw = ptr("추후공지")

	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.StartDate != nil {
		t.Errorf("start_date = %v, want nil (unparseable)", *e.StartDate)
	}
	if e.EndDate != nil {
		t.Errorf("end_date = %v, want nil (unparseable)", *e.EndDate)
	}
	if e.DateConfidence != "low" {
		t.Errorf("date_confidence = %q, want low", e.DateConfidence)
	}
	if !contains(e.MissingFields, "start_date") || !contains(e.MissingFields, "end_date") {
		t.Errorf("missing_fields must contain start_date and end_date: %v", e.MissingFields)
	}
}

// ---------------------------------------------------------------------------
// end < start -> both nulled, missing_fields, low confidence, ambiguity_notes
// ---------------------------------------------------------------------------

func TestNormalize_EndBeforeStart(t *testing.T) {
	p := validParsed()
	p.StartRaw = ptr("2026.05.10")
	p.EndRaw = ptr("2026.05.01")

	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.StartDate != nil || e.EndDate != nil {
		t.Errorf("end<start: both dates must be nulled, got start=%v end=%v", e.StartDate, e.EndDate)
	}
	if e.DateConfidence != "low" {
		t.Errorf("date_confidence = %q, want low", e.DateConfidence)
	}
	if !contains(e.MissingFields, "start_date") || !contains(e.MissingFields, "end_date") {
		t.Errorf("missing_fields must contain both date fields: %v", e.MissingFields)
	}
	if e.AmbiguityNotes == nil || *e.AmbiguityNotes == "" {
		t.Error("end<start must set ambiguity_notes")
	}
}

// ---------------------------------------------------------------------------
// rejection: invalid record (no usable source / category) -> error, not stored
// ---------------------------------------------------------------------------

func TestNormalize_RejectNoSource(t *testing.T) {
	p := validParsed()
	p.URL = "" // no detail URL -> cannot build a source -> rule 5 fails
	if _, err := Normalize(p, now); err == nil {
		t.Fatal("expected error for record with no usable source, got nil")
	}
}

func TestNormalize_RejectNonHTTPURL(t *testing.T) {
	p := validParsed()
	p.URL = "javascript:alert(1)"
	if _, err := Normalize(p, now); err == nil {
		t.Fatal("expected error: javascript: source URL must be rejected")
	}
}

// A non-industry event (no matching taxonomy category) is classified
// excluded=true and PERSISTED with zero categories — not rejected (AC-6).
func TestNormalize_ExcludedNoCategoryStored(t *testing.T) {
	p := validParsed()
	p.ClassifyText = "총동창회 동문회 모임" // non-industry → excluded
	p.Name = "동문회"
	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("excluded event should normalize, got error: %v", err)
	}
	if !e.Excluded {
		t.Error("excluded = false, want true for non-industry event")
	}
	if len(e.Categories) != 0 {
		t.Errorf("categories = %v, want empty for excluded event", e.Categories)
	}
	if err := Validate(e); err != nil {
		t.Fatalf("excluded event failed Validate: %v", err)
	}
}

// A NON-excluded event with zero categories is still invalid (rule 1).
func TestValidate_RejectNonExcludedNoCategory(t *testing.T) {
	e, err := Normalize(validParsed(), now)
	if err != nil {
		t.Fatalf("setup Normalize failed: %v", err)
	}
	e.Excluded = false
	e.Categories = nil
	if err := Validate(e); err == nil {
		t.Fatal("expected error: non-excluded event with no category (rule 1)")
	}
}

// ---------------------------------------------------------------------------
// valid record passes Validate
// ---------------------------------------------------------------------------

func TestNormalize_ValidPasses(t *testing.T) {
	e, err := Normalize(validParsed(), now)
	if err != nil {
		t.Fatalf("Normalize returned error for valid input: %v", err)
	}
	if err := Validate(e); err != nil {
		t.Fatalf("valid record failed Validate: %v", err)
	}
	if !contains(e.Categories, classify.CategoryAI) {
		t.Errorf("categories = %v, want to contain ai", e.Categories)
	}
}

// ---------------------------------------------------------------------------
// non-fabricated stored URLs: homepage with bad scheme is dropped, not stored
// ---------------------------------------------------------------------------

func TestNormalize_DropsBadHomepageScheme(t *testing.T) {
	p := validParsed()
	p.HomepageURL = ptr("javascript:alert(1)")
	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.HomepageURL != nil {
		t.Errorf("bad-scheme homepage must be dropped, got %v", *e.HomepageURL)
	}
	if !contains(e.MissingFields, "homepage_url") {
		t.Errorf("dropped homepage_url must be in missing_fields: %v", e.MissingFields)
	}
}

// missing_fields must be sorted & deduplicated for deterministic output/hash.
func TestNormalize_MissingFieldsSortedUnique(t *testing.T) {
	e, err := Normalize(validParsed(), now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !sort.StringsAreSorted(e.MissingFields) {
		t.Errorf("missing_fields not sorted: %v", e.MissingFields)
	}
	seen := map[string]bool{}
	for _, m := range e.MissingFields {
		if seen[m] {
			t.Errorf("missing_fields has duplicate %q: %v", m, e.MissingFields)
		}
		seen[m] = true
	}
}
