package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchOverSizeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Do not set Content-Length so chunked streaming forces LimitReader cap.
		w.WriteHeader(http.StatusOK)
		chunk := strings.Repeat("a", 64*1024)
		for i := 0; i < 200; i++ { // ~12.5MB > 5MB cap
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv), WithMaxBodyBytes(5<<20), WithMaxRetries(0))
	_, err := f.Fetch(context.Background(), srv.URL+"/big", Conditional{})
	if err == nil {
		t.Fatalf("expected over-size error, got nil")
	}
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

func TestFetchRejectsOverContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", "99999999") // ~95MB declared
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("small"))
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv), WithMaxBodyBytes(5<<20), WithMaxRetries(0))
	_, err := f.Fetch(context.Background(), srv.URL+"/declared-big", Conditional{})
	if err == nil {
		t.Fatalf("expected rejection on over-Content-Length, got nil")
	}
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

// EUC-KR encoded body must be converted to UTF-8.
func TestFetchCharsetConversion(t *testing.T) {
	// "한국" in EUC-KR is bytes 0xC7 0xD1 0xB1 0xB9.
	eucKR := []byte{0x3C, 0x68, 0x74, 0x6D, 0x6C, 0x3E, 0xC7, 0xD1, 0xB1, 0xB9, 0x3C, 0x2F, 0x68, 0x74, 0x6D, 0x6C, 0x3E}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=euc-kr")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(eucKR)
	}))
	defer srv.Close()

	f := testFetcher(t, allowHost(t, srv))
	res, err := f.Fetch(context.Background(), srv.URL+"/k", Conditional{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(string(res.Body), "한국") {
		t.Fatalf("EUC-KR not converted to UTF-8: %q", string(res.Body))
	}
}
