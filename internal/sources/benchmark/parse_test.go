package benchmark

import (
	"context"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/normalize"
)

const nvidiaFixture = `<!doctype html><html><head>
<meta property="og:description" content="Watch the Best of GTC sessions on demand and explore insights on agentic AI, physical AI, inference, and AI factories from global experts.">
<script type="application/ld+json">{
  "@context":"https://schema.org",
  "@type":"Event",
  "name":"AI Conference",
  "description":"Join NVIDIA GTC 2027 in San Jose, March 15-18. Sign up for registration updates or explore GTC sessions on AI, robotics, healthcare, and more.",
  "url":"https://www.nvidia.com/gtc/",
  "startDate":"2027-03-15T00:00:00-00:00",
  "endDate":"2027-03-18T23:59:00-00:00",
  "eventAttendanceMode":"https://schema.org/MixedEventAttendanceMode",
  "organizer":{"@type":"Organization","name":"NVIDIA"},
  "location":[{"@type":"Place","name":"San Jose, CA"},{"@type":"VirtualLocation","url":"https://www.nvidia.com/gtc/"}]
}</script></head><body></body></html>`

const medicaFixture = `<!doctype html><html><head>
<script type="application/ld+json">{
  "@context":"https://schema.org/",
  "@type":"Event",
  "name":"MEDICA",
  "description":"Trade fair for medical technology & healthcare",
  "url":"https://www.medica-tradefair.com/",
  "startDate":"2026-11-16T09:00:00+02:00",
  "endDate":"2026-11-19T18:00:00+02:00",
  "eventAttendanceMode":"https://schema.org/OfflineEventAttendanceMode",
  "organizer":{"@type":"Organization","name":"Messe Düsseldorf GmbH"},
  "location":{"@type":"Place","name":"Messe Düsseldorf GmbH","address":{"@type":"PostalAddress","addressLocality":"Düsseldorf","addressCountry":"DE"}}
}</script></head><body></body></html>`

const techCrunchFixture = `<!doctype html><html><head>
<meta property="og:title" content="TechCrunch Disrupt 2026">
<meta property="og:description" content="TechCrunch Disrupt 2026 brings founders, investors, and technology leaders to San Francisco.">
<meta property="og:url" content="https://techcrunch.com/events/techcrunch-disrupt/">
</head><body></body></html>`

func TestDiscover_ReturnsThirtyTwoBenchmarkRefs(t *testing.T) {
	refs, err := New().Discover(context.Background(), nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 32 {
		t.Fatalf("refs len = %d, want 32", len(refs))
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref.EventID == "" || ref.URL == "" {
			t.Fatalf("empty ref field: %+v", ref)
		}
		if seen[ref.EventID] {
			t.Fatalf("duplicate event id %q", ref.EventID)
		}
		seen[ref.EventID] = true
	}
}

func TestParse_ICMLFromCatalogFallback(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://icml.cc/Conferences/2026", "<!doctype html><html><head></head><body></body></html>"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.EventID != "benchmark-icml-2026" {
		t.Fatalf("EventID = %q", parsed.EventID)
	}
	if parsed.StartRaw == nil || *parsed.StartRaw != "2026-07-06" {
		t.Fatalf("StartRaw = %v", parsed.StartRaw)
	}
	if parsed.EndRaw == nil || *parsed.EndRaw != "2026-07-11" {
		t.Fatalf("EndRaw = %v", parsed.EndRaw)
	}
	if parsed.Country == nil || *parsed.Country != "KR" {
		t.Fatalf("Country = %v", parsed.Country)
	}
	if parsed.VenueName == nil || *parsed.VenueName != "COEX Convention & Exhibition Center" {
		t.Fatalf("VenueName = %v", parsed.VenueName)
	}
	if parsed.Actions.CanRegister == nil || !*parsed.Actions.CanRegister {
		t.Fatalf("CanRegister = %v, want true", parsed.Actions.CanRegister)
	}
}

func TestParse_RejectsURLOutsideCatalog(t *testing.T) {
	_, err := New().Parse(context.Background(), result("https://example.com/not-cataloged", "<!doctype html><html></html>"))
	if err == nil {
		t.Fatal("Parse returned nil error for uncataloged URL")
	}
	if !strings.Contains(err.Error(), "not in catalog") {
		t.Fatalf("error = %v, want not in catalog", err)
	}
}

func TestParse_UsesCatalogFallbackDatesAndActions(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://www.ces.tech/", "<!doctype html><html><head></head><body></body></html>"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.EventID != "benchmark-ces-2027" {
		t.Fatalf("EventID = %q", parsed.EventID)
	}
	if parsed.StartRaw == nil || *parsed.StartRaw != "2027-01-06" {
		t.Fatalf("StartRaw = %v", parsed.StartRaw)
	}
	if parsed.EndRaw == nil || *parsed.EndRaw != "2027-01-09" {
		t.Fatalf("EndRaw = %v", parsed.EndRaw)
	}
	if parsed.Actions.CanExhibit == nil || !*parsed.Actions.CanExhibit {
		t.Fatalf("CanExhibit = %v, want true", parsed.Actions.CanExhibit)
	}
	if parsed.Actions.ExhibitURL == nil || *parsed.Actions.ExhibitURL == "" {
		t.Fatalf("ExhibitURL = %v, want source-backed URL", parsed.Actions.ExhibitURL)
	}
	if parsed.SummaryText == nil || *parsed.SummaryText == "" {
		t.Fatalf("SummaryText = %v, want catalog fallback summary", parsed.SummaryText)
	}
}

func TestParse_NormalizeRecordsMissingActionHonestyForFallback(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://convention.bio.org/future-dates", "<!doctype html><html><head></head><body></body></html>"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	event, err := normalize.Normalize(parsed, "2026-06-23T00:00:00Z")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	if event.StartDate == nil || *event.StartDate != "2027-06-07" {
		t.Fatalf("StartDate = %v, want 2027-06-07", event.StartDate)
	}
	if event.HomepageURL == nil || *event.HomepageURL != "https://convention.bio.org/future-dates" {
		t.Fatalf("HomepageURL = %v, want official URL", event.HomepageURL)
	}
	for _, field := range []string{
		"actions.can_register",
		"actions.can_exhibit",
		"actions.can_sponsor",
		"actions.has_startup_program",
		"register_url",
		"exhibit_url",
		"registration_deadline",
		"exhibitor_deadline",
	} {
		if !containsString(event.MissingFields, field) {
			t.Fatalf("missing_fields = %v, want %q", event.MissingFields, field)
		}
	}
}

func TestParse_NvidiaGTCFromEventJSONLD(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://www.nvidia.com/gtc/", nvidiaFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.EventID != "benchmark-nvidia-gtc-2027" {
		t.Fatalf("EventID = %q", parsed.EventID)
	}
	if parsed.Name != "NVIDIA GTC 2027" {
		t.Fatalf("Name = %q", parsed.Name)
	}
	if parsed.StartRaw == nil || *parsed.StartRaw != "2027-03-15" {
		t.Fatalf("StartRaw = %v", parsed.StartRaw)
	}
	if parsed.Country == nil || *parsed.Country != "US" {
		t.Fatalf("Country = %v", parsed.Country)
	}
	if parsed.Format == nil || *parsed.Format != "hybrid" {
		t.Fatalf("Format = %v", parsed.Format)
	}
	if parsed.HomepageURL == nil || *parsed.HomepageURL != "https://www.nvidia.com/gtc/" {
		t.Fatalf("HomepageURL = %v", parsed.HomepageURL)
	}
}

func TestParse_MedicaFromEventJSONLD(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://www.medica-tradefair.com/", medicaFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.EventID != "benchmark-medica-2026" {
		t.Fatalf("EventID = %q", parsed.EventID)
	}
	if parsed.Country == nil || *parsed.Country != "DE" {
		t.Fatalf("Country = %v", parsed.Country)
	}
	if parsed.City == nil || *parsed.City != "Düsseldorf" {
		t.Fatalf("City = %v", parsed.City)
	}
	if parsed.ClassifyText == "" {
		t.Fatalf("ClassifyText empty")
	}
}

func TestParse_TechCrunchOpenGraphPreservesCatalogDates(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://techcrunch.com/events/techcrunch-disrupt/", techCrunchFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.EventID != "benchmark-techcrunch-disrupt-2026" {
		t.Fatalf("EventID = %q", parsed.EventID)
	}
	if parsed.StartRaw == nil || *parsed.StartRaw != "2026-10-13" {
		t.Fatalf("StartRaw = %v", parsed.StartRaw)
	}
	if parsed.EndRaw == nil || *parsed.EndRaw != "2026-10-15" {
		t.Fatalf("EndRaw = %v", parsed.EndRaw)
	}
	if parsed.Format == nil || *parsed.Format != "onsite" {
		t.Fatalf("Format = %v", parsed.Format)
	}
	if parsed.SummaryText == nil || *parsed.SummaryText != "TechCrunch Disrupt 2026 is a startup and technology conference in San Francisco with founder, investor, speaker, exhibit, and audience-choice programming." {
		t.Fatalf("SummaryText = %v", parsed.SummaryText)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func result(url string, body string) *fetch.Result {
	return &fetch.Result{
		URL:        url,
		StatusCode: 200,
		Body:       []byte(body),
	}
}
