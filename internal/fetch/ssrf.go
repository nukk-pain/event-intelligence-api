package fetch

import "net"

// isBlockedIP reports whether ip is a non-public address that the egress guard
// must reject: loopback, link-local (v4 169.254/16 and v6 fe80::/10),
// unspecified (0.0.0.0/::), RFC1918 private v4, and ULA v6 (fc00::/7).
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsMulticast() {
		return true
	}
	return false
}

// isMetadataOrLinkLocal flags link-local / cloud-metadata addresses that must be
// rejected even when loopback is otherwise permitted (test bypass).
func isMetadataOrLinkLocal(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// 169.254.169.254 is covered by IsLinkLocalUnicast, but assert explicitly.
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}
