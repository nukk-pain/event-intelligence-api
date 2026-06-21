// Package normalize converts a raw sources.ParsedEvent into the canonical v0.1
// model.Event: it parses dates to ISO, classifies against the taxonomy SSOT,
// records every null/unknown field in missing_fields, builds a templated
// summary from facts only (never copied from a source body), validates stored
// URLs, and enforces the v0.1 schema validation rules (1-5). A record that
// fails validation is rejected with an error and is NOT persisted, so the
// caller preserves any existing good row.
//
// NO LLM is used anywhere here (project cost-efficiency mandate). The
// controlled vocabulary lives once in internal/classify (taxonomy SSOT); this
// package imports the SAME lists via classify.IsCategory so "a category must
// exist in the taxonomy" (schema rule 4) is checked against one source.
package normalize

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/smpain/event-intelligence-api/internal/classify"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// dateLayouts are the source date shapes seen on the two venues: COEX uses
// "2024.08.24" and KINTEX uses "2026-06-18". Both normalize to ISO "2006-01-02".
// Order is irrelevant (each layout is unambiguous). Parsing is strict — a string
// that matches no layout is treated as unparseable (null + missing_fields),
// never coerced.
var dateLayouts = []string{"2006.01.02", "2006-01-02"}

// parseDate returns the ISO (YYYY-MM-DD) form of a raw date string, or ("",
// false) when it matches no known layout. It does not fabricate or guess.
func parseDate(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

// normalizeDates parses the raw start/end into ISO dates, enforcing the date
// invariants:
//   - a date that matches no layout is nulled and its field added to mf, with
//     date_confidence "low";
//   - if end < start (both parsed), BOTH are nulled, both added to mf,
//     date_confidence "low", and an ambiguity note returned;
//   - otherwise date_confidence is "high".
//
// Dates are never fabricated or reordered.
func normalizeDates(startRaw, endRaw *string, mf *missingFields) (start, end *string, confidence, ambiguity string) {
	var sOK, eOK bool
	var sISO, eISO string

	if startRaw != nil {
		sISO, sOK = parseDate(*startRaw)
	}
	if endRaw != nil {
		eISO, eOK = parseDate(*endRaw)
	}

	confidence = "high"

	if sOK && eOK && eISO < sISO {
		// end before start: refuse to store a disordered range.
		mf.add("start_date")
		mf.add("end_date")
		return nil, nil, "low", fmt.Sprintf("end_date %s precedes start_date %s; both dropped", eISO, sISO)
	}

	if sOK {
		start = &sISO
	} else {
		mf.add("start_date")
		confidence = "low"
	}
	if eOK {
		end = &eISO
	} else {
		mf.add("end_date")
		confidence = "low"
	}
	return start, end, confidence, ""
}

// country is the fixed ISO 3166-1 alpha-2 code for both v1 venues (COEX/KINTEX
// are Korean). It is a deterministic fact about the source, not a guess.
const country = "KR"

// summaryMaxRunes caps the templated summary length (schema convention: <=240
// chars). Measured in runes so multi-byte Korean text is not truncated mid-glyph.
const summaryMaxRunes = 240

// sourceType is the provenance type for a venue detail page (schema enum:
// venue|organizer|association|press|social|aggregator). Both adapters scrape the
// venue's own site, so the single source is always "venue".
const sourceType = "venue"

// validSourceTypes is the schema enum for sources[].type, used by Validate.
var validSourceTypes = map[string]struct{}{
	"venue": {}, "organizer": {}, "association": {},
	"press": {}, "social": {}, "aggregator": {},
}

// venueIDForSource maps an adapter id to the durable venue slug used in
// venue.venue_id. Unknown adapters get a nil venue_id (recorded in missing_fields).
var venueIDForSource = map[string]string{
	"coex":   "coex",
	"kintex": "kintex",
}

// Normalize maps a raw ParsedEvent to a canonical model.Event.
//
//   - now is an ISO8601 timestamp used for last_checked_at / created_at /
//     updated_at and as the retrieved_at fallback when the parser left it empty.
//   - Dates are parsed to ISO (YYYY-MM-DD). Unparseable dates, or end<start, are
//     set null, the field is added to missing_fields, date_confidence becomes
//     "low", and (for end<start) ambiguity_notes is set. Dates are NEVER
//     fabricated or reordered.
//   - Every null field and every unknown actions.* key is recorded in
//     missing_fields (honesty rule).
//   - summary is a template over factual fields only, <=240 chars; null +
//     missing_fields when not producible. NEVER copied from a source body.
//   - Stored URLs must be http(s); javascript:/data:/file: etc. are dropped.
//   - The result is validated against schema rules 1-5; on failure an error is
//     returned and nothing is persisted.
func Normalize(p *sources.ParsedEvent, now string) (*model.Event, error) {
	if p == nil {
		return nil, fmt.Errorf("normalize: nil parsed event")
	}

	mf := newMissingFields()

	e := &model.Event{
		SchemaVersion: model.SchemaVersion,
		EventID:       strings.TrimSpace(p.EventID),
		Name:          strings.TrimSpace(p.Name),
		Country:       country,
		Status:        "scheduled",
		Format:        "onsite",
		CostHint:      "unknown",
		LastCheckedAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
		// UpdateState is intentionally left empty: it is DERIVED by store/diff
		// from the content_hash comparison, not by normalize.
	}

	// --- optional identity passthroughs (nullable) ---
	e.SeriesID = nullOrTrim(nil) // series detection is out of v1 scope
	mf.add("series_id")
	e.NameKo = trimPtr(p.NameKo)
	if e.NameKo == nil {
		mf.add("name_ko")
	}
	e.NameEn = trimPtr(p.NameEn)
	if e.NameEn == nil {
		mf.add("name_en")
	}
	e.Edition = trimPtr(p.Edition)
	if e.Edition == nil {
		mf.add("edition")
	}

	// --- dates ---
	start, end, dateConf, ambiguity := normalizeDates(p.StartRaw, p.EndRaw, mf)
	e.StartDate = start
	e.EndDate = end
	e.DateConfidence = dateConf
	if ambiguity != "" {
		e.AmbiguityNotes = &ambiguity
	} else {
		mf.add("ambiguity_notes")
	}

	// timezone: venue pages never state it; KR venues are Asia/Seoul (a fact),
	// but to avoid asserting beyond the source we record it as known-constant.
	tz := "Asia/Seoul"
	e.Timezone = &tz

	// --- venue ---
	e.Venue = buildVenue(p, mf)

	// --- classification ---
	cats, excluded, conf := classify.Classify(p.ClassifyText)
	e.Categories = cats
	e.Excluded = excluded
	e.Confidence = conf
	// audience inference is out of v1 scope: empty slice (schema: may be empty).
	e.Audience = []string{}
	e.Scale = nil
	mf.add("scale.visitors")
	mf.add("scale.exhibitors")

	// --- action fields (all unknown on a venue page -> false + missing_fields) ---
	// Actions default to the zero value (all false); each key is recorded.
	for _, k := range []string{
		"actions.can_register", "actions.can_exhibit", "actions.can_sponsor",
		"actions.has_matchmaking", "actions.has_startup_program",
	} {
		mf.add(k)
	}
	// register/exhibit urls + deadlines are not on venue pages in v1.
	e.RegisterURL = nil
	e.ExhibitURL = nil
	e.RegistrationDeadline = nil
	e.ExhibitorDeadline = nil
	for _, k := range []string{"register_url", "exhibit_url", "registration_deadline", "exhibitor_deadline"} {
		mf.add(k)
	}
	mf.add("cost_hint") // unknown -> recorded

	// --- homepage (validated URL) ---
	if hp := validURL(p.HomepageURL); hp != nil {
		e.HomepageURL = hp
	} else {
		e.HomepageURL = nil
		mf.add("homepage_url")
	}

	// --- provenance: one venue source from the detail URL ---
	e.Sources = buildSources(p, now)

	// --- summary (template from facts only; never copied from body) ---
	if sum := buildSummary(e, p); sum != "" {
		e.Summary = &sum
	} else {
		e.Summary = nil
		mf.add("summary")
	}

	// finalize missing_fields (sorted + deduped for deterministic hashing)
	e.MissingFields = mf.sorted()

	if err := Validate(e); err != nil {
		return nil, fmt.Errorf("normalize %s: %w", e.EventID, err)
	}
	return e, nil
}

// buildVenue assembles the venue object from scraped strings, recording absent
// sub-fields in missing_fields. venue_id is derived from the adapter id.
func buildVenue(p *sources.ParsedEvent, mf *missingFields) *model.Venue {
	name := derefTrim(p.VenueName)
	city := derefTrim(p.City)
	// A venue object needs at least a display name to be meaningful; both v1
	// adapters always supply one (constant), so this is defensive.
	if name == "" && city == "" && p.Hall == nil {
		mf.add("venue.venue_id")
		mf.add("venue.name")
		mf.add("venue.hall")
		mf.add("venue.city")
		return nil
	}
	v := &model.Venue{Name: name, City: city}
	if id, ok := venueIDForSource[p.SourceID]; ok {
		v.VenueID = &id
	} else {
		mf.add("venue.venue_id")
	}
	if name == "" {
		mf.add("venue.name")
	}
	if city == "" {
		mf.add("venue.city")
	}
	if h := trimPtr(p.Hall); h != nil {
		v.Hall = h
	} else {
		mf.add("venue.hall")
	}
	return v
}

// buildSources turns the detail URL into the single venue provenance entry.
// retrieved_at falls back to now when the parser left it empty. An invalid /
// non-http(s) URL yields no source (Validate then rejects via rule 5).
func buildSources(p *sources.ParsedEvent, now string) []model.Source {
	u := validURL(&p.URL)
	if u == nil {
		return nil
	}
	retrieved := strings.TrimSpace(p.RetrievedAt)
	if retrieved == "" {
		retrieved = now
	}
	publisher := derefTrim(p.VenueName)
	if publisher == "" {
		publisher = p.SourceID
	}
	return []model.Source{{
		URL:         *u,
		Type:        sourceType,
		Publisher:   publisher,
		RetrievedAt: retrieved,
	}}
}

// buildSummary renders the factual template "{name} — {start}~{end} @ {venue},
// 주최 {organizer}", omitting parts whose facts are absent, and truncates to
// summaryMaxRunes. It returns "" when no name is available (then the caller
// records summary in missing_fields). It NEVER reads any source body text.
func buildSummary(e *model.Event, p *sources.ParsedEvent) string {
	name := e.Name
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(name)

	if e.StartDate != nil && e.EndDate != nil {
		b.WriteString(" — ")
		b.WriteString(*e.StartDate)
		b.WriteString("~")
		b.WriteString(*e.EndDate)
	} else if e.StartDate != nil {
		b.WriteString(" — ")
		b.WriteString(*e.StartDate)
	}

	if e.Venue != nil && e.Venue.Name != "" {
		b.WriteString(" @ ")
		b.WriteString(e.Venue.Name)
	}

	if org := derefTrim(p.Organizer); org != "" {
		b.WriteString(", 주최 ")
		b.WriteString(org)
	}

	return truncateRunes(b.String(), summaryMaxRunes)
}

// truncateRunes caps s to max runes without splitting a multi-byte glyph.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// ---------------------------------------------------------------------------
// validation (schema rules 1-5)
// ---------------------------------------------------------------------------

// Validate enforces the v0.1 manual-dataset-schema validation rules:
//
//	1. required fields present & non-null (event_id, schema_version, name,
//	   country, categories>=1, sources>=1, last_checked_at, update_state*,
//	   confidence, missing_fields). *update_state is set by store/diff after
//	   normalize, so it is NOT required here.
//	2. (missing_fields completeness is produced by Normalize, not re-derived here)
//	3. end_date >= start_date when both present.
//	4. every category exists in the taxonomy (classify.IsCategory).
//	5. >=1 sources[] entry with an http(s) url + enum type + publisher +
//	   retrieved_at.
//
// A failing record must be rejected by the caller and not persisted.
func Validate(e *model.Event) error {
	if e == nil {
		return fmt.Errorf("nil event")
	}
	// rule 1: required scalars
	if strings.TrimSpace(e.EventID) == "" {
		return fmt.Errorf("rule1: event_id required")
	}
	if e.SchemaVersion == "" {
		return fmt.Errorf("rule1: schema_version required")
	}
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("rule1: name required")
	}
	if strings.TrimSpace(e.Country) == "" {
		return fmt.Errorf("rule1: country required")
	}
	if e.Confidence == "" {
		return fmt.Errorf("rule1: confidence required")
	}
	if e.MissingFields == nil {
		return fmt.Errorf("rule1: missing_fields required (use empty slice, not nil)")
	}
	if e.LastCheckedAt == "" {
		return fmt.Errorf("rule1: last_checked_at required")
	}

	// rule 1 + 4: categories required (>=1) and each in taxonomy.
	// An excluded event is, by definition, outside our taxonomy (a non-industry
	// event the classifier declined to categorize). It is still PERSISTED — with
	// excluded=true and zero categories — and merely hidden from the default
	// listing (AC-6). Only non-excluded events must carry >=1 category.
	if !e.Excluded && len(e.Categories) == 0 {
		return fmt.Errorf("rule1: categories required (>=1) for non-excluded event")
	}
	for _, c := range e.Categories {
		if !classify.IsCategory(c) {
			return fmt.Errorf("rule4: category %q not in taxonomy", c)
		}
	}

	// rule 3: end >= start when both present
	if e.StartDate != nil && e.EndDate != nil && *e.EndDate < *e.StartDate {
		return fmt.Errorf("rule3: end_date %q before start_date %q", *e.EndDate, *e.StartDate)
	}

	// rule 5: >=1 source, each with valid url/type/publisher/retrieved_at
	if len(e.Sources) == 0 {
		return fmt.Errorf("rule5: at least one source required")
	}
	for i, s := range e.Sources {
		if !isHTTPURL(s.URL) {
			return fmt.Errorf("rule5: source[%d] url %q not http(s)", i, s.URL)
		}
		if _, ok := validSourceTypes[s.Type]; !ok {
			return fmt.Errorf("rule5: source[%d] type %q not in enum", i, s.Type)
		}
		if strings.TrimSpace(s.Publisher) == "" {
			return fmt.Errorf("rule5: source[%d] publisher required", i)
		}
		if strings.TrimSpace(s.RetrievedAt) == "" {
			return fmt.Errorf("rule5: source[%d] retrieved_at required", i)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// URL validation
// ---------------------------------------------------------------------------

// validURL returns a cleaned *string when p holds an http(s) URL, else nil.
// javascript:/data:/file:/mailto: and other schemes are rejected so they are
// never stored.
func validURL(p *string) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	if !isHTTPURL(s) {
		return nil
	}
	return &s
}

// isHTTPURL reports whether s is a syntactically http:// or https:// URL. It does
// NOT fetch; this is a scheme/shape guard, not a liveness check.
func isHTTPURL(s string) bool {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return false
	}
	// require a host after the scheme
	rest := s[strings.Index(s, "://")+3:]
	return strings.TrimSpace(rest) != ""
}

// ---------------------------------------------------------------------------
// missingFields accumulator
// ---------------------------------------------------------------------------

type missingFields struct {
	set map[string]struct{}
}

func newMissingFields() *missingFields { return &missingFields{set: map[string]struct{}{}} }

func (m *missingFields) add(path string) { m.set[path] = struct{}{} }

// sorted returns the dotted paths sorted & deduplicated (deterministic so the
// content_hash over missing_fields is stable). Always non-nil (empty slice when
// nothing is missing) so schema rule 1 (missing_fields required) holds.
func (m *missingFields) sorted() []string {
	out := make([]string, 0, len(m.set))
	for k := range m.set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	if s == "" {
		return nil
	}
	return &s
}

func nullOrTrim(p *string) *string { return trimPtr(p) }

func derefTrim(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
