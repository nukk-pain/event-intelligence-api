package fetch

import (
	"crypto/tls"
	"testing"
)

func hasSuite(ids []uint16, want uint16) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// Only hosts explicitly opted in via WithLegacyTLSHosts may offer RSA
// key-exchange suites; every other host keeps the crypto/tls defaults so the
// forward-secrecy posture of the rest of the crawl is unchanged.
func TestTLSConfigForHostScopesLegacySuites(t *testing.T) {
	f, err := NewFetcher(
		WithAllowedHosts("k-ai.or.kr", "www.coex.co.kr"),
		WithLegacyTLSHosts("k-ai.or.kr"),
	)
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}

	legacy := f.tlsConfigForHost("k-ai.or.kr")
	if !hasSuite(legacy.CipherSuites, tls.TLS_RSA_WITH_AES_128_GCM_SHA256) {
		t.Errorf("legacy host config missing RSA-KEX suite")
	}
	if legacy.MinVersion != tls.VersionTLS12 {
		t.Errorf("legacy MinVersion = %x, want TLS1.2", legacy.MinVersion)
	}
	if legacy.ServerName != "k-ai.or.kr" {
		t.Errorf("legacy ServerName = %q", legacy.ServerName)
	}

	normal := f.tlsConfigForHost("www.coex.co.kr")
	if normal.CipherSuites != nil {
		t.Errorf("non-legacy host must keep default (nil) cipher suites, got %d entries", len(normal.CipherSuites))
	}
}

// Host matching is case-insensitive; an unrelated host never inherits the
// legacy suites.
func TestWithLegacyTLSHostsNormalizesCase(t *testing.T) {
	f, err := NewFetcher(WithAllowedHosts("k-ai.or.kr"), WithLegacyTLSHosts("K-AI.or.KR"))
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	if cfg := f.tlsConfigForHost("k-ai.or.kr"); cfg.CipherSuites == nil {
		t.Errorf("case-insensitive legacy host lookup failed")
	}
}
