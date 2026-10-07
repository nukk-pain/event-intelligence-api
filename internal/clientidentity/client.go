// Package clientidentity implements the host-process proxy contract for quotas.
package clientidentity

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Key trusts exactly one canonical header only from a loopback socket peer.
// Legacy CF/XFF values never establish identity. IPv4-mapped IPv6 is unwrapped
// before IPv6 addresses are grouped by /64.
func Key(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return "invalid-peer"
	}
	ip = ip.Unmap()
	if ip.IsLoopback() {
		values := r.Header.Values("X-Real-Client-IP")
		if len(values) == 1 {
			forwarded, err := netip.ParseAddr(strings.TrimSpace(values[0]))
			if err == nil && forwarded.Zone() == "" {
				ip = forwarded.Unmap()
			}
		}
	}
	if ip.Is4() {
		return ip.String()
	}
	return netip.PrefixFrom(ip, 64).Masked().String()
}
