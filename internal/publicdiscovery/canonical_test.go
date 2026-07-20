package publicdiscovery

import (
	"errors"
	"testing"
)

func TestCanonicalizeURL_when_HTTPURL_is_normalized(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{
			name: "scheme host default port fragment and dot segments",
			raw:  "HTTPS://Example.COM:443/a/./b/../c?b=2&a=1#part",
			want: "https://example.com/a/c?b=2&a=1",
		},
		{name: "http default port", raw: "http://EXAMPLE.com:80", want: "http://example.com"},
		{name: "non-default port", raw: "https://EXAMPLE.com:8443/x", want: "https://example.com:8443/x"},
		{name: "trailing slash preserved", raw: "https://example.com/a/b/", want: "https://example.com/a/b/"},
		{name: "root slash preserved", raw: "https://example.com/", want: "https://example.com/"},
		{name: "empty path preserved", raw: "https://example.com", want: "https://example.com"},
		{name: "query order and duplicates preserved", raw: "https://example.com/x?z=3&a=1&a=2", want: "https://example.com/x?z=3&a=1&a=2"},
		{name: "escaped path preserved", raw: "https://example.com/a%2Fb/../c", want: "https://example.com/c"},
		{name: "userinfo rejected", raw: "https://user:pass@example.com/x", wantErr: ErrURLUserinfo},
		{name: "unsupported scheme rejected", raw: "ftp://example.com/x", wantErr: ErrInvalidURL},
		{name: "relative rejected", raw: "/event", wantErr: ErrInvalidURL},
		{name: "malformed rejected", raw: "https://%zz", wantErr: ErrInvalidURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given / When
			got, err := CanonicalizeURL(tt.raw)

			// Then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalizeURL: %v", err)
			}
			if got != tt.want {
				t.Fatalf("canonical = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCanonicalizeURL_when_trailing_slash_or_query_order_differs(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
	}{
		{name: "trailing slash", left: "https://example.com/events", right: "https://example.com/events/"},
		{name: "query order", left: "https://example.com/events?a=1&b=2", right: "https://example.com/events?b=2&a=1"},
		{name: "query multiplicity", left: "https://example.com/events?a=1", right: "https://example.com/events?a=1&a=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given / When
			left, leftErr := CanonicalizeURL(tt.left)
			right, rightErr := CanonicalizeURL(tt.right)

			// Then
			if leftErr != nil || rightErr != nil {
				t.Fatalf("canonical errors = (%v, %v)", leftErr, rightErr)
			}
			if left == right {
				t.Fatalf("canonical URLs unexpectedly equal: %q", left)
			}
		})
	}
}

func TestCandidateURLAllowed_when_literal_host_is_nonpublic(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/event",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/event",
		"http://192.0.2.10/event",
		"http://[::1]/event",
		"http://[fe80::1%25eth0]/event",
		"http://[2001:db8::1]/event",
		"https://localhost/event",
		"https://events.local/event",
		"https://events.internal/event",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			// Given / When
			allowed := candidateURLAllowed(rawURL, false)

			// Then
			if allowed {
				t.Fatalf("candidateURLAllowed(%q) = true, want false", rawURL)
			}
		})
	}
}
