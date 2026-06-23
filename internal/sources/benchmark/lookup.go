package benchmark

import "github.com/smpain/event-intelligence-api/internal/sources"

func refs() []sources.Ref {
	out := make([]sources.Ref, 0, len(catalog))
	for _, event := range catalog {
		out = append(out, sources.Ref{EventID: event.EventID, URL: event.URL})
	}
	return out
}

func lookup(rawURL string) (catalogEvent, bool) {
	for _, event := range catalog {
		if event.URL == rawURL {
			return event, true
		}
	}
	return catalogEvent{}, false
}
