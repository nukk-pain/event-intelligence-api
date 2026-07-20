package agent

import (
	"net"
	"net/url"
	"path"
	"strings"
)

func canonicalCandidateURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || StripContacts(trimmed) != trimmed {
		return "", ErrInvalidCandidateURL
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Opaque != "" || u.Host == "" || u.User != nil {
		return "", ErrInvalidCandidateURL
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", ErrInvalidCandidateURL
	}
	hostname := strings.ToLower(u.Hostname())
	if !publicCandidateHostname(hostname) {
		return "", ErrInvalidCandidateURL
	}
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(hostname, ":") {
		u.Host = "[" + hostname + "]"
	} else {
		u.Host = hostname
	}
	if port != "" {
		u.Host = net.JoinHostPort(hostname, port)
	}
	u.Fragment = ""
	u.RawFragment = ""
	cleanCandidatePath(u)
	return u.String(), nil
}

func publicCandidateHostname(host string) bool {
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func cleanCandidatePath(u *url.URL) {
	if u.Path == "" {
		return
	}
	trailingSlash := strings.HasSuffix(u.Path, "/")
	cleaned := path.Clean(u.Path)
	if trailingSlash && cleaned != "/" {
		cleaned += "/"
	}
	u.Path = cleaned
	u.RawPath = ""
}
