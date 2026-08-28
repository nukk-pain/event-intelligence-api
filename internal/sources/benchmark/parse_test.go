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

func TestDiscover_ReturnsSeventyBenchmarkRefs(t *testing.T) {
	refs, err := New().Discover(context.Background(), nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 70 {
		t.Fatalf("refs len = %d, want 70", len(refs))
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

func TestCatalog_EventIDsAndURLsAreUnique(t *testing.T) {
	ids := map[string]bool{}
	urls := map[string]bool{}
	for _, e := range catalog {
		if e.EventID == "" {
			t.Fatal("catalog entry with empty EventID")
		}
		if e.URL == "" {
			t.Fatalf("catalog entry %q with empty URL", e.EventID)
		}
		if ids[e.EventID] {
			t.Fatalf("duplicate EventID %q", e.EventID)
		}
		if urls[e.URL] {
			t.Fatalf("duplicate URL %q (EventID %q)", e.URL, e.EventID)
		}
		ids[e.EventID] = true
		urls[e.URL] = true
	}
}

// refreshFamilies is the 2026-08-29 daily-refresh set: the 14 rolled-forward
// families (5 with a confirmed next edition, 9 honest family/TBA records) plus
// the 25 newly added families — 39 in total.
var refreshFamilies = []string{
	// Rollovers with a confirmed next edition.
	"benchmark-robotics-summit-2027",
	"benchmark-vivatech-2027",
	"benchmark-ieee-icra-2027",
	"benchmark-acl-2027",
	"benchmark-siggraph-2027",
	// Rollovers as honest stable family/TBA records.
	"benchmark-medical-taiwan",
	"benchmark-himss-ai-healthcare-forum-boston",
	"benchmark-gitex-ai-europe",
	"benchmark-waic-shanghai",
	"benchmark-world-robot-conference-beijing",
	"benchmark-icml",
	"benchmark-kdd",
	"benchmark-ijcai",
	"benchmark-khf",
	// Additions.
	"benchmark-mwc-barcelona-2027",
	"benchmark-4yfn-2027",
	"benchmark-jpm-healthcare-2027",
	"benchmark-gitex-global-2026",
	"benchmark-hannover-messe-2027",
	"benchmark-vive-2027",
	"benchmark-ecr-2027",
	"benchmark-cmef-2026",
	"benchmark-corl-2026",
	"benchmark-humanoids-2026",
	"benchmark-rss-2027",
	"benchmark-eccv",
	"benchmark-iccv-2027",
	"benchmark-naacl-2027",
	"benchmark-interspeech-2027",
	"benchmark-gitex-asia-2027",
	"benchmark-ivs-kyoto",
	"benchmark-sus-hi-tech-2027",
	"benchmark-south-summit",
	"benchmark-bits-and-pretzels-2026",
	"benchmark-bio-europe-spring-2027",
	"benchmark-cphi-2027",
	"benchmark-slas-2027",
	"benchmark-automate-2027",
	"benchmark-irex",
}

func TestCatalog_ContainsAllRequestedRolloverAndAdditionFamilies(t *testing.T) {
	present := map[string]bool{}
	for _, e := range catalog {
		present[e.EventID] = true
	}
	var missing []string
	for _, id := range refreshFamilies {
		if !present[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("missing refresh families: %v", missing)
	}
	// Every refresh family is discoverable (event ID + URL both populated).
	refs, err := New().Discover(context.Background(), nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	refByID := map[string]string{}
	for _, ref := range refs {
		refByID[ref.EventID] = ref.URL
	}
	for _, id := range refreshFamilies {
		u, ok := refByID[id]
		if !ok || u == "" {
			t.Fatalf("refresh family %q not discoverable (url %q)", id, u)
		}
	}
}

func TestParse_TBAFamilyNormalizesLowDateConfidenceWithDateMissingFields(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://icml.cc/Conferences/FutureMeetings", "<!doctype html><html><head></head><body></body></html>"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.EventID != "benchmark-icml" {
		t.Fatalf("EventID = %q, want benchmark-icml", parsed.EventID)
	}

	event, err := normalize.Normalize(parsed, "2026-08-29T00:00:00Z")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if event.StartDate != nil || event.EndDate != nil {
		t.Fatalf("TBA entry must have null dates, got start=%v end=%v", event.StartDate, event.EndDate)
	}
	if event.DateConfidence != "low" {
		t.Fatalf("DateConfidence = %q, want low", event.DateConfidence)
	}
	for _, field := range []string{"start_date", "end_date"} {
		if !containsString(event.MissingFields, field) {
			t.Fatalf("missing_fields = %v, want %q", event.MissingFields, field)
		}
	}
}

func TestParse_UnknownInternationalCountryDoesNotFallBackToKR(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://icml.cc/Conferences/FutureMeetings", "<!doctype html><html><head></head><body></body></html>"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	event, err := normalize.Normalize(parsed, "2026-08-29T00:00:00Z")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	// ICML 2027 is officially slated for South America with no country
	// published; the catalog stores the ISO 3166-1 unknown sentinel ZZ rather
	// than letting normalize's default (KR) claim a country.
	if event.Country != "ZZ" {
		t.Fatalf("Country = %q, want ZZ (unknown international, must not be KR)", event.Country)
	}
}

func TestParse_KHFFamilyEntryHasNoStaleEditionDeadline(t *testing.T) {
	parsed, err := New().Parse(context.Background(), result("https://khospital.org/", "<!doctype html><html><head></head><body></body></html>"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if parsed.EventID != "benchmark-khf" {
		t.Fatalf("EventID = %q", parsed.EventID)
	}
	// The 2026 visitor-guide deadline (2026-08-18) belonged to the ended
	// edition; the family/TBA row must not carry it forward.
	if parsed.Actions.RegistrationDeadline != nil {
		t.Fatalf("RegistrationDeadline = %v, want nil for family row", *parsed.Actions.RegistrationDeadline)
	}
	if parsed.StartRaw != nil && *parsed.StartRaw != "" {
		t.Fatalf("StartRaw = %v, want empty for family row", *parsed.StartRaw)
	}
	if parsed.Country == nil || *parsed.Country != "KR" {
		t.Fatalf("Country = %v, want KR", parsed.Country)
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
