package api

import (
	"encoding/base64"
	"errors"
	"strings"
)

// ErrInvalidCursor is returned by DecodeCursor when the supplied cursor string
// is not a well-formed opaque cursor produced by EncodeCursor. Handlers map it
// to a bad_request / invalid_cursor error envelope.
var ErrInvalidCursor = errors.New("invalid cursor")

// Cursor is the keyset pagination position. Listing is ordered by
// (updated_at, event_id) ascending, so a cursor records the LAST row already
// returned; the next page selects rows strictly greater than it. event_id is a
// primary key (unique), so the (updated_at, event_id) pair is a total order and
// the cursor is stable across inserts: a row inserted with an earlier
// updated_at than the cursor position is simply never revisited, and a row with
// a later position is picked up naturally — no gaps or duplicates.
type Cursor struct {
	UpdatedAt string
	EventID   string
}

// cursorSep separates the two fields inside the pre-base64 payload. updated_at
// is an ISO8601 timestamp and event_id is a slug; neither contains a newline,
// so '\n' is an unambiguous separator.
const cursorSep = "\n"

// EncodeCursor renders a Cursor as an opaque URL-safe base64 token. The token
// is intentionally opaque: callers must treat it as a opaque continuation
// handle and pass it back verbatim via ?cursor=.
func EncodeCursor(c Cursor) string {
	payload := c.UpdatedAt + cursorSep + c.EventID
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodeCursor parses a token produced by EncodeCursor. An empty string decodes
// to the zero Cursor with no error (callers treat the zero cursor as "from the
// beginning"). Any malformed token returns ErrInvalidCursor.
func DecodeCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	parts := strings.SplitN(string(raw), cursorSep, 2)
	if len(parts) != 2 {
		return Cursor{}, ErrInvalidCursor
	}
	if parts[0] == "" || parts[1] == "" {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{UpdatedAt: parts[0], EventID: parts[1]}, nil
}
