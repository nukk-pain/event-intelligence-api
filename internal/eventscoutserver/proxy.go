package eventscoutserver

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type clientIdentity struct {
	trustedProxies []netip.Prefix
}

func newClientIdentity(trustedProxies []netip.Prefix) clientIdentity {
	return clientIdentity{trustedProxies: append([]netip.Prefix(nil), trustedProxies...)}
}

func (identity clientIdentity) key(request *http.Request) string {
	peer, ok := parseRemoteAddress(request.RemoteAddr)
	if !ok {
		return "remote:" + request.RemoteAddr
	}
	if identity.isTrusted(peer) {
		if forwarded, ok := firstValidForwardedAddress(request.Header.Get("X-Forwarded-For")); ok {
			return forwarded.String()
		}
	}
	return peer.String()
}

func (identity clientIdentity) isTrusted(address netip.Addr) bool {
	for _, prefix := range identity.trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseRemoteAddress(remoteAddress string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	address, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}, false
	}
	return address.WithZone("").Unmap(), true
}

func firstValidForwardedAddress(header string) (netip.Addr, bool) {
	for _, rawAddress := range strings.Split(header, ",") {
		address, err := netip.ParseAddr(strings.TrimSpace(rawAddress))
		if err == nil {
			return address.WithZone("").Unmap(), true
		}
	}
	return netip.Addr{}, false
}
