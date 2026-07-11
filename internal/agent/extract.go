package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Fields is the extraction target: the event facts an LLM reads out of prose.
// venue_name is the venue only (코엑스, 킨텍스); hall/room goes in hall.
var Fields = []string{
	"name", "name_en", "start_raw", "end_raw",
	"venue_name", "hall", "city", "organizer", "host", "homepage_url",
}

// ExtractPrompt instructs a model to pull structured event facts as JSON.
const ExtractPrompt = `You extract structured event facts from Korean event announcements.
Return ONLY a JSON object with exactly these keys:
name, name_en, start_raw, end_raw, venue_name, hall, city, organizer, host, homepage_url.

Rules:
- Use only facts explicitly present in the text. Never guess or invent.
- If a field is absent, set it to null.
- venue_name is the venue only (e.g. 코엑스, 킨텍스). Put a specific hall/room
  (e.g. 제2전시장, 그랜드볼룸) in hall, not in venue_name.
- organizer is 주최; host is 주관. Do NOT treat 후원 (sponsor) as organizer or host.
- Keep dates as they appear but resolve the full date when the year/month is clear.
- Output the JSON object only, no prose.`

// Facts is one extracted event as a flat key->value map (values are string or nil).
type Facts map[string]any

// Extract runs the extraction step on raw event text. Contact info is stripped
// before the text is sent to the model.
func Extract(ctx context.Context, be Backend, text string, maxTokens int, timeout time.Duration) (Facts, Usage, time.Duration, error) {
	content, u, lat, err := be.Chat(ctx, ExtractPrompt, StripContacts(text), maxTokens, timeout)
	if err != nil {
		return nil, u, lat, err
	}
	js := LastJSONObject(content)
	if js == "" {
		return nil, u, lat, fmt.Errorf("no JSON object in extraction output")
	}
	var f Facts
	if err := json.Unmarshal([]byte(js), &f); err != nil {
		return nil, u, lat, fmt.Errorf("parse extraction json: %w", err)
	}
	return f, u, lat, nil
}
