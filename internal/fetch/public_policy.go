package fetch

import (
	"mime"
	"strings"
)

const (
	defaultPublicMaxBodyBytes          = 512 << 10
	defaultPublicMaxRetries            = 1
	defaultPublicMaxRedirects          = 2
	defaultPublicMaxTransportAttempts  = 64
	defaultPublicMaxAggregateBodyBytes = 6 << 20
)

var publicDocumentMIMETypes = map[string]struct{}{
	"application/atom+xml":  {},
	"application/gzip":      {},
	"application/rss+xml":   {},
	"application/xhtml+xml": {},
	"application/x-gzip":    {},
	"application/xml":       {},
	"text/html":             {},
	"text/plain":            {},
	"text/xml":              {},
}

func parsePublicDocumentMIME(raw string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", &DocumentMIMEError{}
	}
	mediaType = strings.ToLower(mediaType)
	if _, ok := publicDocumentMIMETypes[mediaType]; !ok {
		return "", &DocumentMIMEError{MediaType: mediaType}
	}
	return mediaType, nil
}

func isCompressedPublicDocumentMIME(mediaType string) bool {
	return mediaType == "application/gzip" || mediaType == "application/x-gzip"
}
