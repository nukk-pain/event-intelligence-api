package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/opportunity"
	"github.com/smpain/event-intelligence-api/internal/store"
)

// events.go implements GET /api/v1/events: the keyset-paginated, filterable
// event listing. It is the first read endpoint and establishes the shared
// foundation (Envelope, cursor, store.ListEvents) that 3.2–3.5 reuse.

const (
	// defaultLimit is the page size when ?limit is absent.
	defaultLimit = 20
	// maxLimit is the hard cap; a larger ?limit is clamped to this value and the
	// clamped value is echoed back in PageInfo.Limit.
	maxLimit = 100
)

// RegisterRoutes mounts the v1 read API on r, backed by the read-only handle db.
// 3.2–3.5 extend this same function so there is ONE registration point and the
// route table stays in sync with the OpenAPI contract.
func RegisterRoutes(r chi.Router, db *sql.DB) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/events", handleListEvents(db))
		registerDetailRoutes(r, db) // Task 3.2: detail / sources / changes
		registerMetaRoutes(r)       // Task 3.3: meta index, schema, openapi.yaml
	})
	registerRootMetaRoutes(r) // Task 3.3: /llms.txt (intentionally un-versioned)
}

// Routes returns a ready-to-serve http.Handler with the v1 read API mounted,
// for callers (tests, main) that want a standalone handler rather than mounting
// onto an existing router.
func Routes(db *sql.DB) http.Handler {
	r := chi.NewRouter()
	RegisterRoutes(r, db)
	return r
}

// handleListEvents parses filters/limit/cursor, queries the store, and writes
// the shared Envelope as JSON.
func handleListEvents(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		limit, _ := clampLimit(q.Get("limit"))

		cur, err := DecodeCursor(q.Get("cursor"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is not valid")
			return
		}

		filter := store.EventFilter{
			UpdatedSince:       q.Get("updated_since"),
			ChangedSince:       q.Get("changed_since"),
			Category:           q.Get("category"),
			Venue:              q.Get("venue"),
			MinStartDate:       q.Get("since"),
			MaxStartDate:       q.Get("before"),
			Opportunity:        q.Get("opportunity") == "true",
			Actionable:         q.Get("actionable") == "true",
			OpportunityQuality: q.Get("opportunity_quality"),
			Limit:              limit,
			Cursor:             store.EventCursor{UpdatedAt: cur.UpdatedAt, EventID: cur.EventID},
		}

		events, next, err := store.ListEvents(r.Context(), db, filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "failed to list events")
			return
		}

		// Guarantee a non-null JSON array for an empty page.
		if events == nil {
			events = []model.Event{}
		}
		for i := range events {
			opportunity.Apply(&events[i])
		}

		var nextCursor *string
		var links Links
		if next != nil {
			s := EncodeCursor(Cursor{UpdatedAt: next.UpdatedAt, EventID: next.EventID})
			nextCursor = &s
			links.NextURL = nextPageURL(r, s)
		}

		env := Envelope{
			Data: events,
			Page: PageInfo{
				NextCursor: nextCursor,
				HasMore:    next != nil,
				Limit:      limit, // echo the EFFECTIVE (clamped) limit
			},
		}
		// Task 3.4: route JSON and Markdown through one negotiator so both
		// encodings carry the same field set and the cursor travels in a
		// format-independent Link header.
		Respond(w, r, newEventListView(env), links)
	}
}

// nextPageURL builds the absolute-path URL for the next page by replacing the
// cursor query parameter on the current request URL with the supplied opaque
// cursor. The result is what goes inside the `Link: <...>; rel="next"` header,
// identical regardless of the negotiated response format.
func nextPageURL(r *http.Request, cursor string) string {
	u := *r.URL
	q := u.Query()
	q.Set("cursor", cursor)
	u.RawQuery = q.Encode()
	return u.RequestURI()
}

// clampLimit parses ?limit, defaulting to defaultLimit when absent/invalid and
// clamping to [1, maxLimit]. It returns the effective limit and whether the raw
// value was a valid positive integer (currently unused by callers, kept for the
// 3.x endpoints that may surface a warning).
func clampLimit(raw string) (int, bool) {
	if raw == "" {
		return defaultLimit, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit, false
	}
	if n > maxLimit {
		return maxLimit, true
	}
	return n, true
}

// writeJSON serializes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError emits the canonical error envelope. The explicit status argument is
// retained for call-site readability but the wire status is derived from the
// stable code via errorStatus (Task 3.5), so every error response — handler or
// middleware — carries the same shape, including the always-present
// retry_after_s key (null for non-rate-limited codes). The status param is kept
// for documentation at call sites and validated against errorStatus.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	_ = status // wire status comes from errorStatus(code); see Task 3.5 contract.
	WriteError(w, code, msg)
}
