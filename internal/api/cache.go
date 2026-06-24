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

// negotiatedCacheControl is the policy for a CONTENT-NEGOTIATED response whose
// body depends on the Accept header at a single URL (the bare "/" landing, which
// is HTML for browsers and JSON for agents). A shared/edge cache is URL-keyed and
// — on the Cloudflare Free plan — ignores Vary: Accept, so one cached entry for
// "/" would be handed to every client regardless of Accept (the JSON index to
// browsers, or the HTML UI to agents). "private" forbids shared/edge caches from
// storing it while still letting a browser cache its own correct variant; the
// caller pairs this with Vary: Accept (honored by browsers and any Vary-aware
// cache). The heavy assets under /assets/* remain edge-cached by extension.
const negotiatedCacheControl = "private, max-age=120"

// setNegotiatedCache marks an Accept-negotiated 200 so shared/edge caches do not
// store it (avoiding cross-variant pollution at a single URL), and varies on
// Accept for caches that honor it.
func setNegotiatedCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", negotiatedCacheControl)
	w.Header().Add("Vary", "Accept")
}
