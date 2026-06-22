package normalize

import "strings"

func validURL(p *string) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	if !isHTTPURL(s) {
		return nil
	}
	return &s
}

func isHTTPURL(s string) bool {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return false
	}
	rest := s[strings.Index(s, "://")+3:]
	return strings.TrimSpace(rest) != ""
}
