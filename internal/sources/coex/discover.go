// Package coex implements the COEX (www.coex.co.kr) Source adapter: WordPress
// sitemap-driven discovery of exhibition detail pages plus a deterministic
// static-HTML parser (parser lands in Task 2.2). COEX serves a standard
// wp-sitemap.xml index whose "exhibitions" custom-post-type is sharded across
// several wp-sitemap-posts-exhibitions-N.xml files. Discovery follows whatever
// shard <loc> entries the live index lists — it never hardcodes shards 1..5.
package coex

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// DefaultBaseURL is the live COEX origin. Tests override it via WithBaseURL to
// point discovery at an httptest fixture server.
const DefaultBaseURL = "https://www.coex.co.kr"

// sitemapIndexPath is the WordPress core sitemap index.
const sitemapIndexPath = "/wp-sitemap.xml"

// exhibitionsShardMarker identifies the exhibitions custom-post-type detail
// shards in the index. WordPress names them wp-sitemap-posts-exhibitions-N.xml.
// The taxonomies-exhibitions* shards (category/dept listings) deliberately do
// NOT match because they list term archive pages, not exhibition detail pages.
const exhibitionsShardMarker = "wp-sitemap-posts-exhibitions-"

// Source is the COEX venue adapter. It implements sources.Source.
type Source struct {
	baseURL string
}

// Option configures a Source.
type Option func(*Source)

// WithBaseURL overrides the origin discovery fetches against. The trailing
// slash, if any, is trimmed. Intended for tests (httptest server).
func WithBaseURL(u string) Option {
	return func(s *Source) { s.baseURL = strings.TrimRight(u, "/") }
}

// New builds a COEX Source. By default it targets the live COEX origin.
func New(opts ...Option) *Source {
	s := &Source{baseURL: DefaultBaseURL}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ID returns the adapter id used for registry, event_id prefix and source ids.
func (s *Source) ID() string { return "coex" }

// Discover walks the WordPress sitemap: it fetches the index, selects the
// exhibitions POST shards dynamically, fetches each shard, and turns every
// detail <loc> into a Ref{EventID: "coex-<slug>", URL}. Results are deduped by
// EventID preserving first-seen order. A shard that 404s or otherwise fails is
// skipped (secondary fallback) so a single missing shard never aborts the run;
// only an index fetch/parse failure or zero discovered shards is fatal.
func (s *Source) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	idxRes, err := f.Fetch(ctx, s.baseURL+sitemapIndexPath, fetch.Conditional{})
	if err != nil {
		return nil, fmt.Errorf("coex: fetch sitemap index: %w", err)
	}
	if idxRes.StatusCode != 200 {
		return nil, fmt.Errorf("coex: sitemap index status %d", idxRes.StatusCode)
	}

	shardLocs, err := exhibitionShardLocs(idxRes.Body)
	if err != nil {
		return nil, fmt.Errorf("coex: parse sitemap index: %w", err)
	}
	if len(shardLocs) == 0 {
		return nil, fmt.Errorf("coex: no exhibitions shards in sitemap index")
	}

	var refs []sources.Ref
	seen := make(map[string]struct{})

	for _, shardLoc := range shardLocs {
		shardURL, perr := s.rebase(shardLoc)
		if perr != nil {
			// A malformed shard <loc> is skipped, not fatal.
			continue
		}
		res, ferr := f.Fetch(ctx, shardURL, fetch.Conditional{})
		if ferr != nil || res.StatusCode != 200 {
			// 404 / transient error on a single shard is the secondary
			// fallback path: skip and keep going.
			continue
		}
		locs, perr := parseURLSet(res.Body)
		if perr != nil {
			continue
		}
		for _, loc := range locs {
			detailURL, rerr := s.rebase(loc)
			if rerr != nil {
				continue
			}
			slug, serr := slugFromURL(loc)
			if serr != nil || slug == "" {
				continue
			}
			id := "coex-" + slug
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			refs = append(refs, sources.Ref{EventID: id, URL: detailURL})
		}
	}

	return refs, nil
}

// Parse is implemented in parse.go (Task 2.2): it turns one fetched COEX detail
// page into a sources.ParsedEvent.

// --- sitemap parsing (encoding/xml, stdlib) ---

// sitemapIndex models <sitemapindex><sitemap><loc>...</loc></sitemap>...</sitemapindex>.
type sitemapIndex struct {
	XMLName  xml.Name `xml:"sitemapindex"`
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

// urlSet models <urlset><url><loc>...</loc></url>...</urlset>.
type urlSet struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// exhibitionShardLocs parses a sitemap index and returns only the exhibitions
// POST shard <loc> entries (wp-sitemap-posts-exhibitions-N.xml), in index
// order. Taxonomy and other post-type shards are filtered out.
func exhibitionShardLocs(body []byte) ([]string, error) {
	var idx sitemapIndex
	if err := xml.Unmarshal(body, &idx); err != nil {
		return nil, err
	}
	var out []string
	for _, sm := range idx.Sitemaps {
		loc := strings.TrimSpace(sm.Loc)
		if loc == "" {
			continue
		}
		if strings.Contains(loc, exhibitionsShardMarker) {
			out = append(out, loc)
		}
	}
	return out, nil
}

// parseURLSet parses a sitemap shard and returns its detail <loc> entries in
// document order.
func parseURLSet(body []byte) ([]string, error) {
	var set urlSet
	if err := xml.Unmarshal(body, &set); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set.URLs))
	for _, u := range set.URLs {
		loc := strings.TrimSpace(u.Loc)
		if loc != "" {
			out = append(out, loc)
		}
	}
	return out, nil
}

// slugFromURL extracts the durable WordPress slug — the last non-empty path
// segment — from an exhibition detail URL. The slug is kept verbatim (the
// percent-encoded form WordPress emits) so it is byte-stable across runs and
// safe as an event_id component. e.g.
//
//	https://www.coex.co.kr/exhibitions/2002-fira-congres/  -> "2002-fira-congres"
func slugFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	// Use EscapedPath so percent-encoding is preserved exactly as in the sitemap.
	p := strings.Trim(u.EscapedPath(), "/")
	if p == "" {
		return "", fmt.Errorf("empty path in %q", rawURL)
	}
	segs := strings.Split(p, "/")
	return segs[len(segs)-1], nil
}

// rebase rewrites a sitemap <loc> to the configured baseURL origin, preserving
// the path (and query, if any). In production baseURL is the real COEX origin
// so the URL is unchanged; in tests it retargets loopback. The result is always
// http(s) — the shared Fetcher's SSRF guard re-validates the destination.
func (s *Source) rebase(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("non-http(s) loc: %q", rawURL)
	}
	out := s.baseURL + u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out, nil
}
