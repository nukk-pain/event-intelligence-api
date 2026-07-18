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
- venue_name is a named venue/facility only (e.g. 코엑스, 킨텍스). Put a
  specific hall/room (e.g. 제2전시장, 그랜드볼룸) in hall, not in venue_name.
  If the text names only an area/city (e.g. 판교, 서울), venue_name is null
  and the area goes in city.
- city contains only the city/locality name, without a country prefix:
  "미국 샌프란시스코" -> city "샌프란시스코".
- organizer is 주최; host is 주관. Do NOT treat 후원 (sponsor) as organizer or host.
- name keeps any year that belongs to the announced title, wherever it sits:
  "세계 AI 대회 (World AI Congress) 2027" -> name "세계 AI 대회 2027".
- name_en is the English name exactly as written (usually in parentheses).
  Never add a year or other words to it: "2026 국제로봇산업대전 (Korea Robot
  Expo)" -> name_en "Korea Robot Expo", not "2026 Korea Robot Expo".
- Copy each date in the exact style it appears in the text — never reformat:
  "2026년 12월 3일" stays "2026년 12월 3일", not "2026.12.03".
- If a date range states the year/month only once, complete the other side in
  the same style: "2026.03.11(수) ~ 03.13(금)" -> end_raw "2026.03.13";
  "2026년 12월 3일부터 4일까지" -> end_raw "2026년 12월 4일".
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
