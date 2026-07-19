package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestStrictPublicCrawlLinkHeaderValuesAreIsolated(t *testing.T) {
	// Given
	wantLinks := []string{
		`</feed.atom>; rel="alternate"; type="application/atom+xml"`,
		`</next>; rel="next"`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		for _, value := range wantLinks {
			w.Header().Add("Link", value)
		}
		w.Header().Set("Authorization", "Bearer response-secret")
		w.Header().Set("Proxy-Authorization", "Basic proxy-secret")
		w.Header().Set("Set-Cookie", "session=cookie-secret")
		w.Header().Set("X-Secret", "arbitrary-secret")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	budget := NewPublicCrawlBudget()
	f := strictTestFetcher(t, budget, allowHost(t, srv))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

	// Then
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	values := res.LinkHeaderValues()
	if !slices.Equal(values, wantLinks) {
		t.Fatalf("Link values = %q, want %q", values, wantLinks)
	}
	values[0] = "mutated"
	if !slices.Equal(res.LinkHeaderValues(), wantLinks) {
		t.Fatalf("caller mutation changed Result Link values: %q", res.LinkHeaderValues())
	}
	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal Result: %v", err)
	}
	for _, secret := range [][]byte{[]byte("response-secret"), []byte("proxy-secret"), []byte("cookie-secret"), []byte("arbitrary-secret")} {
		if bytes.Contains(encoded, secret) {
			t.Fatalf("Result exposed sensitive response header in JSON: %q", secret)
		}
	}
	usage := budget.Usage()
	if usage.TransportAttempts != 2 || usage.AggregateBodyBytes != 2 {
		t.Fatalf("budget usage = %+v, want two attempts and two body bytes", usage)
	}
}

func TestLegacyFetchDoesNotExposeOrLimitLinkHeaders(t *testing.T) {
	// Given
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		for i := 0; i < MaxPublicLinkHeaderValues+1; i++ {
			w.Header().Add("Link", `</legacy>; rel="next"`)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("legacy"))
	}))
	defer srv.Close()
	f := testFetcher(t, allowHost(t, srv), WithMaxRetries(0))

	// When
	res, err := f.Fetch(context.Background(), srv.URL+"/event", Conditional{})

	// Then
	if err != nil {
		t.Fatalf("legacy Fetch: %v", err)
	}
	if string(res.Body) != "legacy" {
		t.Fatalf("legacy body = %q, want legacy", res.Body)
	}
	if values := res.LinkHeaderValues(); values != nil {
		t.Fatalf("legacy Link values = %q, want nil", values)
	}
}
