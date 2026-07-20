package eventscoutserver

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

var (
	ErrSolarBackendRequired    = errors.New("eventscout server: Solar backend is required")
	ErrInvalidSolarBackend     = errors.New("eventscout server: invalid Solar backend")
	ErrInvalidTrustedProxyCIDR = errors.New("eventscout server: invalid trusted proxy CIDR")
)

type RuntimeConfig struct {
	SolarBackend agent.Backend
	Handler      HandlerOptions
	Server       ServerOptions
}

// LoadRuntimeConfig selects Solar by name and validates all startup-only settings.
func LoadRuntimeConfig() (RuntimeConfig, error) {
	backend, err := selectSolarBackend(agent.LoadBackends())
	if err != nil {
		return RuntimeConfig{}, err
	}
	trusted, err := trustedProxiesFromEnv(os.Getenv("EVENTSCOUT_TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return RuntimeConfig{}, err
	}
	handler := DefaultHandlerOptions()
	handler.TrustedProxies = trusted
	address := strings.TrimSpace(os.Getenv("EVENTSCOUT_HTTP_ADDR"))
	if address == "" {
		address = "127.0.0.1:8081"
	}
	server := DefaultServerOptions(address)
	if err := validateServerOptions(server); err != nil {
		return RuntimeConfig{}, err
	}
	return RuntimeConfig{SolarBackend: backend, Handler: handler, Server: server}, nil
}

func selectSolarBackend(backends []agent.Backend) (agent.Backend, error) {
	for _, backend := range backends {
		if backend.Name != "solar" {
			continue
		}
		if err := validateSolarBackend(backend); err != nil {
			return agent.Backend{}, err
		}
		return backend, nil
	}
	return agent.Backend{}, ErrSolarBackendRequired
}

func validateSolarBackend(backend agent.Backend) error {
	if backend.Name != "solar" || strings.TrimSpace(backend.APIKey) == "" || strings.TrimSpace(backend.Model) == "" {
		return ErrSolarBackendRequired
	}
	baseURL, err := url.Parse(strings.TrimSpace(backend.BaseURL))
	if err != nil || baseURL.Host == "" || baseURL.User != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return ErrInvalidSolarBackend
	}
	return nil
}

func ParseTrustedProxyCIDRs(rawCIDRs []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(rawCIDRs))
	for index, rawCIDR := range rawCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(rawCIDR))
		if err != nil {
			return nil, fmt.Errorf("%w at entry %d", ErrInvalidTrustedProxyCIDR, index+1)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func trustedProxiesFromEnv(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return []netip.Prefix{}, nil
	}
	return ParseTrustedProxyCIDRs(strings.Split(raw, ","))
}
