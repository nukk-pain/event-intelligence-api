package benchmark

import (
	"encoding/json"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type schemaEvent struct {
	Type                any            `json:"@type"`
	Name                string         `json:"name"`
	Description         string         `json:"description"`
	URL                 string         `json:"url"`
	StartDate           string         `json:"startDate"`
	EndDate             string         `json:"endDate"`
	EventAttendanceMode string         `json:"eventAttendanceMode"`
	Organizer           schemaOrgName  `json:"organizer"`
	Location            schemaLocation `json:"location"`
}

type schemaOrgName struct {
	Name string `json:"name"`
}

type schemaLocation []schemaPlace

type schemaPlace struct {
	Type    any           `json:"@type"`
	Name    string        `json:"name"`
	Address schemaAddress `json:"address"`
}

type schemaAddress struct {
	Locality string `json:"addressLocality"`
	Country  string `json:"addressCountry"`
}

func (l *schemaLocation) UnmarshalJSON(data []byte) error {
	var places []schemaPlace
	if err := json.Unmarshal(data, &places); err == nil {
		*l = places
		return nil
	}
	var place schemaPlace
	if err := json.Unmarshal(data, &place); err != nil {
		return err
	}
	*l = []schemaPlace{place}
	return nil
}

func firstSchemaEvent(doc *goquery.Document) (schemaEvent, bool) {
	var out schemaEvent
	found := false
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if event, ok := decodeSchemaEvent([]byte(sel.Text())); ok {
			out = event
			found = true
			return false
		}
		return true
	})
	return out, found
}

func decodeSchemaEvent(data []byte) (schemaEvent, bool) {
	var event schemaEvent
	if err := json.Unmarshal(data, &event); err == nil && isEventType(event.Type) {
		return event, true
	}
	var graph struct {
		Graph []json.RawMessage `json:"@graph"`
	}
	if err := json.Unmarshal(data, &graph); err != nil {
		return schemaEvent{}, false
	}
	for _, item := range graph.Graph {
		if err := json.Unmarshal(item, &event); err == nil && isEventType(event.Type) {
			return event, true
		}
	}
	return schemaEvent{}, false
}

func isEventType(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.EqualFold(typed, "Event")
	case []any:
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.EqualFold(s, "Event") {
				return true
			}
		}
	}
	return false
}

func (l schemaLocation) place() (city string, country string) {
	for _, place := range l {
		if place.Address.Locality != "" {
			return place.Address.Locality, place.Address.Country
		}
		if strings.Contains(place.Name, ",") {
			parts := strings.Split(place.Name, ",")
			return strings.TrimSpace(parts[0]), countryFromRegion(strings.TrimSpace(parts[len(parts)-1]))
		}
	}
	return "", ""
}

func countryFromRegion(region string) string {
	switch strings.ToUpper(region) {
	case "CA", "NY", "MA", "NV":
		return "US"
	default:
		return ""
	}
}
