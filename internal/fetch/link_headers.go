package fetch

import (
	"fmt"
	"net/http"
)

const (
	// MaxPublicLinkHeaderValues and MaxPublicLinkHeaderBytes bound the complete
	// final-response Link field-values retained by one strict public fetch.
	MaxPublicLinkHeaderValues           = 16
	MaxPublicLinkHeaderBytes            = 8 << 10
	defaultPublicMaxResponseHeaderBytes = 64 << 10
)

// LinkHeaderValues returns a defensive copy of the bounded Link field-values
// retained for a strict public fetch. Legacy results return nil.
func (r *Result) LinkHeaderValues() []string {
	if r == nil || len(r.linkHeaders) == 0 {
		return nil
	}
	return append([]string(nil), r.linkHeaders...)
}

func boundedPublicLinkHeaders(header http.Header) ([]string, error) {
	values := header.Values("Link")
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > MaxPublicLinkHeaderValues {
		return nil, fmt.Errorf("%w: values %d > %d", ErrPublicLinkHeaderLimitExceeded, len(values), MaxPublicLinkHeaderValues)
	}
	totalBytes := 0
	for _, value := range values {
		if len(value) > MaxPublicLinkHeaderBytes-totalBytes {
			return nil, fmt.Errorf("%w: bytes > %d", ErrPublicLinkHeaderLimitExceeded, MaxPublicLinkHeaderBytes)
		}
		totalBytes += len(value)
	}
	return append([]string(nil), values...), nil
}
