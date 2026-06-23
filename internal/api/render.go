package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/model"
)

// render.go implements Task 3.4: content negotiation between the default JSON
// wire format and a token-cheap Markdown rendering (IDEA Serialization Policy).
//
// All three read-path response kinds — the events list, the change feed, and a
// single-event detail — route through Respond(), which:
//
//   - picks JSON (default) or Markdown from ?format / Accept (see negotiate);
//   - for JSON, emits the existing Envelope / detail shapes unchanged;
//   - for Markdown, renders a SINGLE-HEADER-ROW table (collections) or a section
//     (detail), so keys are never repeated per row;
//   - sets the pagination cursor as a Link: <...; rel="next"> RESPONSE HEADER for
//     BOTH encodings, so the continuation token is never trapped inside a table;
//   - guards against Markdown injection in free-text: control chars are escaped
//     and URLs are angle-wrapped (never raw HTML).

// Links carries response-level hypermedia. NextURL, when non-empty, is emitted
// as `Link: <NextURL>; rel="next"` for both JSON and Markdown so the cursor is
// format-independent.
type Links struct {
	NextURL string
}

// renderable is implemented by the per-endpoint view types. JSONBody returns the
// value to JSON-encode (the existing Envelope / detail shapes); Markdown returns
// the Markdown rendering. Keeping both on one type guarantees the two encodings
// expose the same field set.
type renderable interface {
	JSONBody() any
	Markdown() string
}

// negotiate resolves the response format. ?format overrides Accept entirely;
// otherwise Accept: text/markdown selects Markdown. Anything else is JSON.
func negotiate(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "md", "markdown":
		return "md"
	case "json":
		return "json"
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/markdown") {
		return "md"
	}
	return "json"
}

// Respond writes payload to w in the negotiated format, attaching the rel="next"
// Link header (if any) BEFORE writing the status line so it applies to both
// encodings.
func Respond(w http.ResponseWriter, r *http.Request, payload renderable, links Links) {
	// Vary: Accept is REQUIRED for correctness behind a URL-keyed shared cache
	// (Cloudflare, PLAN.md): the same URL serves JSON or Markdown depending on the
	// Accept header (negotiate), so a cache must key on Accept too. Without it a
	// cached JSON body could be served to an Accept: text/markdown client (or the
	// reverse) before any content-derived ETag revalidation.
	w.Header().Add("Vary", "Accept")
	if links.NextURL != "" {
		w.Header().Set("Link", "<"+links.NextURL+">; rel=\"next\"")
	}
	if negotiate(r) == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(payload.Markdown()))
		return
	}
	writeJSON(w, http.StatusOK, payload.JSONBody())
}

// --- Markdown injection guard ---------------------------------------------

// mdCellReplacer escapes the Markdown control characters that, inside a table
// cell or free-text span, would let a field break out of its cell or inject
// active formatting (links, emphasis, code spans, raw pipes). The backslash is
// escaped first by virtue of being listed first.
var mdCellReplacer = strings.NewReplacer(
	"\\", "\\\\",
	"`", "\\`",
	"|", "\\|",
	"*", "\\*",
	"_", "\\_",
	"[", "\\[",
	"]", "\\]",
	"(", "\\(",
	")", "\\)",
	"<", "\\<",
	">", "\\>",
	"&", "\\&",
	"\r\n", " ",
	"\n", " ",
	"\r", " ",
)

// mdText escapes a free-text value for safe inclusion in a Markdown table cell
// or section body. It never emits raw HTML and collapses newlines so a value
// cannot break the single-line table grammar.
func mdText(s string) string {
	if s == "" {
		return ""
	}
	return mdCellReplacer.Replace(s)
}

// mdTextPtr is mdText for nullable fields, rendering nil as an empty cell.
func mdTextPtr(p *string) string {
	if p == nil {
		return ""
	}
	return mdText(*p)
}

// mdURL renders a URL angle-wrapped (<https://…>) which is the autolink form
// that does not require escaping the URL body, and prevents a crafted URL from
// being interpreted as inline-link syntax. An empty URL renders as an empty
// cell.
func mdURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	// Defensively strip angle brackets and newlines so the autolink cannot be
	// closed early or span lines.
	u = strings.NewReplacer("<", "", ">", "", "\n", "", "\r", "", " ", "%20").Replace(u)
	return "<" + u + ">"
}

// --- shared field projection ----------------------------------------------

var eventColumns = []string{"event_id", "name", "start_date", "end_date", "venue_id", "categories", "status"}

// eventRow projects an Event onto the shared, injection-guarded column values in
// eventColumns order.
func eventRow(e model.Event) []string {
	var venueID string
	if e.Venue != nil && e.Venue.VenueID != nil {
		venueID = *e.Venue.VenueID
	}
	return []string{
		mdText(e.EventID),
		mdText(e.Name),
		mdTextPtr(e.StartDate),
		mdTextPtr(e.EndDate),
		mdText(venueID),
		mdText(strings.Join(e.Categories, ", ")),
		mdText(e.Status),
	}
}

// mdTable renders a single-header-row Markdown table: one header row, one
// separator row, then one row per record. Keys appear exactly once (header),
// which is the token-cheapness property the Serialization Policy requires.
func mdTable(columns []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("| " + strings.Join(columns, " | ") + " |\n")
	sep := make([]string, len(columns))
	for i := range sep {
		sep[i] = "---"
	}
	b.WriteString("| " + strings.Join(sep, " | ") + " |\n")
	for _, row := range rows {
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	return b.String()
}

// --- event collection view -------------------------------------------------

// eventListView wraps an event page so it can render as either the JSON Envelope
// or a Markdown table.
type eventListView struct {
	env Envelope
}

// newEventListView builds the view from the already-assembled Envelope so the
// JSON shape is byte-identical to the pre-3.4 handler output.
func newEventListView(env Envelope) eventListView { return eventListView{env: env} }

func (v eventListView) JSONBody() any { return v.env }

func (v eventListView) Markdown() string {
	events, _ := v.env.Data.([]model.Event)
	rows := make([][]string, 0, len(events))
	for _, e := range events {
		rows = append(rows, eventRow(e))
	}
	table := mdTable(eventColumns, rows)
	// Carry the category facet in the Markdown encoding too, so both formats
	// expose the same field set (renderable contract). It is a single
	// collection-level line, not per-row, keeping the rendering token-cheap.
	if len(v.env.CategoryCounts) > 0 {
		parts := make([]string, 0, len(v.env.CategoryCounts))
		for _, c := range v.env.CategoryCounts {
			parts = append(parts, fmt.Sprintf("%s %d", c.Category, c.Count))
		}
		return "category_counts: " + strings.Join(parts, ", ") + "\n\n" + table
	}
	return table
}

// --- event detail view -----------------------------------------------------

// eventDetailView wraps a single event so the detail route renders as either the
// JSON detail envelope (unchanged from the pre-3.4 shape) or a Markdown section.
// It carries the same pinned field set as the list view (eventColumns), so the
// detail markdown and the OpenAPI text/markdown advertisement agree with code.
type eventDetailView struct {
	event *model.Event
}

// newEventDetailView builds the detail view from the loaded event.
func newEventDetailView(e *model.Event) eventDetailView { return eventDetailView{event: e} }

// JSONBody returns the existing single-resource envelope so the JSON wire shape
// is byte-identical to the pre-3.4 handler output ({"data": event}).
func (v eventDetailView) JSONBody() any { return detailEnvelope{Data: v.event} }

// Markdown renders the pinned event field set as a key/value section. Keys appear
// exactly once (one row per field), preserving the token-cheapness property of
// the Serialization Policy while staying readable for a single resource.
func (v eventDetailView) Markdown() string {
	values := eventRow(*v.event)
	var b strings.Builder
	b.WriteString("| field | value |\n")
	b.WriteString("| --- | --- |\n")
	for i, col := range eventColumns {
		b.WriteString("| " + col + " | " + values[i] + " |\n")
	}
	return b.String()
}
