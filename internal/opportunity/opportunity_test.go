package opportunity

import (
	"reflect"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
)

func TestAssessHighWhenEventHasMultipleSourceBackedSignals(t *testing.T) {
	event := baseEvent()
	event.RegisterURL = strptr("https://expo.example/register")
	event.ExhibitURL = strptr("https://expo.example/exhibit")

	got := Assess(event)

	if got.Quality != "high" {
		t.Fatalf("quality = %q, want high", got.Quality)
	}
	wantSignals := []string{"register_url", "exhibit_url"}
	if !reflect.DeepEqual(got.Signals, wantSignals) {
		t.Fatalf("signals = %v, want %v", got.Signals, wantSignals)
	}
	if !got.Actionable {
		t.Fatalf("actionable = false, want true")
	}
	if !got.Shortlist {
		t.Fatalf("shortlist = false, want true")
	}
}

func TestAssessLowWhenRequiredOpportunityFieldsAreMissing(t *testing.T) {
	event := baseEvent()
	event.HomepageURL = nil
	event.Actions.CanRegister = true

	got := Assess(event)

	if got.Quality != "low" {
		t.Fatalf("quality = %q, want low", got.Quality)
	}
	if !got.Actionable {
		t.Fatalf("actionable = false, want true")
	}
	if got.Shortlist {
		t.Fatalf("shortlist = true, want false")
	}
	if len(got.Notes) == 0 || got.Notes[0] != "missing_homepage_url" {
		t.Fatalf("notes = %v, want missing_homepage_url first", got.Notes)
	}
}

func baseEvent() model.Event {
	return model.Event{
		EventID:    "ev",
		Name:       "Event",
		StartDate:  strptr("2026-07-01"),
		EndDate:    strptr("2026-07-02"),
		Categories: []string{"ai"},
		Sources: []model.Source{
			{URL: "https://venue.example/event", Type: "venue", Publisher: "Venue", RetrievedAt: "2026-06-23T00:00:00Z"},
		},
		HomepageURL: strptr("https://expo.example"),
		CostHint:    "unknown",
	}
}

func strptr(value string) *string { return &value }
