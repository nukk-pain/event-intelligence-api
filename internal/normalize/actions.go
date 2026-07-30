package normalize

import (
	"strings"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

func applyActionSignals(e *model.Event, p *sources.ParsedEvent, mf *missingFields, now string) {
	applyBoolSignal(p.Actions.CanRegister, &e.Actions.CanRegister, "actions.can_register", mf)
	applyBoolSignal(p.Actions.CanExhibit, &e.Actions.CanExhibit, "actions.can_exhibit", mf)
	applyBoolSignal(p.Actions.CanSponsor, &e.Actions.CanSponsor, "actions.can_sponsor", mf)
	applyBoolSignal(p.Actions.HasMatchmaking, &e.Actions.HasMatchmaking, "actions.has_matchmaking", mf)
	applyBoolSignal(p.Actions.HasStartupProgram, &e.Actions.HasStartupProgram, "actions.has_startup_program", mf)

	applyURLSignal(p.Actions.RegisterURL, &e.RegisterURL, "register_url", mf)
	applyURLSignal(p.Actions.ExhibitURL, &e.ExhibitURL, "exhibit_url", mf)
	applyDateSignal(p.Actions.RegistrationDeadline, &e.RegistrationDeadline, "registration_deadline", mf, derefOr(e.StartDate), now)
	applyDateSignal(p.Actions.ExhibitorDeadline, &e.ExhibitorDeadline, "exhibitor_deadline", mf, derefOr(e.StartDate), now)

	if hint := trimPtr(p.Actions.CostHint); hint != nil && isCostHint(*hint) {
		e.CostHint = *hint
	} else {
		mf.add("cost_hint")
	}
}

func applyBoolSignal(signal *bool, dst *bool, field string, mf *missingFields) {
	if signal == nil {
		mf.add(field)
		return
	}
	*dst = *signal
}

func applyURLSignal(signal *string, dst **string, field string, mf *missingFields) {
	if u := validURL(signal); u != nil {
		*dst = u
		return
	}
	mf.add(field)
}

func applyDateSignal(signal *string, dst **string, field string, mf *missingFields, start, now string) {
	if signal == nil {
		mf.add(field)
		return
	}
	iso, ok := parseDate(*signal)
	if !ok {
		mf.add(field)
		return
	}
	if !model.DeadlinePlausible(iso, start, now) {
		mf.add(field)
		return
	}
	*dst = &iso
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func isCostHint(s string) bool {
	switch strings.TrimSpace(s) {
	case "free", "paid", "mixed", "unknown":
		return true
	default:
		return false
	}
}
