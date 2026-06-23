package api

import (
	"database/sql"
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/opportunity"
	"github.com/smpain/event-intelligence-api/internal/store"
)

// detail.go implements Task 3.2: the single-event detail endpoint, its sources
// sub-resource, and the change feed. These routes are registered alongside the
// list endpoint by registerDetailRoutes, which RegisterRoutes calls so the whole
// v1 read surface is mounted from one place.
//
// Route ordering note: chi v5 prefers a static segment over a wildcard at the
// same position, so GET /api/v1/events/changes is matched before the
// GET /api/v1/events/{event_id} pattern even though both share the /events
// prefix. No manual ordering is required.

// registerDetailRoutes mounts the 3.2 routes on an /api/v1-rooted router. It is
// invoked from RegisterRoutes (events.go) within the same r.Route("/api/v1") so
// every path here is served under that prefix.
func registerDetailRoutes(r chi.Router, db *sql.DB) {
	r.Get("/events/changes", handleListChanges(db))
	r.Get("/events/{event_id}", handleGetEvent(db))
	r.Get("/events/{event_id}/sources", handleListSources(db))
}

// detailEnvelope is the single-resource wrapper. Detail responses are not
// paginated, so they carry only "data" (the event, with its sources inlined for
// provenance).
type detailEnvelope struct {
	Data any `json:"data"`
}

// handleGetEvent serves GET /api/v1/events/{event_id}. The event JSON already
// embeds its sources array (model.Event.Sources), satisfying the contract that
// the detail response includes provenance.
//
// The detail route negotiates JSON/Markdown through Respond() just like the list
// and change feed, so ?format=md and Accept: text/markdown honour the OpenAPI
// text/markdown advertisement for this path (render.go doc: all three read-path
// kinds route through Respond). Detail is unpaginated, so no Link header.
func handleGetEvent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "event_id")

		event, err := store.GetEvent(r.Context(), db, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "failed to load event")
			return
		}
		if event == nil {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		opportunity.Apply(event)
		Respond(w, r, newEventDetailView(event), Links{})
	}
}

// handleListSources serves GET /api/v1/events/{event_id}/sources. It returns the
// event's provenance entries (>=1 by schema). A missing event yields 404 so the
// caller distinguishes "no such event" from "event with zero sources".
func handleListSources(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "event_id")

		// Confirm the event exists first: ListSources returns (nil,nil) both for a
		// missing event and (in principle) an event with no sources, so we probe
		// existence explicitly to map the missing case to 404.
		event, err := store.GetEvent(r.Context(), db, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "failed to load event")
			return
		}
		if event == nil {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}

		sources, err := store.ListSources(r.Context(), db, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "failed to load sources")
			return
		}
		if sources == nil {
			sources = []model.Source{}
		}
		writeJSON(w, http.StatusOK, detailEnvelope{Data: sources})
	}
}

// changeItem is the change-feed wire shape: a flattened view of a change_log row
// joined with the owning event's current update_state. Field names match the
// PLAN contract ({event_id, field, old, new, update_state, changed_at}); they
// intentionally differ from model.ChangeLog's column names.
type changeItem struct {
	EventID     string  `json:"event_id"`
	Field       string  `json:"field"`
	Old         *string `json:"old"`
	New         *string `json:"new"`
	UpdateState string  `json:"update_state"`
	ChangedAt   string  `json:"changed_at"`
}

// handleListChanges serves GET /api/v1/events/changes: the field-level change
// feed, keyset-paginated under the SAME cursor envelope as the events list and
// filterable by ?since (RFC3339, inclusive >=).
//
// The store change cursor is an int64 change_log id; the API exposes it through
// its OWN opaque base64 token over the int64 change_log id (the events Cursor
// codec requires two non-empty fields, so the change feed carries a dedicated
// single-id cursor). The wire contract is identical: an opaque ?cursor= string
// in the same Envelope/PageInfo shape.
func handleListChanges(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		limit, _ := clampLimit(q.Get("limit"))

		afterID, err := decodeChangeCursor(q.Get("cursor"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is not valid")
			return
		}

		rows, next, err := store.ListChanges(r.Context(), db, store.ChangeFilter{
			Since:  q.Get("since"),
			Limit:  limit,
			Cursor: afterID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "failed to list changes")
			return
		}

		// Resolve each row's event update_state. update_state lives on the event,
		// not the change_log row, so we look it up per distinct event id (page is
		// capped at maxLimit, and distinct ids are memoized to bound the work).
		stateByEvent := map[string]string{}
		items := make([]changeItem, 0, len(rows))
		for _, c := range rows {
			state, ok := stateByEvent[c.EventID]
			if !ok {
				if ev, err := store.GetEvent(r.Context(), db, c.EventID); err != nil {
					writeError(w, http.StatusInternalServerError, "internal", "failed to resolve change state")
					return
				} else if ev != nil {
					state = ev.UpdateState
				}
				stateByEvent[c.EventID] = state
			}
			items = append(items, changeItem{
				EventID:     c.EventID,
				Field:       c.FieldPath,
				Old:         c.OldValue,
				New:         c.NewValue,
				UpdateState: state,
				ChangedAt:   c.ChangedAt,
			})
		}

		var nextCursor *string
		var links Links
		if next != 0 {
			s := encodeChangeCursor(next)
			nextCursor = &s
			links.NextURL = nextPageURL(r, s)
		}

		env := Envelope{
			Data: items,
			Page: PageInfo{
				NextCursor: nextCursor,
				HasMore:    next != 0,
				Limit:      limit,
			},
		}
		// Task 3.4: route the change feed through the same negotiator as the events
		// list so both encodings carry the same field set and the cursor travels in
		// a format-independent rel="next" Link header (render.go doc contract).
		Respond(w, r, newChangeListView(env, items), links)
	}
}

// changeListView wraps a change-feed page so it renders as either the JSON
// Envelope or a Markdown table. It carries the typed items slice alongside the
// Envelope so the Markdown rendering does not depend on a type assertion over
// Envelope.Data.
type changeListView struct {
	env   Envelope
	items []changeItem
}

// newChangeListView builds the view from the already-assembled Envelope so the
// JSON shape is byte-identical to the pre-3.4 handler output.
func newChangeListView(env Envelope, items []changeItem) changeListView {
	return changeListView{env: env, items: items}
}

func (v changeListView) JSONBody() any { return v.env }

// changeColumns is the column order shared by the JSON change-feed shape and the
// Markdown table: {event_id, field, old, new, update_state, changed_at}.
var changeColumns = []string{"event_id", "field", "old", "new", "update_state", "changed_at"}

func (v changeListView) Markdown() string {
	rows := make([][]string, 0, len(v.items))
	for _, c := range v.items {
		rows = append(rows, []string{
			mdText(c.EventID),
			mdText(c.Field),
			mdTextPtr(c.Old),
			mdTextPtr(c.New),
			mdText(c.UpdateState),
			mdText(c.ChangedAt),
		})
	}
	return mdTable(changeColumns, rows)
}

// encodeChangeCursor renders the int64 change_log id as an opaque URL-safe
// base64 token, matching the events cursor's opacity contract so clients treat
// it as a black-box continuation handle.
func encodeChangeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

// decodeChangeCursor parses a token produced by encodeChangeCursor. An empty
// string decodes to 0 with no error ("from the beginning"); anything malformed
// or non-positive returns ErrInvalidCursor so the handler emits invalid_cursor.
func decodeChangeCursor(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, ErrInvalidCursor
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidCursor
	}
	return id, nil
}
