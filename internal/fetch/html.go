package fetch

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ParseHTML parses a UTF-8 HTML body into a goquery document. goquery requires
// UTF-8 input; Fetch already guarantees that for its Result.Body.
func ParseHTML(body string) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(body))
}
