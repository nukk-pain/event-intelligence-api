package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestStrictPublicCrawlTransportReplayCannotExceedBudget(t *testing.T) {
	// Given
	var hits atomic.Int32
	var documentHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch documentHits.Add(1) {
		case 1:
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("Hijack: %v", err)
				return
			}
			_ = conn.Close()
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()
	budget := newTestCrawlBudget(t, 3, 1<<20)
	f := strictTestFetcher(t, budget, allowHost(t, srv), WithRetryBackoff(0))

	// When
	_, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})
	usage := budget.Usage()
	t.Logf("wire hits=%d document hits=%d usage=%+v error=%v", hits.Load(), documentHits.Load(), usage, err)

	// Then
	if int64(hits.Load()) > usage.MaxTransportAttempts {
		t.Fatalf("server hits = %d exceed hard transport cap %d; charged attempts = %d", hits.Load(), usage.MaxTransportAttempts, usage.TransportAttempts)
	}
	if err == nil {
		t.Fatal("Fetch succeeded after a dropped capped attempt, want transport error")
	}
	if usage.TransportAttempts != 3 {
		t.Fatalf("transport attempts = %d, want 3", usage.TransportAttempts)
	}
}

func TestStrictPublicCrawlCopiedBudgetSharesConcurrentCap(t *testing.T) {
	// Given
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	budget := newTestCrawlBudget(t, 3, 1<<20)
	copiedBudget := *budget
	first := strictTestFetcher(t, budget, allowHost(t, srv))
	second := strictTestFetcher(t, &copiedBudget, allowHost(t, srv))
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, f := range []*Fetcher{first, second} {
		go func() {
			<-start
			_, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})
			errs <- err
		}()
	}

	// When
	close(start)
	budgetErrors := 0
	for range 2 {
		if err := <-errs; errors.Is(err, ErrTransportBudgetExhausted) {
			budgetErrors++
		} else if err != nil {
			t.Fatalf("unexpected Fetch error: %v", err)
		}
	}
	originalUsage := budget.Usage()
	copyUsage := copiedBudget.Usage()
	t.Logf("combined hits=%d budget errors=%d original=%+v copy=%+v", hits.Load(), budgetErrors, originalUsage, copyUsage)

	// Then
	if hits.Load() != 3 {
		t.Fatalf("combined server hits = %d, want copied budgets capped at 3", hits.Load())
	}
	if budgetErrors != 1 {
		t.Fatalf("transport budget errors = %d, want 1", budgetErrors)
	}
	if originalUsage.TransportAttempts != 3 || copyUsage.TransportAttempts != 3 {
		t.Fatalf("usage diverged after copy: original=%+v copy=%+v", originalUsage, copyUsage)
	}
}
