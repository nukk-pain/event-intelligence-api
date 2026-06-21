package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/smpain/event-intelligence-api/internal/classify"
	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/static"
)

// meta.go implements the discovery / self-description surface (Task 3.3):
//
//   GET /api/v1            meta index (version, schema, endpoints, quota, vocab)
//   GET /api/v1/schema     v0.1 schema descriptor + supported_versions
//   GET /api/v1/openapi.yaml   the embedded OpenAPI 3.x document
//   GET /llms.txt          embedded agent-facing API summary (root, not /api/v1)
//
// These endpoints are data-independent: they describe the contract, not the
// data, so they take no *sql.DB. The controlled vocabularies are sourced from
// the SSOT (internal/classify for categories, model for the schema version) so
// they never drift from validation.

// supportedVersions is the list of schema versions this build can serve. It is a
// single-element list today; new versions are added here as they ship.
var supportedVersions = []string{model.SchemaVersion}

// metaVenues is the venue vocabulary for v0.1 (the two launch sources). Kept
// local because there is no venue SSOT package yet; revisit when one exists.
var metaVenues = []string{"coex", "kintex"}

// updateStates and statuses mirror the enums in prototype/manual-dataset-schema.md.
// They are surfaced in the meta index so agents can validate filter inputs.
var (
	updateStates = []string{"new", "unchanged", "updated", "conflicting"}
	statuses     = []string{"scheduled", "tentative", "postponed", "cancelled", "ended"}
)

// OpenAPISpec returns the embedded OpenAPI 3.x document as text. Exported so the
// route↔OpenAPI consistency test can parse the same bytes that are served.
func OpenAPISpec() string { return static.OpenAPIYAML }

// registerMetaRoutes mounts the discovery endpoints. The /api/v1-scoped routes
// are registered inside the caller's existing /api/v1 group; /llms.txt is
// registered on the root router because it is intentionally not versioned.
func registerMetaRoutes(v1 chi.Router) {
	v1.Get("/", handleMetaIndex())
	v1.Get("/schema", handleSchema())
	v1.Get("/openapi.yaml", handleOpenAPI())
}

// registerRootMetaRoutes mounts root-level (non-/api/v1) discovery routes.
func registerRootMetaRoutes(r chi.Router) {
	r.Get("/llms.txt", handleLLMsTxt())
}

// fieldDescriptor describes one field of the v0.1 schema for GET /api/v1/schema.
type fieldDescriptor struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Required bool   `json:"required"`
}

// schemaResponse is the GET /api/v1/schema wire shape.
type schemaResponse struct {
	SchemaVersion     string            `json:"schema_version"`
	SupportedVersions []string          `json:"supported_versions"`
	Fields            []fieldDescriptor `json:"fields"`
}

// schemaFields is the v0.1 field descriptor list, mirroring
// prototype/manual-dataset-schema.md. "required" follows validation rule 1.
var schemaFields = []fieldDescriptor{
	{Name: "schema_version", Type: "string", Nullable: false, Required: true},
	{Name: "event_id", Type: "string", Nullable: false, Required: true},
	{Name: "series_id", Type: "string", Nullable: true, Required: false},
	{Name: "name", Type: "string", Nullable: false, Required: true},
	{Name: "name_ko", Type: "string", Nullable: true, Required: false},
	{Name: "name_en", Type: "string", Nullable: true, Required: false},
	{Name: "edition", Type: "string", Nullable: true, Required: false},
	{Name: "start_date", Type: "date", Nullable: true, Required: false},
	{Name: "end_date", Type: "date", Nullable: true, Required: false},
	{Name: "timezone", Type: "string", Nullable: true, Required: false},
	{Name: "date_confidence", Type: "enum(high|medium|low)", Nullable: false, Required: false},
	{Name: "status", Type: "enum(scheduled|tentative|postponed|cancelled|ended)", Nullable: false, Required: false},
	{Name: "format", Type: "enum(onsite|hybrid|online)", Nullable: false, Required: false},
	{Name: "venue", Type: "object", Nullable: true, Required: false},
	{Name: "country", Type: "string", Nullable: false, Required: true},
	{Name: "categories", Type: "string[]", Nullable: false, Required: true},
	{Name: "audience", Type: "string[]", Nullable: false, Required: false},
	{Name: "scale", Type: "object", Nullable: true, Required: false},
	{Name: "actions", Type: "object", Nullable: false, Required: false},
	{Name: "register_url", Type: "string", Nullable: true, Required: false},
	{Name: "exhibit_url", Type: "string", Nullable: true, Required: false},
	{Name: "registration_deadline", Type: "date", Nullable: true, Required: false},
	{Name: "exhibitor_deadline", Type: "date", Nullable: true, Required: false},
	{Name: "cost_hint", Type: "enum(free|paid|mixed|unknown)", Nullable: false, Required: false},
	{Name: "summary", Type: "string", Nullable: true, Required: false},
	{Name: "sources", Type: "object[]", Nullable: false, Required: true},
	{Name: "homepage_url", Type: "string", Nullable: true, Required: false},
	{Name: "last_checked_at", Type: "datetime", Nullable: false, Required: true},
	{Name: "update_state", Type: "enum(new|unchanged|updated|conflicting)", Nullable: false, Required: true},
	{Name: "confidence", Type: "enum(high|medium|low)", Nullable: false, Required: true},
	{Name: "missing_fields", Type: "string[]", Nullable: false, Required: true},
	{Name: "ambiguity_notes", Type: "string", Nullable: true, Required: false},
	{Name: "curated_by", Type: "string", Nullable: false, Required: false},
	{Name: "created_at", Type: "datetime", Nullable: false, Required: false},
	{Name: "updated_at", Type: "datetime", Nullable: false, Required: false},
	{Name: "excluded", Type: "bool", Nullable: false, Required: false},
}

func handleSchema() http.HandlerFunc {
	resp := schemaResponse{
		SchemaVersion:     model.SchemaVersion,
		SupportedVersions: supportedVersions,
		Fields:            schemaFields,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, resp)
	}
}

// metaIndex is the GET /api/v1 wire shape.
type metaIndex struct {
	Version       string            `json:"version"`
	SchemaVersion string            `json:"schema_version"`
	Endpoints     map[string]string `json:"endpoints"`
	Quota         quotaBlock        `json:"quota"`
	Vocabularies  vocabBlock        `json:"vocabularies"`
}

type quotaBlock struct {
	Keyless quotaTier `json:"keyless"`
}

type quotaTier struct {
	PerMin int `json:"per_min"`
	PerDay int `json:"per_day"`
}

type vocabBlock struct {
	Categories  []string `json:"categories"`
	Venues      []string `json:"venues"`
	UpdateState []string `json:"update_state"`
	Status      []string `json:"status"`
}

func handleMetaIndex() http.HandlerFunc {
	resp := metaIndex{
		Version:       "v1",
		SchemaVersion: model.SchemaVersion,
		Endpoints: map[string]string{
			"meta":          "/api/v1",
			"schema":        "/api/v1/schema",
			"openapi":       "/api/v1/openapi.yaml",
			"llms":          "/llms.txt",
			"events":        "/api/v1/events",
			"event_detail":  "/api/v1/events/{event_id}",
			"event_sources": "/api/v1/events/{event_id}/sources",
			"changes":       "/api/v1/events/changes",
		},
		Quota: quotaBlock{
			Keyless: quotaTier{PerMin: 60, PerDay: 2000},
		},
		Vocabularies: vocabBlock{
			Categories:  classify.Categories, // SSOT
			Venues:      metaVenues,
			UpdateState: updateStates,
			Status:      statuses,
		},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleOpenAPI() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(static.OpenAPIYAML))
	}
}

func handleLLMsTxt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(static.LLMsTxt))
	}
}
