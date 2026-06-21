package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/smpain/event-intelligence-api/internal/api"
	"github.com/smpain/event-intelligence-api/internal/classify"
	"github.com/smpain/event-intelligence-api/internal/model"
)

// metaServer spins up the read API with no seed data; the meta/schema/openapi
// endpoints are data-independent.
func metaServer(t *testing.T) string {
	t.Helper()
	srv := newServer(t, nil)
	return srv.URL
}

// TestSchemaEndpoint asserts GET /api/v1/schema returns the v0.1 descriptor with
// the schema version and supported_versions list.
func TestSchemaEndpoint(t *testing.T) {
	base := metaServer(t)
	resp, err := http.Get(base + "/api/v1/schema")
	if err != nil {
		t.Fatalf("GET schema: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var body struct {
		SchemaVersion     string   `json:"schema_version"`
		SupportedVersions []string `json:"supported_versions"`
		Fields            []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Nullable bool   `json:"nullable"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.SchemaVersion != model.SchemaVersion {
		t.Errorf("schema_version = %q, want %q", body.SchemaVersion, model.SchemaVersion)
	}
	if len(body.SupportedVersions) != 1 || body.SupportedVersions[0] != "0.1" {
		t.Errorf("supported_versions = %v, want [0.1]", body.SupportedVersions)
	}
	if len(body.Fields) == 0 {
		t.Errorf("fields is empty, want the v0.1 field descriptors")
	}
	// A couple of representative required fields must be described.
	have := map[string]bool{}
	for _, f := range body.Fields {
		have[f.Name] = true
	}
	for _, want := range []string{"event_id", "name", "categories", "sources", "update_state"} {
		if !have[want] {
			t.Errorf("schema descriptor missing field %q", want)
		}
	}
}

// TestMetaIndex asserts GET /api/v1 returns the meta index with version,
// schema_version, endpoints, quota, and vocabularies (categories from the
// classify SSOT, venues, update_state, status).
func TestMetaIndex(t *testing.T) {
	base := metaServer(t)
	resp, err := http.Get(base + "/api/v1")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Version       string            `json:"version"`
		SchemaVersion string            `json:"schema_version"`
		Endpoints     map[string]string `json:"endpoints"`
		Quota         struct {
			Keyless struct {
				PerMin int `json:"per_min"`
				PerDay int `json:"per_day"`
			} `json:"keyless"`
		} `json:"quota"`
		Vocabularies struct {
			Categories  []string `json:"categories"`
			Venues      []string `json:"venues"`
			UpdateState []string `json:"update_state"`
			Status      []string `json:"status"`
		} `json:"vocabularies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Version != "v1" {
		t.Errorf("version = %q, want v1", body.Version)
	}
	if body.SchemaVersion != "0.1" {
		t.Errorf("schema_version = %q, want 0.1", body.SchemaVersion)
	}
	if len(body.Endpoints) == 0 {
		t.Errorf("endpoints is empty")
	}
	if body.Quota.Keyless.PerMin != 60 {
		t.Errorf("quota.keyless.per_min = %d, want 60", body.Quota.Keyless.PerMin)
	}
	if body.Quota.Keyless.PerDay != 2000 {
		t.Errorf("quota.keyless.per_day = %d, want 2000", body.Quota.Keyless.PerDay)
	}

	// Categories must be exactly the classify SSOT, in order.
	if len(body.Vocabularies.Categories) != len(classify.Categories) {
		t.Fatalf("vocabularies.categories = %v, want %v", body.Vocabularies.Categories, classify.Categories)
	}
	for i, c := range classify.Categories {
		if body.Vocabularies.Categories[i] != c {
			t.Errorf("vocabularies.categories[%d] = %q, want %q", i, body.Vocabularies.Categories[i], c)
		}
	}
	wantVenues := map[string]bool{"coex": true, "kintex": true}
	if len(body.Vocabularies.Venues) != len(wantVenues) {
		t.Errorf("vocabularies.venues = %v, want coex+kintex", body.Vocabularies.Venues)
	}
	for _, v := range body.Vocabularies.Venues {
		if !wantVenues[v] {
			t.Errorf("unexpected venue %q", v)
		}
	}
	if len(body.Vocabularies.UpdateState) == 0 {
		t.Errorf("vocabularies.update_state is empty")
	}
	if len(body.Vocabularies.Status) == 0 {
		t.Errorf("vocabularies.status is empty")
	}
}

// TestOpenAPIServed asserts GET /api/v1/openapi.yaml returns a YAML document.
func TestOpenAPIServed(t *testing.T) {
	base := metaServer(t)
	resp, err := http.Get(base + "/api/v1/openapi.yaml")
	if err != nil {
		t.Fatalf("GET openapi: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	s := string(b)
	if !strings.Contains(s, "openapi:") {
		t.Errorf("openapi.yaml missing 'openapi:' version key")
	}
	if !strings.Contains(s, "paths:") {
		t.Errorf("openapi.yaml missing 'paths:' section")
	}
}

// TestLLMsTxtServed asserts GET /llms.txt returns a non-empty text summary.
func TestLLMsTxtServed(t *testing.T) {
	base := metaServer(t)
	resp, err := http.Get(base + "/llms.txt")
	if err != nil {
		t.Fatalf("GET llms.txt: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if len(strings.TrimSpace(string(b))) == 0 {
		t.Errorf("llms.txt is empty")
	}
}

// openapiPaths extracts the top-level path keys from the embedded openapi.yaml.
// It scans the lines under "paths:" for two-space-indented "/...:" keys. This is
// a deliberately minimal parser so the test pulls in no YAML dependency.
func openapiPaths(t *testing.T, doc string) map[string]bool {
	t.Helper()
	paths := map[string]bool{}
	lines := strings.Split(doc, "\n")
	inPaths := false
	// A path key is indented exactly two spaces and ends with a colon.
	pathRe := regexp.MustCompile(`^  (/\S*):\s*$`)
	for _, ln := range lines {
		trimmed := strings.TrimRight(ln, " \t")
		if trimmed == "paths:" {
			inPaths = true
			continue
		}
		if !inPaths {
			continue
		}
		// A new top-level key (no indent, non-empty) ends the paths block.
		if len(trimmed) > 0 && trimmed[0] != ' ' {
			break
		}
		if m := pathRe.FindStringSubmatch(trimmed); m != nil {
			paths[m[1]] = true
		}
	}
	return paths
}

// chiToOpenAPIPath maps a chi route pattern (already {param} style) to the
// OpenAPI path. They use the same brace syntax, so the prefix is identical.
func chiToOpenAPIPath(p string) string {
	// chi may register a trailing slash variant; normalize it away (except root).
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// TestRouteOpenAPIConsistency asserts every chi-registered /api/v1 route appears
// as a path key in openapi.yaml (route ↔ OpenAPI consistency). The openapi.yaml
// document static asset is itself served at /api/v1/openapi.yaml; that doc route
// is excluded from the assertion because the spec does not (and should not)
// describe the spec file as an API resource.
func TestRouteOpenAPIConsistency(t *testing.T) {
	doc := api.OpenAPISpec()
	specPaths := openapiPaths(t, doc)
	if len(specPaths) == 0 {
		t.Fatalf("no paths parsed from openapi.yaml")
	}

	// Build the live chi router exactly as production does.
	router := chi.NewRouter()
	api.RegisterRoutes(router, nil)

	excluded := map[string]bool{
		"/api/v1/openapi.yaml": true, // spec asset, not an API resource
		"/llms.txt":            true, // doc asset, not under /api/v1
	}

	err := chi.Walk(router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		p := chiToOpenAPIPath(route)
		if !strings.HasPrefix(p, "/api/v1") {
			return nil
		}
		if excluded[p] {
			return nil
		}
		if !specPaths[p] {
			t.Errorf("registered route %s %q is not documented as a path in openapi.yaml", method, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
}
