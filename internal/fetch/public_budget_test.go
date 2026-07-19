package fetch

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestStrictPublicCrawlBudgetCountsRobotsRedirectsAndRetries(t *testing.T) {
	// Given
	var hits atomic.Int32
	var endpointMu sync.Mutex
	endpointHits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		endpointMu.Lock()
		endpointHits[r.URL.Path]++
		endpointMu.Unlock()
		switch r.URL.Path {
		case "/robots.txt":
			http.Redirect(w, r, "/robots-final.txt", http.StatusFound)
		case "/robots-final.txt":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("User-agent: *\n"))
		case "/start":
			http.Redirect(w, r, "/retry", http.StatusFound)
		case "/retry":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	budget := newTestCrawlBudget(t, 5, 1<<20)
	f := strictTestFetcher(t, budget, allowHost(t, srv), WithRetryBackoff(0))

	// When
	_, err := f.Fetch(context.Background(), srv.URL+"/start", Conditional{})

	// Then
	if !errors.Is(err, ErrTransportBudgetExhausted) {
		t.Fatalf("error = %v, want ErrTransportBudgetExhausted", err)
	}
	usage := budget.Usage()
	if usage.TransportAttempts != 5 {
		t.Fatalf("transport attempts = %d, want 5", usage.TransportAttempts)
	}
	if hits.Load() != 5 {
		t.Fatalf("server hits = %d, want exactly 5", hits.Load())
	}
	endpointMu.Lock()
	gotEndpointHits := maps.Clone(endpointHits)
	endpointMu.Unlock()
	wantEndpointHits := map[string]int{
		"/robots.txt":       1,
		"/robots-final.txt": 1,
		"/start":            2,
		"/retry":            1,
	}
	t.Logf("endpoint attempts=%v budget usage=%+v", gotEndpointHits, usage)
	if !maps.Equal(gotEndpointHits, wantEndpointHits) {
		t.Fatalf("endpoint attempts = %v, want %v", gotEndpointHits, wantEndpointHits)
	}
}

func TestStrictPublicCrawlAggregateBodyBudget(t *testing.T) {
	// Given
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("1234"))
	}))
	defer srv.Close()
	budget := newTestCrawlBudget(t, 10, 6)
	f := strictTestFetcher(t, budget, allowHost(t, srv))
	if _, err := f.Fetch(context.Background(), srv.URL+"/one", Conditional{}); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	// When
	_, err := f.Fetch(context.Background(), srv.URL+"/two", Conditional{})

	// Then
	if !errors.Is(err, ErrAggregateBodyBudgetExhausted) {
		t.Fatalf("error = %v, want ErrAggregateBodyBudgetExhausted", err)
	}
	usage := budget.Usage()
	if usage.AggregateBodyBytes != 6 {
		t.Fatalf("aggregate body bytes = %d, want 6", usage.AggregateBodyBytes)
	}
	t.Logf("aggregate budget usage=%+v", usage)
}

func TestStrictPublicCrawlBudgetsHaveNoSharedStaleState(t *testing.T) {
	// Given
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	first := newTestCrawlBudget(t, 2, 8)
	firstFetcher := strictTestFetcher(t, first, allowHost(t, srv))
	if _, err := firstFetcher.Fetch(context.Background(), srv.URL+"/first", Conditional{}); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	second := newTestCrawlBudget(t, 2, 8)
	secondFetcher := strictTestFetcher(t, second, allowHost(t, srv))

	// When
	_, err := secondFetcher.Fetch(context.Background(), srv.URL+"/second", Conditional{})
	secondUsage := second.Usage()

	// Then
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if secondUsage.TransportAttempts != 2 || secondUsage.AggregateBodyBytes != 2 {
		t.Fatalf("fresh budget usage = %+v, want attempts=2 body=2", secondUsage)
	}
}

func TestStrictPublicCrawlAggregateBodyBudgetIncludesRobots(t *testing.T) {
	// Given
	var documentHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private\n"))
			return
		}
		documentHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	budget := newTestCrawlBudget(t, 4, 8)
	f := strictTestFetcher(t, budget, allowHost(t, srv))

	// When
	_, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

	// Then
	if !errors.Is(err, ErrRobotsUnavailable) || !errors.Is(err, ErrAggregateBodyBudgetExhausted) {
		t.Fatalf("error = %v, want robots unavailable caused by aggregate budget", err)
	}
	if usage := budget.Usage(); usage.AggregateBodyBytes != 8 {
		t.Fatalf("aggregate body bytes = %d, want 8", usage.AggregateBodyBytes)
	}
	if documentHits.Load() != 0 {
		t.Fatalf("document hits = %d, want 0", documentHits.Load())
	}
}

func TestStrictPublicCrawlBudgetSharedAcrossFetchers(t *testing.T) {
	// Given
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	budget := newTestCrawlBudget(t, 3, 32)
	first := strictTestFetcher(t, budget, allowHost(t, srv))
	second := strictTestFetcher(t, budget, allowHost(t, srv))
	if _, err := first.Fetch(context.Background(), srv.URL+"/first", Conditional{}); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	// When
	_, err := second.Fetch(context.Background(), srv.URL+"/second", Conditional{})

	// Then
	if !errors.Is(err, ErrTransportBudgetExhausted) {
		t.Fatalf("error = %v, want ErrTransportBudgetExhausted", err)
	}
	if usage := budget.Usage(); usage.TransportAttempts != 3 {
		t.Fatalf("transport attempts = %d, want 3", usage.TransportAttempts)
	}
	if hits.Load() != 3 {
		t.Fatalf("server hits = %d, want 3", hits.Load())
	}
}

func TestStrictPublicCrawlDocumentBodyLimit(t *testing.T) {
	// Given
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write(make([]byte, defaultPublicMaxBodyBytes+64))
	}))
	defer srv.Close()
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, allowHost(t, srv))

	// When
	_, err := f.Fetch(context.Background(), srv.URL+"/large", Conditional{})

	// Then
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
	if usage := budget.Usage(); usage.AggregateBodyBytes != defaultPublicMaxBodyBytes+1 {
		t.Fatalf("body reader consumed %d bytes, want bounded overflow probe %d", usage.AggregateBodyBytes, defaultPublicMaxBodyBytes+1)
	}
}

func TestStrictPublicCrawlConcurrentBudgetNeverExceedsCap(t *testing.T) {
	// Given
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	budget := newTestCrawlBudget(t, 5, 64)
	f := strictTestFetcher(t, budget, allowHost(t, srv))
	start := make(chan struct{})
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			<-start
			_, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})
			errs <- err
		}()
	}

	// When
	close(start)
	budgetErrors := 0
	unexpectedErrors := 0
	for range 8 {
		err := <-errs
		if errors.Is(err, ErrTransportBudgetExhausted) {
			budgetErrors++
		} else if err != nil {
			unexpectedErrors++
		}
	}

	// Then
	if usage := budget.Usage(); usage.TransportAttempts != 5 {
		t.Fatalf("transport attempts = %d, want capped at 5", usage.TransportAttempts)
	}
	if hits.Load() != 5 {
		t.Fatalf("server hits = %d, want capped at 5", hits.Load())
	}
	if budgetErrors == 0 {
		t.Fatal("expected at least one caller-observable transport budget error")
	}
	if unexpectedErrors != 0 {
		t.Fatalf("unexpected concurrent errors = %d, want 0", unexpectedErrors)
	}
}
