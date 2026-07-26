package publicdiscovery

import (
	"bytes"
	"compress/gzip"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/smpain/event-intelligence-api/internal/fetch"
)

const maxExpandedParserBytes = 512 << 10

type parsedLinkSource struct {
	item      frontierItem
	parentURL string
	fetchedAt time.Time
}

type feedSource struct {
	item      frontierItem
	parentURL string
	protocol  Protocol
}

func (state *crawlState) processProtocolResult(item frontierItem, result *fetch.Result) {
	state.addResultLinks(item, result)
	body, ok := parserBody(result)
	if !ok {
		state.budget.Usage.MalformedDocuments++
		return
	}
	mediaType, _, err := mime.ParseMediaType(result.ContentType)
	if err != nil {
		state.budget.Usage.MalformedDocuments++
		return
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/feed+json" || mediaType == "application/json" {
		entries, err := parseJSONFeed(body)
		if err != nil {
			state.budget.Usage.MalformedDocuments++
			return
		}
		state.addFeedEntries(feedSource{item: item, parentURL: result.URL, protocol: ProtocolJSONFeed}, entries)
		return
	}
	if item.kind == documentSitemap || mediaType == "application/gzip" || mediaType == "application/x-gzip" {
		document, err := parseSitemap(body)
		if err != nil {
			state.budget.Usage.MalformedDocuments++
			return
		}
		state.addSitemapLocations(item, result.URL, document)
		return
	}
	protocol, entries, err := parseXMLFeed(body)
	if err == nil {
		state.addFeedEntries(feedSource{item: item, parentURL: result.URL, protocol: protocol}, entries)
		return
	}
	document, sitemapErr := parseSitemap(body)
	if sitemapErr == nil {
		state.addSitemapLocations(item, result.URL, document)
		return
	}
	state.budget.Usage.MalformedDocuments++
}

func (state *crawlState) processHTMLResult(item frontierItem, result *fetch.Result) {
	mediaType, _, err := mime.ParseMediaType(result.ContentType)
	if err != nil {
		state.budget.Usage.MalformedDocuments++
		state.recordSeedOutcome(item, SeedOutcomeUnsupportedContent)
		return
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		state.recordSeedOutcome(item, SeedOutcomeUnsupportedContent)
		state.processProtocolResult(item, result)
		return
	}
	title, links, err := parseHTML(result.Body, result.URL)
	if err != nil {
		state.budget.Usage.MalformedDocuments++
		state.recordSeedOutcome(item, SeedOutcomeUnsupportedContent)
		return
	}
	parentURL, err := CanonicalizeURL(result.URL)
	if err != nil {
		state.budget.Usage.MalformedDocuments++
		state.recordSeedOutcome(item, SeedOutcomeUnsupportedContent)
		return
	}
	fetchedAt := state.provider.now().UTC()
	if item.relation == ProtocolSeed {
		if title == "" {
			title = item.seed.Name
		}
		stored, rejection := state.addCandidate(candidateDiscovery{
			rawURL: result.URL, title: title, seed: item.seed,
			protocol: ProtocolSeed, depth: item.depth, fetchedAt: fetchedAt,
		})
		if stored {
			state.recordSeedOutcome(item, SeedOutcomeCandidate)
		} else {
			state.recordSeedOutcome(item, rejection)
		}
	}
	state.addParsedLinks(parsedLinkSource{item: item, parentURL: parentURL, fetchedAt: fetchedAt}, links)
	state.addResultLinks(item, result)
}

func (state *crawlState) addResultLinks(item frontierItem, result *fetch.Result) {
	links := parseHTTPLinks(result.URL, result.LinkHeaderValues())
	if len(links) == 0 {
		return
	}
	parentURL, err := CanonicalizeURL(result.URL)
	if err != nil {
		return
	}
	state.addParsedLinks(parsedLinkSource{
		item: item, parentURL: parentURL, fetchedAt: state.provider.now().UTC(),
	}, links)
}

func (state *crawlState) addSitemapLocations(item frontierItem, parentURL string, document sitemapDocument) {
	canonicalParent, err := CanonicalizeURL(parentURL)
	if err != nil {
		return
	}
	fetchedAt := state.provider.now().UTC()
	for _, rawURL := range document.locations {
		if document.isIndex {
			state.enqueue(frontierItem{
				rawURL: rawURL, seed: item.seed, parentURL: canonicalParent,
				relation: ProtocolSitemapIndex, depth: item.depth + 1, kind: documentSitemap,
			})
			continue
		}
		state.addAndQueue(candidateDiscovery{
			rawURL: rawURL, seed: item.seed, parentURL: canonicalParent,
			protocol: ProtocolSitemapURLSet, depth: item.depth + 1, fetchedAt: fetchedAt,
		}, documentAuto)
	}
}

func (state *crawlState) addFeedEntries(source feedSource, entries []feedEntry) {
	canonicalParent, err := CanonicalizeURL(source.parentURL)
	if err != nil {
		return
	}
	fetchedAt := state.provider.now().UTC()
	for _, entry := range entries {
		resolvedURL, ok := resolveURLReference(canonicalParent, entry.rawURL)
		if !ok {
			continue
		}
		state.addAndQueue(candidateDiscovery{
			rawURL: resolvedURL, provenanceRawURL: entry.rawURL,
			title: entry.title, seed: source.item.seed,
			parentURL: canonicalParent, protocol: source.protocol,
			depth: source.item.depth + 1, fetchedAt: fetchedAt,
		}, documentAuto)
	}
}

func (state *crawlState) addParsedLinks(source parsedLinkSource, links []parsedLink) {
	for _, link := range links {
		discovery := candidateDiscovery{
			rawURL: link.resolvedURL, provenanceRawURL: link.rawURL,
			title: link.title, seed: source.item.seed,
			parentURL: source.parentURL, protocol: link.relation,
			depth: source.item.depth + 1, fetchedAt: source.fetchedAt,
		}
		switch link.kind {
		case documentSitemap, documentFeed:
			state.enqueue(frontierItem{
				rawURL: link.resolvedURL, seed: source.item.seed, parentURL: source.parentURL,
				relation: link.relation, depth: source.item.depth + 1, kind: link.kind,
			})
		case documentHTML, documentAuto:
			state.addAndQueue(discovery, link.kind)
		}
	}
}

func parserBody(result *fetch.Result) ([]byte, bool) {
	mediaType, _, err := mime.ParseMediaType(result.ContentType)
	if err != nil || (mediaType != "application/gzip" && mediaType != "application/x-gzip") {
		return result.Body, err == nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(result.Body))
	if err != nil {
		return nil, false
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, maxExpandedParserBytes+1))
	if err != nil || len(body) > maxExpandedParserBytes {
		return nil, false
	}
	return body, true
}
