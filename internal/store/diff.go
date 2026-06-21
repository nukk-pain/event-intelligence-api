package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/smpain/event-intelligence-api/internal/model"
)

// ---------------------------------------------------------------------------
// Field-level diff (Task 1.3)
// ---------------------------------------------------------------------------
//
// Diff compares two events over the SHARED canonical projection (CanonicalFields)
// and emits exactly one ChangeLog row per changed field_path, in STABLE sorted
// field_path order. Because it diffs the same projection ContentHash hashes, the
// two never split-brain: ContentHash(prev)==ContentHash(next) <=> Diff returns
// zero rows.
//
// Semantics:
//   - prev == nil  -> a brand-new event: zero rows (creation is not a change).
//   - A field present in the projection always exists on both sides (the key set
//     is fixed), so old/new are compared directly.
//   - nullSentinel (absent value) is surfaced as a nil *string in old/new_value.
//
// The caller supplies changedAt and batchID, applied uniformly to every row.
func Diff(prev, next *model.Event, changedAt, batchID string) []model.ChangeLog {
	if prev == nil || next == nil {
		return nil
	}
	oldFields := CanonicalFields(*prev)
	newFields := CanonicalFields(*next)

	// The key set is identical for any two events; range one side, sorted.
	paths := make([]string, 0, len(newFields))
	for k := range newFields {
		paths = append(paths, k)
	}
	sort.Strings(paths)

	var bid *string
	if batchID != "" {
		b := batchID
		bid = &b
	}

	out := make([]model.ChangeLog, 0)
	for _, p := range paths {
		ov := oldFields[p]
		nv := newFields[p]
		if ov == nv {
			continue
		}
		out = append(out, model.ChangeLog{
			EventID:   next.EventID,
			FieldPath: p,
			OldValue:  fieldValuePtr(ov),
			NewValue:  fieldValuePtr(nv),
			ChangedAt: changedAt,
			BatchID:   bid,
		})
	}
	return out
}

// fieldValuePtr converts a canonical field value to a *string, mapping the
// absent-value sentinel back to nil so old_value/new_value are NULL in the feed.
func fieldValuePtr(v string) *string {
	if v == nullSentinel {
		return nil
	}
	s := v
	return &s
}

// ---------------------------------------------------------------------------
// ApplyBatch — idempotent batch runner primitive (wired from pipeline/run.go)
// ---------------------------------------------------------------------------
//
// ApplyBatch applies one batch of freshly-discovered/normalized events to the
// canonical store with the Task 1.3 guarantees:
//
//   - Change detection derives strictly from the PERSISTED content_hash and
//     persisted field values (never from in-memory prior-run state), so a re-run
//     over identical source is a pure no-op regardless of crash history.
//   - new event        -> insert, update_state="new", zero change_log rows.
//   - changed event     -> upsert + N change_log rows (one per changed field),
//     update_state="updated"; updated_at advances.
//   - unchanged event   -> update_state="unchanged"; ONLY last_checked_at is
//     refreshed via a narrow UPDATE that does NOT touch updated_at (so the
//     (updated_at,event_id) change-feed cursor is not perturbed). content_hash
//     stays byte-identical.
//   - DISAPPEARANCE: events in the DB but absent from this batch receive NO
//     write at all — their last_checked_at simply goes stale. There is no reap /
//     delete / status-mutation path (preserves data on partial/empty fetches).
//
// The whole call shares the single writer connection; each event's write is its
// own transaction (via UpsertEvent / a narrow UPDATE) so a mid-batch failure
// leaves already-applied events intact and never partially writes one event.
func ApplyBatch(ctx context.Context, db *sql.DB, events []model.Event, batchID string) error {
	for i := range events {
		e := events[i]
		// The batch argument is the authoritative batch identity for change-feed
		// attribution, regardless of any per-event BatchID the caller set.
		e.BatchID = batchID

		prev, prevHash, found, err := loadCanonical(ctx, db, e.EventID)
		if err != nil {
			return fmt.Errorf("load existing %s: %w", e.EventID, err)
		}

		newHash := ContentHash(e)

		switch {
		case !found:
			// Brand-new event: insert, no change rows.
			e.UpdateState = "new"
			if err := UpsertEvent(ctx, db, e, nil, nil); err != nil {
				return fmt.Errorf("insert new %s: %w", e.EventID, err)
			}

		case newHash == prevHash:
			// No semantic change. Settle update_state to "unchanged" and refresh
			// ONLY last_checked_at, leaving updated_at (and thus the
			// (updated_at,event_id) change-feed cursor) untouched. Do not
			// re-upsert (that would bump updated_at and rewrite content_hash).
			// Re-running an already-"unchanged" event is then a true fixed point.
			if err := markUnchanged(ctx, db, e.EventID, e.LastCheckedAt); err != nil {
				return fmt.Errorf("mark unchanged %s: %w", e.EventID, err)
			}

		default:
			// Real change: diff against the PERSISTED state, write N rows.
			// Guard the change-feed timestamp BEFORE diffing: change_log.changed_at
			// is NOT NULL but '' satisfies that, so an unstamped event would silently
			// write a blank changed_at, corrupting the /events/changes feed and any
			// since= cursor ordering. Fail loudly instead.
			ts := changeTimestamp(e)
			if ts == "" {
				return fmt.Errorf("%s: empty change timestamp (last_checked_at/updated_at unset)", e.EventID)
			}
			e.UpdateState = "updated"
			changes := Diff(prev, &e, ts, batchID)
			if err := UpsertEvent(ctx, db, e, changes, nil); err != nil {
				return fmt.Errorf("update %s: %w", e.EventID, err)
			}
		}
	}
	return nil
}

// changeTimestamp picks the timestamp recorded on change_log rows. It uses the
// event's LastCheckedAt (the verification instant for this batch) when present,
// falling back to the model's UpdatedAt.
func changeTimestamp(e model.Event) string {
	if e.LastCheckedAt != "" {
		return e.LastCheckedAt
	}
	return e.UpdatedAt
}

// markUnchanged settles update_state to "unchanged" and advances last_checked_at,
// deliberately NOT touching updated_at or content_hash, so a no-op run does not
// reorder the (updated_at,event_id) change-feed cursor nor perturb the hash. If
// lastChecked is empty, last_checked_at is left as-is but update_state still
// settles to "unchanged".
func markUnchanged(ctx context.Context, db *sql.DB, eventID, lastChecked string) error {
	if lastChecked == "" {
		_, err := db.ExecContext(ctx,
			`UPDATE events SET update_state='unchanged' WHERE event_id=?`, eventID)
		return err
	}
	_, err := db.ExecContext(ctx,
		`UPDATE events SET update_state='unchanged', last_checked_at=? WHERE event_id=?`,
		lastChecked, eventID)
	return err
}

// loadCanonical reads the persisted semantic columns for an event and rebuilds a
// model.Event sufficient for CanonicalFields/Diff, plus the stored content_hash.
// found=false means the event is not yet in the store.
func loadCanonical(ctx context.Context, db *sql.DB, eventID string) (*model.Event, string, bool, error) {
	const q = `
SELECT schema_version, series_id, name, name_ko, name_en, edition,
       start_date, end_date, timezone, date_confidence, status, format,
       venue, country, categories, audience, scale, actions,
       register_url, exhibit_url, registration_deadline, exhibitor_deadline, cost_hint,
       sources, homepage_url, missing_fields, ambiguity_notes, content_hash
FROM events WHERE event_id=?`

	var (
		schemaVersion                                                    string
		seriesID, nameKo, nameEn, edition                                sql.NullString
		name                                                             string
		startDate, endDate, timezone                                     sql.NullString
		dateConfidence, status, format                                   string
		venueJSON                                                        sql.NullString
		country                                                          string
		categoriesJSON                                                   sql.NullString
		audienceJSON, scaleJSON, actionsJSON                             sql.NullString
		registerURL, exhibitURL, registrationDeadline, exhibitorDeadline sql.NullString
		costHint                                                         string
		sourcesJSON                                                      sql.NullString
		homepageURL, missingFieldsJSON, ambiguityNotes, contentHash      sql.NullString
	)

	err := db.QueryRowContext(ctx, q, eventID).Scan(
		&schemaVersion, &seriesID, &name, &nameKo, &nameEn, &edition,
		&startDate, &endDate, &timezone, &dateConfidence, &status, &format,
		&venueJSON, &country, &categoriesJSON, &audienceJSON, &scaleJSON, &actionsJSON,
		&registerURL, &exhibitURL, &registrationDeadline, &exhibitorDeadline, &costHint,
		&sourcesJSON, &homepageURL, &missingFieldsJSON, &ambiguityNotes, &contentHash,
	)
	if err == sql.ErrNoRows {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}

	e := model.Event{
		SchemaVersion:        schemaVersion,
		EventID:              eventID,
		SeriesID:             nsPtr(seriesID),
		Name:                 name,
		NameKo:               nsPtr(nameKo),
		NameEn:               nsPtr(nameEn),
		Edition:              nsPtr(edition),
		StartDate:            nsPtr(startDate),
		EndDate:              nsPtr(endDate),
		Timezone:             nsPtr(timezone),
		DateConfidence:       dateConfidence,
		Status:               status,
		Format:               format,
		Country:              country,
		RegisterURL:          nsPtr(registerURL),
		ExhibitURL:           nsPtr(exhibitURL),
		RegistrationDeadline: nsPtr(registrationDeadline),
		ExhibitorDeadline:    nsPtr(exhibitorDeadline),
		CostHint:             costHint,
		HomepageURL:          nsPtr(homepageURL),
		AmbiguityNotes:       nsPtr(ambiguityNotes),
	}

	if venueJSON.Valid && venueJSON.String != "" {
		var v model.Venue
		if err := json.Unmarshal([]byte(venueJSON.String), &v); err != nil {
			return nil, "", false, fmt.Errorf("decode venue: %w", err)
		}
		e.Venue = &v
	}
	if scaleJSON.Valid && scaleJSON.String != "" {
		var s model.Scale
		if err := json.Unmarshal([]byte(scaleJSON.String), &s); err != nil {
			return nil, "", false, fmt.Errorf("decode scale: %w", err)
		}
		e.Scale = &s
	}
	if actionsJSON.Valid && actionsJSON.String != "" {
		if err := json.Unmarshal([]byte(actionsJSON.String), &e.Actions); err != nil {
			return nil, "", false, fmt.Errorf("decode actions: %w", err)
		}
	}
	if err := decodeStringSlice(categoriesJSON, &e.Categories); err != nil {
		return nil, "", false, fmt.Errorf("decode categories: %w", err)
	}
	if err := decodeStringSlice(audienceJSON, &e.Audience); err != nil {
		return nil, "", false, fmt.Errorf("decode audience: %w", err)
	}
	if err := decodeStringSlice(missingFieldsJSON, &e.MissingFields); err != nil {
		return nil, "", false, fmt.Errorf("decode missing_fields: %w", err)
	}
	if sourcesJSON.Valid && sourcesJSON.String != "" {
		if err := json.Unmarshal([]byte(sourcesJSON.String), &e.Sources); err != nil {
			return nil, "", false, fmt.Errorf("decode sources: %w", err)
		}
	}

	return &e, contentHash.String, true, nil
}

func nsPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func decodeStringSlice(ns sql.NullString, dst *[]string) error {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	return json.Unmarshal([]byte(ns.String), dst)
}
