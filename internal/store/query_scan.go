package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/smpain/event-intelligence-api/internal/model"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row rowScanner) (model.Event, error) {
	var (
		e        model.Event
		excluded int

		venueJSON, scaleJSON         sql.NullString
		categoriesJSON, audienceJSON sql.NullString
		actionsJSON                  sql.NullString
		sourcesJSON, missingJSON     sql.NullString
		venueID                      sql.NullString
		curatedBy                    sql.NullString
	)

	err := row.Scan(
		&e.EventID, &e.SchemaVersion, &e.SeriesID, &e.Name, &e.NameKo, &e.NameEn, &e.Edition,
		&e.StartDate, &e.EndDate, &e.Timezone, &e.DateConfidence, &e.Status, &e.Format,
		&venueJSON, &venueID, &e.Country,
		&categoriesJSON, &audienceJSON, &scaleJSON,
		&actionsJSON, &e.RegisterURL, &e.ExhibitURL, &e.RegistrationDeadline, &e.ExhibitorDeadline, &e.CostHint,
		&e.Summary, &sourcesJSON, &e.HomepageURL, &e.LastCheckedAt, &e.UpdateState, &e.Confidence, &missingJSON, &e.AmbiguityNotes,
		&curatedBy, &e.CreatedAt, &e.UpdatedAt, &e.ContentHash, &excluded,
	)
	if err != nil {
		return model.Event{}, err
	}

	e.Excluded = excluded != 0
	e.CuratedBy = curatedBy.String

	if venueJSON.Valid && venueJSON.String != "" {
		var v model.Venue
		if err := json.Unmarshal([]byte(venueJSON.String), &v); err != nil {
			return model.Event{}, fmt.Errorf("decode venue for %s: %w", e.EventID, err)
		}
		e.Venue = &v
	}
	if scaleJSON.Valid && scaleJSON.String != "" {
		var sc model.Scale
		if err := json.Unmarshal([]byte(scaleJSON.String), &sc); err != nil {
			return model.Event{}, fmt.Errorf("decode scale for %s: %w", e.EventID, err)
		}
		e.Scale = &sc
	}
	if actionsJSON.Valid && actionsJSON.String != "" {
		if err := json.Unmarshal([]byte(actionsJSON.String), &e.Actions); err != nil {
			return model.Event{}, fmt.Errorf("decode actions for %s: %w", e.EventID, err)
		}
	}
	if err := decodeStringSlice(categoriesJSON, &e.Categories); err != nil {
		return model.Event{}, fmt.Errorf("decode categories for %s: %w", e.EventID, err)
	}
	if err := decodeStringSlice(audienceJSON, &e.Audience); err != nil {
		return model.Event{}, fmt.Errorf("decode audience for %s: %w", e.EventID, err)
	}
	if err := decodeStringSlice(missingJSON, &e.MissingFields); err != nil {
		return model.Event{}, fmt.Errorf("decode missing_fields for %s: %w", e.EventID, err)
	}
	if sourcesJSON.Valid && sourcesJSON.String != "" {
		if err := json.Unmarshal([]byte(sourcesJSON.String), &e.Sources); err != nil {
			return model.Event{}, fmt.Errorf("decode sources for %s: %w", e.EventID, err)
		}
	}
	e.Status = effectiveStatusAt(e, time.Now())

	return e, nil
}

// effectiveStatusAt keeps preserved historical rows truthful without deleting
// them or rewriting their audit history. Explicit cancellation/postponement
// states win; only a scheduled event whose final known date is before today in
// its own timezone becomes ended on reads.
func effectiveStatusAt(e model.Event, now time.Time) string {
	if e.Status != "scheduled" {
		return e.Status
	}
	last := e.EndDate
	if last == nil {
		last = e.StartDate
	}
	if last == nil {
		return e.Status
	}
	tz := "UTC"
	if e.Timezone != nil && *e.Timezone != "" {
		tz = *e.Timezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	if *last < now.In(loc).Format("2006-01-02") {
		return "ended"
	}
	return e.Status
}
