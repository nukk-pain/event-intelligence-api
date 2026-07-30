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

// observedOn is the day these defects were seen in live data; the package-wide
// `now` predates the deadlines involved, which would make them future dates.
const observedOn = "2026-07-30T00:00:00Z"

// A deadline read off an organizer page belongs to the edition that page is
// about. Live 2026-07-30: 제54회 맘앤베이비엑스포 (8/6) and 제55회 (11/26) both
// list momnbabyexpo.co.kr, so both stored reg 06-22 / exh 07-07 — the November
// edition claiming a deadline that passed 157 days before it opens. 한가위
// (8/14) and 설맞이 (12/22) did the same through fgfair.com.
func TestNormalize_PastDeadlineRejectedForAFarOffEvent(t *testing.T) {
	p := validParsed()
	p.StartRaw = ptr("2026.11.26")
	p.EndRaw = ptr("2026.11.29")
	p.Actions.RegistrationDeadline = ptr("2026-06-22")
	p.Actions.ExhibitorDeadline = ptr("2026-07-07")

	e, err := Normalize(p, observedOn)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.RegistrationDeadline != nil {
		t.Errorf("registration_deadline = %q, want nil — the page describes another edition", *e.RegistrationDeadline)
	}
	if e.ExhibitorDeadline != nil {
		t.Errorf("exhibitor_deadline = %q, want nil", *e.ExhibitorDeadline)
	}
}

func TestNormalize_PastDeadlineKeptForTheNearEditionItDescribes(t *testing.T) {
	// The same page, applied to the edition it is actually about: that
	// registration really did close, and saying so is useful.
	p := validParsed()
	p.StartRaw = ptr("2026.08.06")
	p.EndRaw = ptr("2026.08.09")
	p.Actions.RegistrationDeadline = ptr("2026-06-22")

	e, err := Normalize(p, observedOn)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.RegistrationDeadline == nil || *e.RegistrationDeadline != "2026-06-22" {
		t.Errorf("registration_deadline = %v, want 2026-06-22 kept", e.RegistrationDeadline)
	}
}

func TestNormalize_DeadlineMoreThanAYearBeforeTheEventRejected(t *testing.T) {
	// Live 2026-07-30: 제 6회 국제 물류 및 공급망 관리 산업전 (2026-09-09) stored
	// exhibitor_deadline 2025-08-14, 391 days before its own event.
	p := validParsed()
	p.StartRaw = ptr("2026.09.09")
	p.EndRaw = ptr("2026.09.11")
	p.Actions.ExhibitorDeadline = ptr("2025-08-14")

	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.ExhibitorDeadline != nil {
		t.Errorf("exhibitor_deadline = %q, want nil — no deadline precedes its event by a year", *e.ExhibitorDeadline)
	}
}

func TestNormalize_FutureDeadlineOnAFarOffEventKept(t *testing.T) {
	// The common, correct case must be untouched by either rule.
	p := validParsed()
	p.StartRaw = ptr("2026.11.26")
	p.EndRaw = ptr("2026.11.29")
	p.Actions.RegistrationDeadline = ptr("2026-10-30")

	e, err := Normalize(p, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if e.RegistrationDeadline == nil || *e.RegistrationDeadline != "2026-10-30" {
		t.Errorf("registration_deadline = %v, want 2026-10-30 kept", e.RegistrationDeadline)
	}
}
