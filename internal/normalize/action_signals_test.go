package normalize

import (
	"testing"

	"github.com/smpain/event-intelligence-api/internal/sources"
)

func TestNormalize_ActionsFromOfficialPageSignals(t *testing.T) {
	p := validParsed()
	canRegister := true
	canExhibit := true
	costHint := "mixed"
	registerURL := "https://aiexpo.example.org/register"
	exhibitURL := "https://aiexpo.example.org/exhibit"
	registrationDeadline := "2026.7.3"
	exhibitorDeadline := "2026-8-15"
	p.Actions = sources.ActionSignals{
		CanRegister:          &canRegister,
		CanExhibit:           &canExhibit,
		RegisterURL:          &registerURL,
		ExhibitURL:           &exhibitURL,
		RegistrationDeadline: &registrationDeadline,
		ExhibitorDeadline:    &exhibitorDeadline,
		CostHint:             &costHint,
	}
	p.ExtraSources = []sources.ParsedSource{{
		URL:         "https://aiexpo.example.org",
		Type:        "organizer",
		Publisher:   "official event page",
		RetrievedAt: now,
	}}

	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if !e.Actions.CanRegister || !e.Actions.CanExhibit {
		t.Fatalf("actions = %+v, want register/exhibit true", e.Actions)
	}
	if e.RegisterURL == nil || *e.RegisterURL != registerURL {
		t.Fatalf("register_url = %v, want %q", e.RegisterURL, registerURL)
	}
	if e.ExhibitURL == nil || *e.ExhibitURL != exhibitURL {
		t.Fatalf("exhibit_url = %v, want %q", e.ExhibitURL, exhibitURL)
	}
	if e.CostHint != "mixed" {
		t.Fatalf("cost_hint = %q, want mixed", e.CostHint)
	}
	if e.RegistrationDeadline == nil || *e.RegistrationDeadline != "2026-07-03" {
		t.Fatalf("registration_deadline = %v, want 2026-07-03", e.RegistrationDeadline)
	}
	if e.ExhibitorDeadline == nil || *e.ExhibitorDeadline != "2026-08-15" {
		t.Fatalf("exhibitor_deadline = %v, want 2026-08-15", e.ExhibitorDeadline)
	}
	for _, absent := range []string{
		"actions.can_register", "actions.can_exhibit", "register_url", "exhibit_url",
		"registration_deadline", "exhibitor_deadline", "cost_hint",
	} {
		if contains(e.MissingFields, absent) {
			t.Fatalf("missing_fields contains %q after source supplied it: %v", absent, e.MissingFields)
		}
	}
	if len(e.Sources) != 2 {
		t.Fatalf("sources len = %d, want venue + organizer", len(e.Sources))
	}
	if e.Sources[1].Type != "organizer" || e.Sources[1].URL != "https://aiexpo.example.org" {
		t.Fatalf("extra source = %+v", e.Sources[1])
	}
}
