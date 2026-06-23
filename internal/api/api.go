// Package api implements the cache-first read-only HTTP API. Phase 3 wires the
// full read surface together: Router builds the chi route table (events list,
// detail/sources/changes, schema/meta/openapi/llms) and wraps it in the
// cache-first middleware chain (quota, conditional-GET ETag, concurrency cap,
// response-size cap, error envelope, X-RateLimit headers).
package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/smpain/event-intelligence-api/static"
)

// Router builds the complete read-only API handler: every /api/v1 route plus the
// root-level /llms.txt and /healthz, wrapped in the Task 3.5 middleware chain.
//
// The chain wraps the router from the outermost (first to see the request) to the
// innermost (closest to the handler):
//
//	concurrency cap  -> sheds load with 503 before any work
//	quota            -> per-IP 60/min + 2000/day, emits X-RateLimit-* / 429
//	ETag conditional -> buffers the body, derives an ETag, 304 on If-None-Match
//	response-size cap -> truncates a runaway serialization
//	chi router       -> the route handlers (which use Respond for negotiation)
//
// /healthz is registered on the bare router and therefore sits INSIDE the chain.
// That is intentional: a liveness probe should observe the same quota/concurrency
// posture as real traffic and is cheap enough not to distort it. cfg's zero value
// is valid; missing fields fall back to the documented defaults (see
// MiddlewareConfig.withDefaults). It returns an error only if a trusted-proxy
// CIDR in cfg is malformed.
func Router(db *sql.DB, cfg MiddlewareConfig) (http.Handler, error) {
	cfg = cfg.withDefaults()

	r := chi.NewRouter()
	r.Get("/", handleRoot)
	r.Get("/assets/theme.css", staticAsset("text/css; charset=utf-8", static.ThemeCSS))
	r.Get("/assets/index.css", staticAsset("text/css; charset=utf-8", static.IndexCSS))
	r.Get("/assets/list.css", staticAsset("text/css; charset=utf-8", static.ListCSS))
	r.Get("/assets/detail.css", staticAsset("text/css; charset=utf-8", static.DetailCSS))
	r.Get("/assets/ui.js", staticAsset("application/javascript; charset=utf-8", static.UIJS))
	r.Get("/assets/events.js", staticAsset("application/javascript; charset=utf-8", static.EventsJS))
	r.Get("/assets/detail.js", staticAsset("application/javascript; charset=utf-8", static.DetailJS))
	r.Get("/assets/app.js", staticAsset("application/javascript; charset=utf-8", static.AppJS))
	r.Get("/healthz", handleHealthz)
	RegisterRoutes(r, db)

	quota, err := NewQuotaMiddleware(cfg)
	if err != nil {
		return nil, err
	}

	// Compose from innermost to outermost so the listed order above is the order
	// requests traverse on the way in.
	var h http.Handler = r
	h = NewResponseSizeCap(cfg.MaxResponseSize)(h)
	h = ETagMiddleware()(h)
	h = quota(h)
	h = NewConcurrencyLimiter(cfg.MaxConcurrent)(h)
	return h, nil
}

// Server holds the read handle and the composed handler. It is the convenience
// wrapper used by cmd serve; tests that need a bare router call Routes directly.
type Server struct {
	db      *sql.DB
	handler http.Handler
}

// NewServer builds a Server with the full middleware chain applied. It returns an
// error if the middleware config is invalid (malformed trusted-proxy CIDR).
func NewServer(db *sql.DB, cfg MiddlewareConfig) (*Server, error) {
	h, err := Router(db, cfg)
	if err != nil {
		return nil, err
	}
	return &Server{db: db, handler: h}, nil
}

// Handler exposes the composed http.Handler for serving.
func (s *Server) Handler() http.Handler { return s.handler }

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleRoot is the landing index for the bare domain. Browsers (Accept:
// text/html) get the embedded HTML UI; everything else (curl, agents, JSON
// consumers) gets the JSON service index pointing at the real entry points.
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if prefersHTML(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		setEdgeCache(w)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(static.IndexHTML))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	setEdgeCache(w)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{` +
		`"service":"event-intelligence-api",` +
		`"description":"Free, read-only API for Korean AI/robotics/bio/medical-device industry events (COEX, KINTEX). JSON first; agent-friendly.",` +
		`"version":"v1",` +
		`"docs":{"meta":"/api/v1","schema":"/api/v1/schema","openapi":"/api/v1/openapi.yaml","llms":"/llms.txt"},` +
		`"endpoints":{"events":"/api/v1/events","event":"/api/v1/events/{event_id}","sources":"/api/v1/events/{event_id}/sources","changes":"/api/v1/events/changes"}` +
		`}`))
}

func staticAsset(contentType, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		setEdgeCache(w)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
}

// prefersHTML reports whether the client's Accept header prefers HTML over
// JSON. A request with no Accept header defaults to JSON (the agent-friendly
// default). Browsers always send text/html in Accept.
func prefersHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	html, json := 0.0, 0.0
	for _, part := range strings.Split(accept, ",") {
		seg := strings.TrimSpace(part)
		var media string
		q := 1.0
		if i := strings.Index(seg, ";"); i >= 0 {
			media = strings.TrimSpace(seg[:i])
			for _, p := range strings.Split(seg[i+1:], ";") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "q=") {
					if v, err := strconv.ParseFloat(p[2:], 64); err == nil {
						q = v
					}
				}
			}
		} else {
			media = seg
		}
		switch {
		case media == "text/html" || media == "text/html;charset=utf-8":
			html = q
		case media == "application/json" || media == "application/*" || media == "*/*":
			json = q
		}
	}
	return html > json && html > 0
}
