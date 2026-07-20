package publicdiscovery

import "strings"

func parseHTTPLinks(baseURL string, values []string) []parsedLink {
	var links []parsedLink
	for _, value := range values {
		for _, part := range splitLinkHeader(value) {
			link, ok := parseLinkPart(part)
			if ok {
				links = append(links, link)
			}
			if len(links) >= maxLinksPerDocument {
				return resolveParsedLinks(baseURL, links)
			}
		}
	}
	return resolveParsedLinks(baseURL, links)
}

func splitLinkHeader(value string) []string {
	var parts []string
	start := 0
	inQuotes := false
	inTarget := false
	for index, char := range value {
		switch char {
		case '"':
			if !inTarget {
				inQuotes = !inQuotes
			}
		case '<':
			if !inQuotes {
				inTarget = true
			}
		case '>':
			if !inQuotes {
				inTarget = false
			}
		case ',':
			if !inQuotes && !inTarget {
				parts = append(parts, strings.TrimSpace(value[start:index]))
				start = index + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts
}

func parseLinkPart(part string) (parsedLink, bool) {
	left := strings.IndexByte(part, '<')
	right := strings.IndexByte(part, '>')
	if left < 0 || right <= left+1 {
		return parsedLink{}, false
	}
	rawURL := strings.TrimSpace(part[left+1 : right])
	parameters := strings.Split(part[right+1:], ";")
	relation := ""
	mediaType := ""
	for _, parameter := range parameters {
		key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
		if !found {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(key) {
		case "rel":
			relation = value
		case "type":
			mediaType = value
		}
	}
	kind, _, ok := linkDocumentKind(relation, mediaType)
	if ok {
		return parsedLink{rawURL: rawURL, kind: kind, relation: ProtocolHTTPLink}, true
	}
	for _, rel := range strings.Fields(strings.ToLower(relation)) {
		if rel == "next" || rel == "related" || rel == "item" {
			return parsedLink{rawURL: rawURL, kind: documentAuto, relation: ProtocolHTTPLink}, true
		}
	}
	return parsedLink{}, false
}
