package api

import "net/http"

// cache.go centralizes the edge-cache policy for the read-only API.
//
// The public API is read-only and cache-first (see CLAUDE.md hard constraints):
// data only changes when the 24h ingest batch runs, so successful read responses
// are highly cacheable. Before this, only the landing HTML and static assets set
// Cache-Control, so Cloudflare treated every data/API response as DYNAMIC and
// every page open paid a full edge->origin round trip. setEdgeCache attaches the
// shared policy to success responses so Cloudflare (with a "Cache Everything"
// rule) and browsers can serve them from cache.
//
// Freshness after an ingest is handled out-of-band by purging the Cloudflare
// cache (cmd ingest -> internal/cfpurge), which is what makes the long s-maxage
// safe on a daily-updated dataset.
//
// publicCacheControl breakdown:
//   - max-age=120        browsers cache 2 min (covers rapid re-navigation)
//   - s-maxage=3600      shared/edge caches hold 1h; purge-on-ingest keeps it fresh
//   - stale-while-revalidate=86400  serve stale instantly while revalidating for a day
const publicCacheControl = "public, max-age=120, s-maxage=3600, stale-while-revalidate=86400"

// setEdgeCache stamps the shared public cache policy on a success response. It is
// only ever called on 200 responses; error responses (writeError path) must NOT
// carry it so Cloudflare never caches an error or 429.
func setEdgeCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", publicCacheControl)
}
