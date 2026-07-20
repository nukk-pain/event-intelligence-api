package publicdiscovery

import (
	"encoding/xml"
	"strings"
)

type sitemapIndexXML struct {
	XMLName  xml.Name `xml:"sitemapindex"`
	Sitemaps []struct {
		Location string `xml:"loc"`
	} `xml:"sitemap"`
}

type sitemapURLSetXML struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []struct {
		Location string `xml:"loc"`
	} `xml:"url"`
}

func parseSitemap(body []byte) (sitemapDocument, error) {
	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return sitemapDocument{}, errMalformedDocument
	}
	switch strings.ToLower(root.XMLName.Local) {
	case "sitemapindex":
		var index sitemapIndexXML
		if err := xml.Unmarshal(body, &index); err != nil {
			return sitemapDocument{}, errMalformedDocument
		}
		locations := make([]string, 0, len(index.Sitemaps))
		for _, item := range index.Sitemaps {
			if location := strings.TrimSpace(item.Location); location != "" {
				locations = append(locations, location)
			}
		}
		return sitemapDocument{isIndex: true, locations: locations}, nil
	case "urlset":
		var set sitemapURLSetXML
		if err := xml.Unmarshal(body, &set); err != nil {
			return sitemapDocument{}, errMalformedDocument
		}
		locations := make([]string, 0, len(set.URLs))
		for _, item := range set.URLs {
			if location := strings.TrimSpace(item.Location); location != "" {
				locations = append(locations, location)
			}
		}
		return sitemapDocument{locations: locations}, nil
	default:
		return sitemapDocument{}, errMalformedDocument
	}
}

func parseRobotsSitemaps(body []byte) []string {
	var sitemaps []string
	for _, rawLine := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(rawLine)
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		field, value, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(field), "sitemap") {
			continue
		}
		if value = strings.TrimSpace(value); value != "" {
			sitemaps = append(sitemaps, value)
		}
	}
	return sitemaps
}
