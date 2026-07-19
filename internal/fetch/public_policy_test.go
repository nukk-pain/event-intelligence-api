package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStrictPublicCrawlDefaults(t *testing.T) {
	// Given
	budget := NewPublicCrawlBudget()

	// When
	f := strictTestFetcher(t, budget)
	usage := budget.Usage()

	// Then
	if f.maxRetries != 1 {
		t.Fatalf("max retries = %d, want 1", f.maxRetries)
	}
	if f.maxRedirects != 2 {
		t.Fatalf("max redirects = %d, want 2", f.maxRedirects)
	}
	if f.maxBodyBytes != 512<<10 {
		t.Fatalf("max body bytes = %d, want %d", f.maxBodyBytes, 512<<10)
	}
	if usage.MaxTransportAttempts != 64 || usage.MaxAggregateBodyBytes != 6<<20 {
		t.Fatalf("public budget limits = %+v, want attempts=64 body=%d", usage, 6<<20)
	}
}

func TestStrictPublicCrawlRequiresCallerOwnedBudget(t *testing.T) {
	// Given / When
	_, err := NewFetcher(WithStrictPublicCrawl(nil))

	// Then
	if !errors.Is(err, ErrInvalidCrawlBudget) {
		t.Fatalf("error = %v, want ErrInvalidCrawlBudget", err)
	}
}

func TestStrictPublicCrawlRejectsInitialUserinfo(t *testing.T) {
	// Given
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, allowHost(t, srv))
	rawURL := strings.Replace(srv.URL, "http://", "http://user:pass@", 1) + "/private"

	// When
	_, err := f.Fetch(context.Background(), rawURL, Conditional{})

	// Then
	if !errors.Is(err, ErrURLUserinfo) {
		t.Fatalf("error = %v, want ErrURLUserinfo", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("userinfo URL caused %d destination requests, want 0", hits.Load())
	}
	if usage := budget.Usage(); usage.TransportAttempts != 0 {
		t.Fatalf("transport attempts = %d, want 0", usage.TransportAttempts)
	}
}

func TestStrictPublicCrawlRejectsRedirectUserinfo(t *testing.T) {
	// Given
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	targetWithUserinfo := strings.Replace(target.URL, "http://", "http://user:pass@", 1) + "/secret"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.Redirect(w, r, targetWithUserinfo, http.StatusFound)
	}))
	defer origin.Close()
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, origin), allowHost(t, target))

	// When
	_, err := f.Fetch(context.Background(), origin.URL+"/start", Conditional{})

	// Then
	if !errors.Is(err, ErrURLUserinfo) {
		t.Fatalf("error = %v, want ErrURLUserinfo", err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("userinfo redirect target received %d requests, want 0", targetHits.Load())
	}
}

func TestStrictPublicCrawlAllowsTwoRedirects(t *testing.T) {
	// Given
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(http.StatusNotFound)
		case "/one":
			http.Redirect(w, r, "/two", http.StatusFound)
		case "/two":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, srv))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/one", Conditional{})

	// Then
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.URL != srv.URL+"/final" {
		t.Fatalf("final URL = %q, want %q", res.URL, srv.URL+"/final")
	}
}

func TestStrictPublicCrawlRejectsThirdRedirect(t *testing.T) {
	// Given
	var finalHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.WriteHeader(http.StatusNotFound)
		case "/one":
			http.Redirect(w, r, "/two", http.StatusFound)
		case "/two":
			http.Redirect(w, r, "/three", http.StatusFound)
		case "/three":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			finalHits.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, srv))

	// When
	_, err := f.Fetch(context.Background(), srv.URL+"/one", Conditional{})

	// Then
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("error = %v, want ErrTooManyRedirects", err)
	}
	if finalHits.Load() != 0 {
		t.Fatalf("final redirect target hits = %d, want 0", finalHits.Load())
	}
}

func TestStrictPublicCrawlContextCancellationStopsHungDocument(t *testing.T) {
	// Given
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		cancel()
		<-r.Context().Done()
	}))
	defer srv.Close()
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, srv))

	// When
	_, err := f.Fetch(ctx, srv.URL+"/hung", Conditional{})

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestStrictPublicCrawlRejectsDocumentStatusAndMIME(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		wantErr     error
	}{
		{name: "404 html", status: http.StatusNotFound, contentType: "text/html", wantErr: ErrUnexpectedStatus},
		{name: "image", status: http.StatusOK, contentType: "image/png", wantErr: ErrUnsupportedDocumentMIME},
		{name: "octet stream", status: http.StatusOK, contentType: "application/octet-stream", wantErr: ErrUnsupportedDocumentMIME},
		{name: "malformed content type", status: http.StatusOK, contentType: "text/html; charset", wantErr: ErrUnsupportedDocumentMIME},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("untrusted payload"))
			}))
			defer srv.Close()
			f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, srv), WithMaxRetries(0))

			// When
			res, err := f.Fetch(context.Background(), srv.URL+"/document", Conditional{})

			// Then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if res != nil {
				t.Fatalf("result = %+v, want nil so payload cannot reach a parser", res)
			}
		})
	}
}

func TestStrictPublicCrawlRejectsMalformedURLWithoutTransport(t *testing.T) {
	// Given
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, WithAnyPublicHost(true))

	// When
	_, err := f.Fetch(context.Background(), "https://%zz", Conditional{})

	// Then
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("error = %v, want ErrInvalidURL", err)
	}
	if usage := budget.Usage(); usage.TransportAttempts != 0 {
		t.Fatalf("transport attempts = %d, want 0", usage.TransportAttempts)
	}
}

func TestLegacyFetchDefaultsRemainPermissive(t *testing.T) {
	// Given
	var documentHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		documentHits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("legacy body"))
	}))
	defer srv.Close()
	f := testFetcher(t, allowHost(t, srv), WithMaxRetries(0))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/missing", Conditional{})

	// Then
	if err != nil {
		t.Fatalf("legacy Fetch: %v", err)
	}
	if res.StatusCode != http.StatusNotFound || string(res.Body) != "legacy body" {
		t.Fatalf("legacy result = status %d body %q", res.StatusCode, string(res.Body))
	}
	if documentHits.Load() != 1 {
		t.Fatalf("legacy document hits = %d, want 1", documentHits.Load())
	}
}
