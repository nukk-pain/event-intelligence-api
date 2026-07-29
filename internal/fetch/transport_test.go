package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransport_AnyPublicHostUsesHTTP1(t *testing.T) {
	// Organizer sites are arbitrary, often old servers; several terminate
	// HTTP/2 streams with PROTOCOL_ERROR while serving HTTP/1.1 fine
	// (rehahomecare.com, observed 2026-07-29 — the whole site was
	// unreachable to ingest). Any-public-host fetchers must not attempt h2.
	f, err := NewFetcher(WithUserAgent("test"), WithAnyPublicHost(true))
	if err != nil {
		t.Fatal(err)
	}
	tr := underlyingTransport(t, f.client)
	if tr.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 must be false for any-public-host fetchers")
	}
	if tr.TLSNextProto == nil || len(tr.TLSNextProto) != 0 {
		t.Errorf("TLSNextProto = %v, want empty non-nil map (h2 disabled)", tr.TLSNextProto)
	}
}

func TestTransport_AllowlistedFetcherKeepsHTTP2(t *testing.T) {
	f, err := NewFetcher(WithUserAgent("test"), WithAllowedHosts("example.com"))
	if err != nil {
		t.Fatal(err)
	}
	tr := underlyingTransport(t, f.client)
	if !tr.ForceAttemptHTTP2 {
		t.Error("allowlisted venue fetchers keep HTTP/2")
	}
}

func underlyingTransport(t *testing.T, c *http.Client) *http.Transport {
	t.Helper()
	switch v := c.Transport.(type) {
	case *http.Transport:
		return v
	case *budgetRoundTripper:
		tr, ok := v.base.(*http.Transport)
		if !ok {
			t.Fatalf("unexpected base transport %T", v.base)
		}
		return tr
	default:
		t.Fatalf("unexpected transport %T", c.Transport)
		return nil
	}
}

func TestFetch_FollowsMetaRefreshSameHost(t *testing.T) {
	// Older Korean event sites serve a tiny stub whose only content is a
	// meta-refresh to the real page (rehahomecare.com). Without following it,
	// ingest reads 3.6KB of nothing and the event never gets action URLs.
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", http.NotFound)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><meta http-equiv="refresh" content="0;URL=/ko/main/main.asp"></head><body></body></html>`)
	})
	mux.HandleFunc("/ko/main/main.asp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><a href="../visitor/pre_regist.asp">사전등록</a>실제 본문</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f, err := NewFetcher(WithUserAgent("test"), WithAnyPublicHost(true), WithAllowLoopback(true))
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Fetch(context.Background(), srv.URL+"/", Conditional{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Body), "실제 본문") {
		t.Fatalf("body did not follow meta refresh: %q", string(res.Body))
	}
	if !strings.HasSuffix(res.URL, "/ko/main/main.asp") {
		t.Fatalf("Result.URL = %q, want the redirect target so relative links resolve", res.URL)
	}
}

func TestFetch_CrossHostMetaRefreshNotFollowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", http.NotFound)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><meta http-equiv="refresh" content="0;URL=https://evil.example.com/x"></head><body>stub</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f, err := NewFetcher(WithUserAgent("test"), WithAnyPublicHost(true), WithAllowLoopback(true))
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Fetch(context.Background(), srv.URL+"/", Conditional{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Body), "stub") {
		t.Fatalf("cross-host refresh must not be followed: %q", string(res.Body))
	}
}
