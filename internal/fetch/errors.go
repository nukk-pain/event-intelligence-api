package fetch

import (
	"errors"
	"fmt"
)

var (
	ErrHostNotAllowed                = errors.New("fetch: host not allowed")
	ErrBlockedAddress                = errors.New("fetch: blocked destination address")
	ErrBadScheme                     = errors.New("fetch: scheme must be http or https")
	ErrInvalidURL                    = errors.New("fetch: invalid URL")
	ErrURLUserinfo                   = errors.New("fetch: URL userinfo is forbidden")
	ErrBodyTooLarge                  = errors.New("fetch: response body too large")
	ErrRobotsDisallowed              = errors.New("fetch: path disallowed by robots.txt")
	ErrRobotsUnavailable             = errors.New("fetch: robots.txt unavailable in strict crawl mode")
	ErrTooManyRedirects              = errors.New("fetch: too many redirects")
	ErrTransportBudgetExhausted      = errors.New("fetch: transport attempt budget exhausted")
	ErrAggregateBodyBudgetExhausted  = errors.New("fetch: aggregate body budget exhausted")
	ErrInvalidCrawlBudget            = errors.New("fetch: invalid crawl budget")
	ErrUnexpectedStatus              = errors.New("fetch: unexpected document status")
	ErrUnsupportedDocumentMIME       = errors.New("fetch: unsupported document MIME type")
	ErrPublicLinkHeaderLimitExceeded = errors.New("fetch: public Link response headers exceed limit")
	errMalformedRobots               = errors.New("fetch: malformed robots.txt")
)

// StatusError carries the upstream status without exposing response content.
type StatusError struct {
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("fetch: status %d", e.StatusCode)
}

func (e *StatusError) Is(target error) bool {
	return target == ErrUnexpectedStatus
}

// DocumentMIMEError carries only a normalized media type. Malformed header
// values are represented by an empty MediaType so hostile raw headers are not
// propagated into logs by callers.
type DocumentMIMEError struct {
	MediaType string
}

func (e *DocumentMIMEError) Error() string {
	if e.MediaType == "" {
		return ErrUnsupportedDocumentMIME.Error()
	}
	return fmt.Sprintf("%s: %s", ErrUnsupportedDocumentMIME, e.MediaType)
}

func (e *DocumentMIMEError) Is(target error) bool {
	return target == ErrUnsupportedDocumentMIME
}

// RobotsUnavailableError is the strict policy result for a robots retrieval
// that cannot safely grant permission. StatusCode is zero for transport or
// parsing failures; Cause remains available through errors.Is/errors.As.
type RobotsUnavailableError struct {
	StatusCode int
	Cause      error
}

func (e *RobotsUnavailableError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s: status %d", ErrRobotsUnavailable, e.StatusCode)
	}
	return ErrRobotsUnavailable.Error()
}

func (e *RobotsUnavailableError) Is(target error) bool {
	return target == ErrRobotsUnavailable
}

func (e *RobotsUnavailableError) Unwrap() error {
	return e.Cause
}

func isPublicCrawlBoundaryError(err error) bool {
	return errors.Is(err, ErrHostNotAllowed) ||
		errors.Is(err, ErrBlockedAddress) ||
		errors.Is(err, ErrBadScheme) ||
		errors.Is(err, ErrInvalidURL) ||
		errors.Is(err, ErrURLUserinfo) ||
		errors.Is(err, ErrBodyTooLarge) ||
		errors.Is(err, ErrRobotsDisallowed) ||
		errors.Is(err, ErrRobotsUnavailable) ||
		errors.Is(err, ErrTooManyRedirects) ||
		errors.Is(err, ErrTransportBudgetExhausted) ||
		errors.Is(err, ErrAggregateBodyBudgetExhausted) ||
		errors.Is(err, ErrPublicLinkHeaderLimitExceeded) ||
		errors.Is(err, ErrUnsupportedDocumentMIME)
}

func legacyClientBoundaryError(err error) error {
	switch {
	case errors.Is(err, ErrBlockedAddress):
		return ErrBlockedAddress
	case errors.Is(err, ErrHostNotAllowed):
		return ErrHostNotAllowed
	case errors.Is(err, ErrBadScheme):
		return ErrBadScheme
	case errors.Is(err, ErrTooManyRedirects):
		return ErrTooManyRedirects
	case errors.Is(err, ErrRobotsDisallowed):
		return ErrRobotsDisallowed
	default:
		return nil
	}
}
