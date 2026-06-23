package normalize

import (
	"testing"

	"github.com/smpain/event-intelligence-api/internal/classify"
)

func TestNormalize_InternationalParsedEvent(t *testing.T) {
	p := validParsed()
	p.SourceID = "benchmark"
	p.EventID = "benchmark-medica-2026"
	p.URL = "https://www.medica-tradefair.com/"
	p.Name = "MEDICA"
	p.StartRaw = ptr("2026-11-16T09:00:00+02:00")
	p.EndRaw = ptr("2026-11-19T18:00:00+02:00")
	p.VenueName = ptr("Messe Düsseldorf GmbH")
	p.City = ptr("Düsseldorf")
	p.Country = ptr("DE")
	p.Timezone = ptr("Europe/Berlin")
	p.Format = ptr("onsite")
	p.SourceType = ptr("organizer")
	p.Publisher = ptr("Messe Düsseldorf GmbH")
	p.ClassifyText = "MEDICA medical technology healthcare medical devices"

	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if e.Country != "DE" {
		t.Fatalf("Country = %q, want DE", e.Country)
	}
	if e.Timezone == nil || *e.Timezone != "Europe/Berlin" {
		t.Fatalf("Timezone = %v", e.Timezone)
	}
	if e.Format != "onsite" {
		t.Fatalf("Format = %q, want onsite", e.Format)
	}
	if e.StartDate == nil || *e.StartDate != "2026-11-16" {
		t.Fatalf("StartDate = %v, want 2026-11-16", e.StartDate)
	}
	if e.Venue == nil || e.Venue.VenueID != nil {
		t.Fatalf("Venue = %+v, want international venue without local venue_id", e.Venue)
	}
	if len(e.Sources) != 1 || e.Sources[0].Type != "organizer" {
		t.Fatalf("Sources = %+v, want organizer source", e.Sources)
	}
	if !contains(e.Categories, classify.CategoryMedicalDevices) {
		t.Fatalf("Categories = %v, want medical-devices", e.Categories)
	}
}

func TestParseDateRejectsInvalidISOShapedDate(t *testing.T) {
	if got, ok := parseDate("2026-02-31T09:00:00+02:00"); ok {
		t.Fatalf("parseDate accepted invalid RFC3339-shaped date as %q", got)
	}
	if got, ok := parseDate("2026-11-16T09:00:00+02:00"); !ok || got != "2026-11-16" {
		t.Fatalf("parseDate valid RFC3339 = %q, %v; want 2026-11-16, true", got, ok)
	}
	if got, ok := parseDate("2026-11-16-not-a-date"); ok {
		t.Fatalf("parseDate accepted sliced date prefix as %q", got)
	}
}
