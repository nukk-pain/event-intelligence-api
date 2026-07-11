package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Event is one queryable event record. In production these come from the store
// / read API; the MCP server can also load them from a fixture for offline use.
type Event struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	NameEn      string `json:"name_en,omitempty"`
	StartDate   string `json:"start_date"` // YYYY-MM-DD
	EndDate     string `json:"end_date,omitempty"`
	Venue       string `json:"venue,omitempty"`
	City        string `json:"city,omitempty"`
	Category    string `json:"category,omitempty"` // ai|robotics|bio|medical|health
	RegisterURL string `json:"register_url,omitempty"`
	SourceURL   string `json:"source_url,omitempty"`
}

// Filter is a structured event query. Empty fields are ignored.
type Filter struct {
	Category string `json:"category"`
	Region   string `json:"region"`
	Keyword  string `json:"keyword"`
	FromDate string `json:"from_date"` // YYYY-MM-DD inclusive
	ToDate   string `json:"to_date"`   // YYYY-MM-DD inclusive
}

// Match returns the events passing the filter. Date range compares on StartDate
// lexically (YYYY-MM-DD sorts correctly as a string).
func Match(events []Event, f Filter) []Event {
	cat := strings.ToLower(strings.TrimSpace(f.Category))
	region := strings.ToLower(strings.TrimSpace(f.Region))
	kw := strings.ToLower(strings.TrimSpace(f.Keyword))
	var out []Event
	for _, e := range events {
		if cat != "" && !strings.Contains(strings.ToLower(e.Category), cat) {
			continue
		}
		if region != "" && !strings.Contains(strings.ToLower(e.City+" "+e.Venue), region) {
			continue
		}
		if kw != "" && !strings.Contains(strings.ToLower(e.Name+" "+e.NameEn), kw) {
			continue
		}
		if f.FromDate != "" && e.StartDate != "" && e.StartDate < f.FromDate {
			continue
		}
		if f.ToDate != "" && e.StartDate != "" && e.StartDate > f.ToDate {
			continue
		}
		out = append(out, e)
	}
	return out
}

const parseQueryPrompt = `Convert the user's natural-language question about
Korean/international industry events into a JSON filter with exactly these keys:
category, region, keyword, from_date, to_date.

Rules:
- category is one of: ai, robotics, bio, medical, health — or "" if unspecified.
- region is a city or country keyword (e.g. 서울, 고양, 샌프란시스코) or "".
- keyword is any remaining free-text term to match in the event name, or "".
- from_date / to_date are YYYY-MM-DD or "". Resolve relative dates against the
  reference date given below.
- Use "" for anything unspecified. Output the JSON object only.`

// ParseQuery uses the model to turn a natural-language question into a Filter.
// This is the LLM-powered surface (Solar as backend); the actual data lookup
// (Match) uses no LLM, keeping the read path model-free.
func ParseQuery(ctx context.Context, be Backend, question, referenceDate string, maxTokens int, timeout time.Duration) (Filter, Usage, error) {
	user := fmt.Sprintf("Reference date (today): %s\n\nQuestion: %s", referenceDate, StripContacts(question))
	content, u, _, err := be.Chat(ctx, parseQueryPrompt, user, maxTokens, timeout)
	if err != nil {
		return Filter{}, u, err
	}
	var f Filter
	if js := LastJSONObject(content); js != "" {
		if err := json.Unmarshal([]byte(js), &f); err != nil {
			return Filter{}, u, fmt.Errorf("parse filter json: %w", err)
		}
	}
	return f, u, nil
}
