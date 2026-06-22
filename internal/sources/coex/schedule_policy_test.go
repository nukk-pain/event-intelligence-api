package coex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestDiscover_PrefersCurrentScheduleRefsOverSitemap(t *testing.T) {
	// Given
	sitemapRequests := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/event/full-schedules/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<a href="/exhibitions/current-schedule-event/">current</a>`)
	})
	mux.HandleFunc("/wp-sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		sitemapRequests++
		fmt.Fprint(w, `<?xml version="1.0"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"></sitemapindex>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	source := New(
		WithBaseURL(srv.URL),
		WithClock(func() time.Time { return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC) }),
	)

	// When
	refs, err := source.Discover(context.Background(), testFetcher(t, srv))

	// Then
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("len(refs) = %d, want 1 schedule ref", len(refs))
	}
	if refs[0].EventID != "coex-current-schedule-event" {
		t.Fatalf("EventID = %q, want coex-current-schedule-event", refs[0].EventID)
	}
	if sitemapRequests != 0 {
		t.Fatalf("sitemap requests = %d, want 0", sitemapRequests)
	}
}

func TestDiscoverScheduleRefs_StopsAtPageLimit(t *testing.T) {
	// Given
	requests := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/event/full-schedules/", func(w http.ResponseWriter, r *http.Request) {
		requests++
		page := 1
		if rawPage := r.URL.Query().Get("var_page"); rawPage != "" {
			n, err := strconv.Atoi(rawPage)
			if err != nil {
				t.Fatalf("parse var_page %q: %v", rawPage, err)
			}
			page = n
		}
		next := page + 1
		fmt.Fprintf(w, `<a href="/event/full-schedules/?var_page=%d&search_start_date=2026.06.21&search_end_date=2027.06.21&list_type=LIST">next</a>`, next)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	source := New(
		WithBaseURL(srv.URL),
		WithClock(func() time.Time { return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC) }),
	)

	// When
	_ = source.discoverScheduleRefs(context.Background(), testFetcher(t, srv))

	// Then
	if requests != maxSchedulePages {
		t.Fatalf("requests = %d, want %d", requests, maxSchedulePages)
	}
}

func sitemapOnlyFixtureServer(t *testing.T, indexFixture string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/event/full-schedules/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body></body></html>")
	})
	mux.HandleFunc("/wp-sitemap.xml", serveFixture(t, indexFixture))
	mux.HandleFunc("/wp-sitemap-posts-exhibitions-1.xml", serveFixture(t, "wp-sitemap-posts-exhibitions-1.xml"))
	mux.HandleFunc("/wp-sitemap-posts-exhibitions-2.xml", serveFixture(t, "wp-sitemap-posts-exhibitions-1.xml"))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
