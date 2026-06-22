package fetch

import (
	"bytes"
	"io"

	"golang.org/x/net/html/charset"
)

// toUTF8 converts body to UTF-8 using the declared content-type and any meta
// charset declaration. If the content is already UTF-8 it is returned as-is.
func toUTF8(body []byte, contentType string) ([]byte, error) {
	r, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		// Unknown charset: fall back to the original bytes rather than failing.
		return body, nil
	}
	converted, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return converted, nil
}
