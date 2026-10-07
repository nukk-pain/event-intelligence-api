package clientidentity

import (
	"net/http/httptest"
	"testing"
)

func TestKey(t *testing.T) {
	for _, tc := range []struct {
		name, peer string
		canonical  []string
		want       string
	}{
		{"direct", "198.51.100.7:1234", []string{"203.0.113.9"}, "198.51.100.7"},
		{"cloudflare is not a peer proxy", "173.245.48.1:1234", []string{"203.0.113.9"}, "173.245.48.1"},
		{"loopback v4", "127.0.0.1:1234", []string{"203.0.113.9"}, "203.0.113.9"},
		{"loopback v6", "[::1]:1234", []string{"203.0.113.9"}, "203.0.113.9"},
		{"mapped peer", "[::ffff:127.0.0.1]:1234", []string{"::ffff:203.0.113.9"}, "203.0.113.9"},
		{"missing", "127.0.0.1:1234", nil, "127.0.0.1"},
		{"malformed", "127.0.0.1:1234", []string{"garbage"}, "127.0.0.1"},
		{"list", "127.0.0.1:1234", []string{"203.0.113.9, 203.0.113.10"}, "127.0.0.1"},
		{"duplicate", "127.0.0.1:1234", []string{"203.0.113.9", "203.0.113.9"}, "127.0.0.1"},
		{"port", "127.0.0.1:1234", []string{"203.0.113.9:5000"}, "127.0.0.1"},
		{"zone", "127.0.0.1:1234", []string{"fe80::1%en0"}, "127.0.0.1"},
		{"v6 a", "127.0.0.1:1234", []string{"2001:db8:1:2::1"}, "2001:db8:1:2::/64"},
		{"v6 b same", "127.0.0.1:1234", []string{"2001:db8:1:2::ffff"}, "2001:db8:1:2::/64"},
		{"v6 c different", "[2001:db8:1:3::1]:1234", nil, "2001:db8:1:3::/64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("CF-Connecting-IP", "192.0.2.99")
			r.Header.Set("X-Forwarded-For", "192.0.2.98")
			for _, value := range tc.canonical {
				r.Header.Add("X-Real-Client-IP", value)
			}
			if got := Key(r); got != tc.want {
				t.Fatalf("Key = %q, want %q", got, tc.want)
			}
		})
	}
}
