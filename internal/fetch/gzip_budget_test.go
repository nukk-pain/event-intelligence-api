package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStrictPublicCrawlPreservesGzipDocumentBytes(t *testing.T) {
	// Given
	gzipBody := gzipFixture(t, 128)
	srv := gzipDocumentServer(t, gzipBody)
	f := strictTestFetcher(t, NewPublicCrawlBudget(), allowHost(t, srv))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/sitemap.xml.gz", Conditional{})

	// Then
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bytes.Equal(res.Body, gzipBody) {
		t.Fatalf("gzip body changed: got %d bytes, want %d", len(res.Body), len(gzipBody))
	}
}

func TestStrictPublicCrawlGzipExpansionConsumesAggregateBudget(t *testing.T) {
	// Given
	gzipBody := gzipFixture(t, 2048)
	srv := gzipDocumentServer(t, gzipBody)
	budget := newTestCrawlBudget(t, 10, 256)
	f := strictTestFetcher(t, budget, allowHost(t, srv))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/sitemap.xml.gz", Conditional{})
	usage := budget.Usage()
	t.Logf("compressed bytes=%d usage=%+v error=%v", len(gzipBody), usage, err)

	// Then
	if !errors.Is(err, ErrAggregateBodyBudgetExhausted) {
		t.Fatalf("error = %v, want ErrAggregateBodyBudgetExhausted", err)
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	if usage.AggregateBodyBytes != 256 {
		t.Fatalf("aggregate body bytes = %d, want hard cap 256", usage.AggregateBodyBytes)
	}
}

func TestStrictPublicCrawlGzipExpansionHonorsDocumentLimit(t *testing.T) {
	// Given
	srv := gzipDocumentServer(t, gzipFixture(t, defaultPublicMaxBodyBytes+1))
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, allowHost(t, srv))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/sitemap.xml.gz", Conditional{})
	usage := budget.Usage()
	t.Logf("usage=%+v error=%v", usage, err)

	// Then
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	if usage.AggregateBodyBytes <= defaultPublicMaxBodyBytes {
		t.Fatalf("aggregate body bytes = %d, want compressed input plus bounded expansion probe", usage.AggregateBodyBytes)
	}
}

func gzipFixture(t *testing.T, expandedBytes int) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte("x"), expandedBytes)); err != nil {
		t.Fatalf("write gzip fixture: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close gzip fixture: %v", err)
	}
	return compressed.Bytes()
}

func gzipDocumentServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}
