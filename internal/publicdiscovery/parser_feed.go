package publicdiscovery

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"strings"
)

type atomXML struct {
	XMLName xml.Name `xml:"feed"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

type rssXML struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
		} `xml:"item"`
	} `xml:"channel"`
}

func parseXMLFeed(body []byte) (Protocol, []feedEntry, error) {
	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return "", nil, errMalformedDocument
	}
	switch strings.ToLower(root.XMLName.Local) {
	case "feed":
		var feed atomXML
		if err := xml.Unmarshal(body, &feed); err != nil {
			return "", nil, errMalformedDocument
		}
		entries := make([]feedEntry, 0, len(feed.Entries))
		for _, item := range feed.Entries {
			for _, link := range item.Links {
				if link.Href == "" || (link.Rel != "" && !strings.EqualFold(link.Rel, "alternate")) {
					continue
				}
				entries = append(entries, feedEntry{rawURL: strings.TrimSpace(link.Href), title: normalizeText(item.Title)})
				break
			}
		}
		return ProtocolAtom, entries, nil
	case "rss":
		var feed rssXML
		if err := xml.Unmarshal(body, &feed); err != nil {
			return "", nil, errMalformedDocument
		}
		entries := make([]feedEntry, 0, len(feed.Channel.Items))
		for _, item := range feed.Channel.Items {
			if link := strings.TrimSpace(item.Link); link != "" {
				entries = append(entries, feedEntry{rawURL: link, title: normalizeText(item.Title)})
			}
		}
		return ProtocolRSS, entries, nil
	default:
		return "", nil, errMalformedDocument
	}
}

func parseJSONFeed(body []byte) ([]feedEntry, error) {
	var feed struct {
		Version string `json:"version"`
		Items   []struct {
			URL         string `json:"url"`
			ExternalURL string `json:"external_url"`
			Title       string `json:"title"`
		} `json:"items"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&feed); err != nil {
		return nil, errMalformedDocument
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errMalformedDocument
	}
	if feed.Version != "https://jsonfeed.org/version/1" && feed.Version != "https://jsonfeed.org/version/1.1" {
		return nil, errMalformedDocument
	}
	entries := make([]feedEntry, 0, len(feed.Items))
	for _, item := range feed.Items {
		rawURL := strings.TrimSpace(item.URL)
		if rawURL == "" {
			rawURL = strings.TrimSpace(item.ExternalURL)
		}
		if rawURL != "" {
			entries = append(entries, feedEntry{rawURL: rawURL, title: normalizeText(item.Title)})
		}
	}
	return entries, nil
}

func normalizeText(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}
