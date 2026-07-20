package publicdiscovery

import "errors"

var errMalformedDocument = errors.New("public discovery: malformed protocol document")

type documentKind uint8

const (
	documentSitemap documentKind = iota + 1
	documentFeed
	documentHTML
	documentAuto
)

type parsedLink struct {
	rawURL      string
	resolvedURL string
	title       string
	kind        documentKind
	relation    Protocol
}

type feedEntry struct {
	rawURL string
	title  string
}

type sitemapDocument struct {
	isIndex   bool
	locations []string
}
