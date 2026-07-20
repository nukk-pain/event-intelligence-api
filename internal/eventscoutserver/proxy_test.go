package eventscoutserver

import (
	"net/http"
	"testing"
)

func TestHandler_ignores_forwarded_for_from_untrusted_peer(t *testing.T) {
	// Given
	settings := defaultTestHandlerSettings()
	settings.trustedProxyCIDRs = []string{"10.0.0.0/8"}
	handler, _ := newTestHandler(t, &stubRunner{}, settings)
	for _, forwarded := range []string{"198.51.100.1", "198.51.100.2"} {
		request := testJSONRequest{
			remoteAddr: "203.0.113.10:5000", forwardedFor: forwarded, body: `{"goal":"robotics"}`,
		}
		if recorder := serveJSON(handler, request); recorder.Code != http.StatusOK {
			t.Fatalf("setup status = %d", recorder.Code)
		}
	}

	// When
	recorder := serveJSON(handler, testJSONRequest{
		remoteAddr: "203.0.113.10:5001", forwardedFor: "198.51.100.3", body: `{"goal":"robotics"}`,
	})

	// Then
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 because spoofed XFF is ignored", recorder.Code)
	}
}

func TestHandler_uses_leftmost_valid_forwarded_for_from_trusted_peer(t *testing.T) {
	// Given
	settings := defaultTestHandlerSettings()
	settings.trustedProxyCIDRs = []string{"10.0.0.0/8"}
	handler, _ := newTestHandler(t, &stubRunner{}, settings)
	for _, forwarded := range []string{
		"invalid, 198.51.100.1, 203.0.113.1",
		"198.51.100.2, 203.0.113.2",
		"198.51.100.3, 203.0.113.3",
	} {
		recorder := serveJSON(handler, testJSONRequest{
			remoteAddr: "10.2.3.4:5000", forwardedFor: forwarded, body: `{"goal":"robotics"}`,
		})
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for distinct leftmost valid clients", recorder.Code)
		}
	}

	// When
	first := serveJSON(handler, testJSONRequest{
		remoteAddr: "10.2.3.4:5000", forwardedFor: "198.51.100.99, 203.0.113.10", body: `{"goal":"robotics"}`,
	})
	second := serveJSON(handler, testJSONRequest{
		remoteAddr: "10.2.3.4:5000", forwardedFor: "198.51.100.99, 203.0.113.11", body: `{"goal":"robotics"}`,
	})
	third := serveJSON(handler, testJSONRequest{
		remoteAddr: "10.2.3.4:5000", forwardedFor: "198.51.100.99, 203.0.113.12", body: `{"goal":"robotics"}`,
	})

	// Then
	if first.Code != http.StatusOK || second.Code != http.StatusOK || third.Code != http.StatusTooManyRequests {
		t.Fatalf("statuses = %d, %d, %d; want 200, 200, 429", first.Code, second.Code, third.Code)
	}
}
