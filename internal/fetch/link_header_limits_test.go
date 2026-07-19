package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStrictPublicCrawlLinkHeaderLimits(t *testing.T) {
	tests := []struct {
		name    string
		addLink func(http.Header)
	}{
		{
			name: "value count",
			addLink: func(header http.Header) {
				for i := 0; i < MaxPublicLinkHeaderValues+1; i++ {
					header.Add("Link", fmt.Sprintf("</page/%d>; rel=next", i))
				}
			},
		},
		{
			name: "aggregate bytes",
			addLink: func(header http.Header) {
				header.Set("Link", "<"+strings.Repeat("a", MaxPublicLinkHeaderBytes)+">; rel=next")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/robots.txt" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				tt.addLink(w.Header())
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("untrusted body"))
			}))
			defer srv.Close()
			budget := NewPublicCrawlBudget()
			f := strictTestFetcher(t, budget, allowHost(t, srv))

			// When
			res, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

			// Then
			if !errors.Is(err, ErrPublicLinkHeaderLimitExceeded) {
				t.Fatalf("error = %v, want ErrPublicLinkHeaderLimitExceeded", err)
			}
			if res != nil {
				t.Fatalf("result = %+v, want nil", res)
			}
			usage := budget.Usage()
			if usage.TransportAttempts != 2 || usage.AggregateBodyBytes != 0 {
				t.Fatalf("budget usage = %+v, want charged attempts and no body read", usage)
			}
		})
	}
}

func TestStrictPublicCrawlLinkHeadersDoNotBypassBodyLimit(t *testing.T) {
	// Given
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Link", `</feed>; rel="alternate"`)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write(make([]byte, defaultPublicMaxBodyBytes+64))
	}))
	defer srv.Close()
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, allowHost(t, srv))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/large", Conditional{})

	// Then
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	usage := budget.Usage()
	if usage.TransportAttempts != 2 || usage.AggregateBodyBytes != defaultPublicMaxBodyBytes+1 {
		t.Fatalf("budget usage = %+v, want body overflow probe charged", usage)
	}
}

func TestStrictPublicCrawlLinkHeadersDoNotBypassAttemptLimit(t *testing.T) {
	// Given
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Link", `</feed>; rel="alternate"`)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("unreachable"))
	}))
	defer srv.Close()
	budget := newTestCrawlBudget(t, 1, 1<<20)
	f := strictTestFetcher(t, budget, allowHost(t, srv), WithMaxRetries(0))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

	// Then
	if !errors.Is(err, ErrTransportBudgetExhausted) {
		t.Fatalf("error = %v, want ErrTransportBudgetExhausted", err)
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	if hits.Load() != 1 || budget.Usage().TransportAttempts != 1 {
		t.Fatalf("server hits = %d usage = %+v, want one robots attempt", hits.Load(), budget.Usage())
	}
}

func TestStrictPublicCrawlBoundsAllResponseHeaders(t *testing.T) {
	// Given
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Set-Cookie", "session="+strings.Repeat("x", defaultPublicMaxResponseHeaderBytes))
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("untrusted body"))
	}))
	defer srv.Close()
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, allowHost(t, srv), WithMaxRetries(0))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

	// Then
	if err == nil {
		t.Fatal("Fetch succeeded with oversized response headers")
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	usage := budget.Usage()
	if usage.TransportAttempts != 2 || usage.AggregateBodyBytes != 0 {
		t.Fatalf("budget usage = %+v, want charged attempt and no body read", usage)
	}
}
