// Package normalize converts a raw sources.ParsedEvent into the canonical v0.1
// model.Event: it parses dates to ISO, classifies against the taxonomy SSOT,
// records every null/unknown field in missing_fields, accepts a short
// source-derived summary when the venue detail page provides one, validates stored
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
var dateLayouts = []string{"2006.01.02", "2006.1.2", "2006-01-02", "2006-1-2"}

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

// summaryMaxRunes caps the source-derived summary length (schema convention:
// <=240 chars). Measured in runes so multi-byte Korean text is not truncated
// mid-glyph.
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
//   - summary is source-derived venue detail text, <=240 chars; null +
//     missing_fields when absent. It is not fabricated from date/venue facts.
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

	applyActionSignals(e, p, mf)

	// --- homepage (validated URL) ---
	if hp := validURL(p.HomepageURL); hp != nil {
		e.HomepageURL = hp
	} else {
		e.HomepageURL = nil
		mf.add("homepage_url")
	}

	// --- provenance: one venue source from the detail URL ---
	e.Sources = buildSources(p, now)

	// --- summary (source-derived detail text only) ---
	if sum := buildSummary(p); sum != "" {
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
