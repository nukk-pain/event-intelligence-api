package cfpurge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPurgeEverything_DisabledIsNoOp asserts that missing credentials make the
// call a silent no-op (so local/serve paths need no Cloudflare token).
func TestPurgeEverything_DisabledIsNoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	for _, tc := range []struct{ zone, token string }{
		{"", ""},
		{"zone", ""},
		{"", "token"},
	} {
		if err := PurgeEverything(context.Background(), tc.zone, tc.token, srv.Client()); err != nil {
			t.Fatalf("disabled purge (zone=%q token-set=%v) returned error: %v", tc.zone, tc.token != "", err)
		}
	}
	if called {
		t.Fatal("disabled purge must not make an HTTP request")
	}
}

// TestPurgeEverything_SendsAuthorizedPurgeRequest verifies the request shape:
// Bearer auth, purge_cache path, and {"purge_everything":true} body.
func TestPurgeEverything_SendsAuthorizedPurgeRequest(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	// Point the client at the test server by overriding transport via a custom
	// client whose requests are redirected: simplest is to call the unexported
	// endpoint through a client that rewrites the host. Instead we exercise the
	// real URL builder by asserting it, then drive the HTTP path against srv.
	client := srv.Client()
	client.Transport = rewriteHost{base: http.DefaultTransport, target: srv.URL}

	err := PurgeEverything(context.Background(), "zone123", "secret-token", client)
	if err != nil {
		t.Fatalf("purge returned error: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want Bearer secret-token", gotAuth)
	}
	if !strings.Contains(gotPath, "/zones/zone123/purge_cache") {
		t.Errorf("path = %q, want .../zones/zone123/purge_cache", gotPath)
	}
	if v, ok := gotBody["purge_everything"].(bool); !ok || !v {
		t.Errorf("body purge_everything = %v, want true", gotBody["purge_everything"])
	}
}

// TestPurgeEverything_Non2xxIsError verifies a Cloudflare error status surfaces
// as an error (the caller logs it; it must not contain the token).
func TestPurgeEverything_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
	}))
	defer srv.Close()

	client := srv.Client()
	client.Transport = rewriteHost{base: http.DefaultTransport, target: srv.URL}

	err := PurgeEverything(context.Background(), "zone123", "secret-token", client)
	if err == nil {
		t.Fatal("expected error on 403, got nil")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Errorf("error leaked the token: %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention status 403, got: %v", err)
	}
}

// rewriteHost redirects requests to the test server while preserving the path,
// so the test exercises the real endpoint builder + request construction without
// hitting api.cloudflare.com.
type rewriteHost struct {
	base   http.RoundTripper
	target string
}

func (rt rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := http.NewRequest(req.Method, rt.target+req.URL.Path, req.Body)
	if err != nil {
		return nil, err
	}
	target.Header = req.Header
	return rt.base.RoundTrip(target)
}
