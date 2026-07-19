package fetch

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStrictPublicCrawlJSONFeedMIMEPolicy(t *testing.T) {
	payload := []byte(`{"version":"https://jsonfeed.org/version/1.1","items":[]}`)
	tests := []struct {
		name        string
		contentType string
		wantError   bool
	}{
		{name: "json feed", contentType: "application/feed+json"},
		{name: "generic json", contentType: "application/json"},
		{name: "binary remains rejected", contentType: "application/octet-stream", wantError: true},
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
				_, _ = w.Write(payload)
			}))
			defer srv.Close()
			budget := NewPublicCrawlBudget()
			f := strictTestFetcher(t, budget, allowHost(t, srv))

			// When
			res, err := f.Fetch(context.Background(), srv.URL+"/feed", Conditional{})

			// Then
			if tt.wantError {
				if !errors.Is(err, ErrUnsupportedDocumentMIME) {
					t.Fatalf("error = %v, want ErrUnsupportedDocumentMIME", err)
				}
				if res != nil {
					t.Fatalf("result = %+v, want nil", res)
				}
			} else {
				if err != nil {
					t.Fatalf("Fetch: %v", err)
				}
				if !bytes.Equal(res.Body, payload) {
					t.Fatalf("body = %q, want JSON Feed payload", res.Body)
				}
			}
			if usage := budget.Usage(); usage.TransportAttempts != 2 {
				t.Fatalf("transport attempts = %d, want robots plus document", usage.TransportAttempts)
			}
		})
	}
}
