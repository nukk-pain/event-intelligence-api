package eventscoutserver

import (
	"errors"
	"testing"
)

func TestLoadRuntimeConfig_fails_without_solar_even_when_local_is_configured(t *testing.T) {
	// Given
	t.Setenv("EVENTSINTEL_LOCAL_BASE_URL", "http://127.0.0.1:18900/v1")
	t.Setenv("EVENTSINTEL_SOLAR_API_KEY", "")

	// When
	_, err := LoadRuntimeConfig()

	// Then
	if !errors.Is(err, ErrSolarBackendRequired) {
		t.Fatalf("error = %v, want ErrSolarBackendRequired", err)
	}
}

func TestLoadRuntimeConfig_selects_solar_explicitly_when_local_also_exists(t *testing.T) {
	// Given
	t.Setenv("EVENTSINTEL_LOCAL_BASE_URL", "http://127.0.0.1:18900/v1")
	t.Setenv("EVENTSINTEL_SOLAR_API_KEY", "test-key")
	t.Setenv("EVENTSINTEL_SOLAR_BASE_URL", "https://solar.example/v1")
	t.Setenv("EVENTSINTEL_SOLAR_MODEL", "solar-open2")
	t.Setenv("EVENTSCOUT_HTTP_ADDR", "127.0.0.1:18081")
	t.Setenv("EVENTSCOUT_TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 2001:db8::/32")

	// When
	config, err := LoadRuntimeConfig()

	// Then
	if err != nil {
		t.Fatalf("LoadRuntimeConfig() error = %v", err)
	}
	if config.SolarBackend.Name != "solar" || config.SolarBackend.Model != "solar-open2" {
		t.Fatalf("selected backend = %#v", config.SolarBackend)
	}
	if config.Server.Address != "127.0.0.1:18081" || len(config.Handler.TrustedProxies) != 2 {
		t.Fatalf("runtime config = %#v", config)
	}
}

func TestLoadRuntimeConfig_rejects_malformed_trusted_proxy_CIDR(t *testing.T) {
	// Given
	t.Setenv("EVENTSINTEL_SOLAR_API_KEY", "test-key")
	t.Setenv("EVENTSINTEL_SOLAR_BASE_URL", "https://solar.example/v1")
	t.Setenv("EVENTSINTEL_SOLAR_MODEL", "solar-open2")
	t.Setenv("EVENTSCOUT_TRUSTED_PROXY_CIDRS", "not-a-cidr")

	// When
	_, err := LoadRuntimeConfig()

	// Then
	if !errors.Is(err, ErrInvalidTrustedProxyCIDR) {
		t.Fatalf("error = %v, want ErrInvalidTrustedProxyCIDR", err)
	}
}
