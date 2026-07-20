package publicdiscovery

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/smpain/event-intelligence-api/internal/fetch"
)

const maxLinksPerDocument = 96

func parseHTML(body []byte, baseURL string) (string, []parsedLink, error) {
	document, err := fetch.ParseHTML(string(body))
	if err != nil {
		return "", nil, errMalformedDocument
	}
	title := normalizeText(document.Find("title").First().Text())
	links := make([]parsedLink, 0, min(maxLinksPerDocument, MaxCandidates))
	document.Find("link[href]").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		rawURL := strings.TrimSpace(selection.AttrOr("href", ""))
		kind, relation, ok := linkDocumentKind(selection.AttrOr("rel", ""), selection.AttrOr("type", ""))
		if ok {
			links = append(links, parsedLink{rawURL: rawURL, kind: kind, relation: relation})
		}
		return len(links) < maxLinksPerDocument
	})
	document.Find("a[href]").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		links = append(links, parsedLink{
			rawURL: strings.TrimSpace(selection.AttrOr("href", "")),
			title:  normalizeText(selection.Text()), kind: documentAuto, relation: ProtocolHTMLLink,
		})
		return len(links) < maxLinksPerDocument
	})
	return title, resolveParsedLinks(baseURL, links), nil
}

func linkDocumentKind(rawRel, rawType string) (documentKind, Protocol, bool) {
	relations := strings.Fields(strings.ToLower(rawRel))
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(rawType, ";", 2)[0]))
	for _, relation := range relations {
		switch relation {
		case "sitemap":
			return documentSitemap, ProtocolHTMLLink, true
		case "alternate":
			if isFeedMediaType(mediaType) {
				return documentFeed, ProtocolHTMLFeed, true
			}
		}
	}
	return 0, "", false
}

func isFeedMediaType(mediaType string) bool {
	switch mediaType {
	case "application/atom+xml", "application/rss+xml", "application/feed+json", "application/json":
		return true
	default:
		return false
	}
}

func resolveParsedLinks(baseURL string, links []parsedLink) []parsedLink {
	resolved := make([]parsedLink, 0, len(links))
	for _, link := range links {
		resolvedURL, ok := resolveURLReference(baseURL, link.rawURL)
		if !ok {
			continue
		}
		link.resolvedURL = resolvedURL
		resolved = append(resolved, link)
	}
	return resolved
}

func resolveURLReference(baseURL, rawURL string) (string, bool) {
	base, err := url.Parse(baseURL)
	if err != nil || rawURL == "" {
		return "", false
	}
	reference, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	return base.ResolveReference(reference).String(), true
}
