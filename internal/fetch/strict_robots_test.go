package fetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStrictRobots404PermitsDocument(t *testing.T) {
	// Given
	var documentHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		documentHits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>public</html>"))
	}))
	defer srv.Close()
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, srv))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

	// Then
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.StatusCode != http.StatusOK || documentHits.Load() != 1 {
		t.Fatalf("status=%d document hits=%d, want 200/1", res.StatusCode, documentHits.Load())
	}
}

func TestStrictRobotsFailuresSkipDocument(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{name: "500", status: http.StatusInternalServerError},
		{name: "429", status: http.StatusTooManyRequests},
		{name: "401", status: http.StatusUnauthorized},
		{name: "403", status: http.StatusForbidden},
		{name: "malformed", status: http.StatusOK, contentType: "text/plain", body: "not robots syntax"},
		{name: "invalid utf8", status: http.StatusOK, contentType: "text/plain", body: string([]byte{0xff})},
		{name: "unusable mime", status: http.StatusOK, contentType: "text/html", body: "<html>not robots</html>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			var documentHits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					if tt.contentType != "" {
						w.Header().Set("Content-Type", tt.contentType)
					}
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
					return
				}
				documentHits.Add(1)
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, srv))

			// When
			_, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

			// Then
			if !errors.Is(err, ErrRobotsUnavailable) {
				t.Fatalf("error = %v, want ErrRobotsUnavailable", err)
			}
			if documentHits.Load() != 0 {
				t.Fatalf("document hits = %d, want 0", documentHits.Load())
			}
		})
	}
}

func TestStrictRobotsOversizeSkipsAtBound(t *testing.T) {
	// Given
	var documentHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, maxRobotsBodyBytes+64))
			return
		}
		documentHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, allowHost(t, srv))

	// When
	_, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

	// Then
	if !errors.Is(err, ErrRobotsUnavailable) || !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want oversize robots unavailable", err)
	}
	if usage := budget.Usage(); usage.AggregateBodyBytes > maxRobotsBodyBytes+1 {
		t.Fatalf("robots reader consumed %d bytes, want at most %d", usage.AggregateBodyBytes, maxRobotsBodyBytes+1)
	}
	if documentHits.Load() != 0 {
		t.Fatalf("document hits = %d, want 0", documentHits.Load())
	}
}

func TestStrictRobotsTimeoutSkipsDocument(t *testing.T) {
	// Given
	var documentHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			<-r.Context().Done()
			return
		}
		documentHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, srv), WithTimeout(40*time.Millisecond))

	// When
	_, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

	// Then
	if !errors.Is(err, ErrRobotsUnavailable) {
		t.Fatalf("error = %v, want ErrRobotsUnavailable", err)
	}
	if documentHits.Load() != 0 {
		t.Fatalf("document hits = %d, want 0", documentHits.Load())
	}
}

func TestStrictRobotsDNSFailureSkipsDocument(t *testing.T) {
	// Given
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, WithAnyPublicHost(true))
	f.robotsClient.Transport = &budgetRoundTripper{
		budget: budget,
		base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &net.DNSError{Err: "no such host", Name: "public.invalid", IsNotFound: true}
		}),
	}

	// When
	_, err := f.Fetch(context.Background(), "https://public.invalid/event", Conditional{})

	// Then
	if !errors.Is(err, ErrRobotsUnavailable) {
		t.Fatalf("error = %v, want ErrRobotsUnavailable", err)
	}
	if usage := budget.Usage(); usage.TransportAttempts != 1 {
		t.Fatalf("transport attempts = %d, want 1 DNS attempt", usage.TransportAttempts)
	}
}

func TestStrictRobotsRedirectRejectsUserinfo(t *testing.T) {
	// Given
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	targetWithUserinfo := strings.Replace(target.URL, "http://", "http://user:pass@", 1) + "/robots.txt"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.Redirect(w, r, targetWithUserinfo, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, origin), allowHost(t, target))

	// When
	_, err := f.Fetch(context.Background(), origin.URL+"/event", Conditional{})

	// Then
	if !errors.Is(err, ErrURLUserinfo) {
		t.Fatalf("error = %v, want ErrURLUserinfo", err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("robots userinfo target received %d requests, want 0", targetHits.Load())
	}
}

func TestStrictRedirectRobotsFailureSkipsWithoutDocumentRetry(t *testing.T) {
	// Given
	var targetRobotsHits atomic.Int32
	var targetDocumentHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			targetRobotsHits.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		targetDocumentHits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.Redirect(w, r, target.URL+"/event", http.StatusFound)
	}))
	defer origin.Close()
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, origin), allowHost(t, target))

	// When
	_, err := f.Fetch(context.Background(), origin.URL+"/start", Conditional{})

	// Then
	if !errors.Is(err, ErrRobotsUnavailable) {
		t.Fatalf("error = %v, want ErrRobotsUnavailable", err)
	}
	if targetRobotsHits.Load() != 1 {
		t.Fatalf("target robots hits = %d, want 1 without document retry", targetRobotsHits.Load())
	}
	if targetDocumentHits.Load() != 0 {
		t.Fatalf("target document hits = %d, want 0", targetDocumentHits.Load())
	}
}
