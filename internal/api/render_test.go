package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/model"
)

// fieldset is the field set Task 3.4 requires both encodings to carry for each
// event: {event_id, name, start_date, end_date, venue.venue_id, categories,
// status}. Tests assert json and md expose the SAME set.

// TestRender_DefaultIsJSON asserts that with no ?format and no Accept the events
// list is served as application/json.
func TestRender_DefaultIsJSON(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})

	resp, err := http.Get(srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("default Content-Type = %q, want application/json", ct)
	}
}

// TestRender_AcceptMarkdown asserts Accept: text/markdown triggers a markdown
// table response.
func TestRender_AcceptMarkdown(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events", nil)
	req.Header.Set("Accept", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/markdown") {
		t.Fatalf("Accept md Content-Type = %q, want text/markdown", ct)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "| event_id |") {
		t.Fatalf("md body missing single-header-row table; got:\n%s", body)
	}
}

// TestRender_FormatQueryPrecedence asserts ?format=md overrides Accept and
// ?format=json overrides Accept: text/markdown.
func TestRender_FormatQueryPrecedence(t *testing.T) {
	srv := newServer(t, []model.Event{seedEvent("ev-001", "coex", "ai", false)})

	// ?format=md wins over Accept: application/json.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events?format=md", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET md: %v", err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		resp.Body.Close()
		t.Fatalf("?format=md Content-Type = %q, want text/markdown", ct)
	}
	resp.Body.Close()

	// ?format=json wins over Accept: text/markdown.
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/events?format=json", nil)
	req2.Header.Set("Accept", "text/markdown")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET json: %v", err)
	}
	defer resp2.Body.Close()
	if ct := resp2.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("?format=json Content-Type = %q, want application/json", ct)
	}
}

// TestRender_SameFieldSet asserts json and md carry the same field set for an
// event: event_id, name, start_date, end_date, venue.venue_id, categories,
// status.
func TestRender_SameFieldSet(t *testing.T) {
	ev := seedEvent("ev-fields", "coex", "ai", false)
	ev.Name = "AI Summit"
	ev.StartDate = strptr("2026-09-01")
	ev.EndDate = strptr("2026-09-03")
	ev.Status = "scheduled"
	srv := newServer(t, []model.Event{ev})

	// JSON view.
	jenv, _ := getEnvelope(t, srv.URL+"/api/v1/events")
	if len(jenv.Data) != 1 {
		t.Fatalf("json list len = %d, want 1", len(jenv.Data))
	}
	je := jenv.Data[0]

	// Markdown view.
	md := getMarkdown(t, srv.URL+"/api/v1/events?format=md")

	for _, want := range []string{
		je.EventID,
		je.Name,
		derefOr(je.StartDate, ""),
		derefOr(je.EndDate, ""),
		derefOr(je.Venue.VenueID, ""),
		je.Categories[0],
		je.Status,
	} {
		if want == "" {
			continue
		}
		if !strings.Contains(md, want) {
			t.Errorf("md missing field value %q\nmd:\n%s", want, md)
		}
	}

	// Header row contains every required column.
	for _, col := range []string{"event_id", "name", "start_date", "end_date", "venue_id", "categories", "status"} {
		if !strings.Contains(md, col) {
			t.Errorf("md header missing column %q", col)
		}
	}
}

// TestRender_LinkHeaderParity asserts a multi-page result emits an identical
// Link: <...; rel="next"> header for both json and md.
func TestRender_LinkHeaderParity(t *testing.T) {
	var events []model.Event
	for i := 0; i < 25; i++ {
		events = append(events, seedEvent(fmt.Sprintf("ev-%03d", i), "coex", "ai", false))
	}
	srv := newServer(t, events)

	jsonLink := linkHeader(t, srv.URL+"/api/v1/events?limit=10", "")
	mdLink := linkHeader(t, srv.URL+"/api/v1/events?limit=10", "text/markdown")

	if jsonLink == "" {
		t.Fatalf("json multi-page result missing Link next header")
	}
	if mdLink == "" {
		t.Fatalf("md multi-page result missing Link next header")
	}
	if jsonLink != mdLink {
		t.Fatalf("Link header parity broken:\n json: %s\n md:   %s", jsonLink, mdLink)
	}
	if !strings.Contains(jsonLink, `rel="next"`) || !strings.Contains(jsonLink, "cursor=") {
		t.Fatalf("Link header malformed: %s", jsonLink)
	}
}

// TestRender_VaryAccept asserts every negotiated response carries `Vary: Accept`
// so a URL-keyed shared cache (Cloudflare in front, PLAN.md) cannot serve a
// JSON body to a client sending Accept: text/markdown, or vice versa. Asserted
// across both negotiated encodings and for both paginated collection endpoints.
func TestRender_VaryAccept(t *testing.T) {
	var events []model.Event
	for i := 0; i < 5; i++ {
		events = append(events, seedEvent(fmt.Sprintf("ev-%03d", i), "coex", "ai", false))
	}
	srv := newServer(t, events)

	cases := []struct {
		name   string
		url    string
		accept string
	}{
		{"events json", "/api/v1/events", ""},
		{"events md", "/api/v1/events", "text/markdown"},
		{"events format=md", "/api/v1/events?format=md", ""},
		{"detail json", "/api/v1/events/ev-000", ""},
		{"detail md", "/api/v1/events/ev-000", "text/markdown"},
		{"changes json", "/api/v1/events/changes", ""},
		{"changes md", "/api/v1/events/changes", "text/markdown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+tc.url, nil)
			if tc.accept != "" {
				req.Header.Set("Accept", tc.accept)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.url, err)
			}
			defer resp.Body.Close()
			if !vary(resp, "Accept") {
				t.Fatalf("%s: missing `Vary: Accept`, got %q", tc.url, resp.Header.Values("Vary"))
			}
		})
	}
}

// vary reports whether the response's Vary header (across any number of Vary
// header lines) lists the given token, case-insensitively.
func vary(resp *http.Response, token string) bool {
	for _, h := range resp.Header.Values("Vary") {
		for _, part := range strings.Split(h, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// TestRender_MarkdownInjectionGuard asserts free-text fields with markdown
// control sequences (a link payload and backticks) are escaped, not emitted raw.
func TestRender_MarkdownInjectionGuard(t *testing.T) {
	ev := seedEvent("ev-evil", "coex", "ai", false)
	ev.Name = "Pwn](http://evil) `rm -rf` event"
	srv := newServer(t, []model.Event{ev})

	md := getMarkdown(t, srv.URL+"/api/v1/events?format=md")

	// The raw injection substring must not appear verbatim.
	if strings.Contains(md, "](http://evil)") {
		t.Errorf("md leaked raw link injection: %s", md)
	}
	if strings.Contains(md, "`rm -rf`") {
		t.Errorf("md leaked raw backtick code span: %s", md)
	}
}

// --- helpers ---

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func getMarkdown(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Fatalf("%s Content-Type = %q, want text/markdown", url, ct)
	}
	return readBody(t, resp)
}

func linkHeader(t *testing.T, url, accept string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.Header.Get("Link")
}

func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// compile-time guard that envelopeResp is the json shape we read.
var _ = json.Marshal
