package publicdiscovery

import (
	"net"
	"net/netip"
	"net/url"
	"path"
	"strings"
)

var reservedPrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func CanonicalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ErrInvalidURL
	}
	if parsed.User != nil {
		return "", ErrURLUserinfo
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return "", ErrInvalidURL
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	parsed.Host = hostname
	if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if err := cleanURLPath(parsed); err != nil {
		return "", ErrInvalidURL
	}
	return parsed.String(), nil
}

func cleanURLPath(parsed *url.URL) error {
	escaped := parsed.EscapedPath()
	if escaped == "" {
		return nil
	}
	trailingSlash := strings.HasSuffix(escaped, "/")
	cleaned := path.Clean(escaped)
	if trailingSlash && cleaned != "/" {
		cleaned += "/"
	}
	decoded, err := url.PathUnescape(cleaned)
	if err != nil {
		return err
	}
	parsed.Path = decoded
	parsed.RawPath = cleaned
	if parsed.EscapedPath() != cleaned {
		parsed.RawPath = ""
	}
	return nil
}

func candidateURLAllowed(raw string, allowLocal bool) bool {
	canonical, err := CanonicalizeURL(raw)
	if err != nil {
		return false
	}
	parsed, err := url.Parse(canonical)
	if err != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if allowLocal {
		if host == "localhost" {
			return true
		}
		if localIP := net.ParseIP(host); localIP != nil && localIP.IsLoopback() {
			return true
		}
	}
	for _, suffix := range []string{"localhost", "local", "internal", "home", "lan", "test", "invalid"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return false
		}
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return host != ""
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
