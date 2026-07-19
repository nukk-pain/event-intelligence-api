package fetch

import (
	"net/http"
	"testing"
)

func newTestCrawlBudget(t *testing.T, attempts, bodyBytes int64) *CrawlBudget {
	t.Helper()
	budget, err := NewCrawlBudget(CrawlBudgetLimits{
		MaxTransportAttempts:  attempts,
		MaxAggregateBodyBytes: bodyBytes,
	})
	if err != nil {
		t.Fatalf("NewCrawlBudget: %v", err)
	}
	return budget
}

func strictTestFetcher(t *testing.T, budget *CrawlBudget, opts ...Option) *Fetcher {
	t.Helper()
	strictOpts := []Option{WithStrictPublicCrawl(budget)}
	strictOpts = append(strictOpts, opts...)
	return testFetcher(t, strictOpts...)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
