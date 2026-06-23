package opportunity

import (
	"strings"

	"github.com/smpain/event-intelligence-api/internal/model"
)

type Assessment struct {
	Quality    string
	Signals    []string
	Notes      []string
	Actionable bool
	Shortlist  bool
}

func Assess(event model.Event) Assessment {
	var notes []string
	if event.StartDate == nil || strings.TrimSpace(*event.StartDate) == "" {
		notes = append(notes, "missing_start_date")
	}
	if event.EndDate == nil || strings.TrimSpace(*event.EndDate) == "" {
		notes = append(notes, "missing_end_date")
	}
	if event.HomepageURL == nil || strings.TrimSpace(*event.HomepageURL) == "" {
		notes = append(notes, "missing_homepage_url")
	}
	if len(event.Categories) == 0 {
		notes = append(notes, "missing_categories")
	}
	if len(event.Sources) == 0 {
		notes = append(notes, "missing_sources")
	}

	signals, actionable := signals(event)
	eligible := len(notes) == 0
	quality := "low"
	if eligible && len(signals) >= 2 {
		quality = "high"
	} else if eligible && len(signals) == 1 {
		quality = "medium"
	}

	return Assessment{
		Quality:    quality,
		Signals:    signals,
		Notes:      notes,
		Actionable: actionable,
		Shortlist:  eligible && len(signals) > 0,
	}
}

func Apply(event *model.Event) {
	assessment := Assess(*event)
	event.OpportunityQuality = assessment.Quality
	event.OpportunitySignals = assessment.Signals
	event.SourceQualityNotes = assessment.Notes
}

func signals(event model.Event) ([]string, bool) {
	var out []string
	actionable := false
	add := func(name string, isActionable bool) {
		out = append(out, name)
		actionable = actionable || isActionable
	}

	if present(event.RegisterURL) {
		add("register_url", true)
	}
	if present(event.ExhibitURL) {
		add("exhibit_url", true)
	}
	if present(event.RegistrationDeadline) {
		add("registration_deadline", true)
	}
	if present(event.ExhibitorDeadline) {
		add("exhibitor_deadline", true)
	}
	if event.CostHint != "" && event.CostHint != "unknown" {
		add("cost_hint", true)
	}
	if event.Actions.CanRegister {
		add("actions.can_register", true)
	}
	if event.Actions.CanExhibit {
		add("actions.can_exhibit", true)
	}
	if event.Actions.CanSponsor {
		add("actions.can_sponsor", true)
	}
	if event.Actions.HasMatchmaking {
		add("actions.has_matchmaking", true)
	}
	if event.Actions.HasStartupProgram {
		add("actions.has_startup_program", true)
	}
	if present(event.Summary) {
		add("summary", false)
	}
	return out, actionable
}

func present(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}
