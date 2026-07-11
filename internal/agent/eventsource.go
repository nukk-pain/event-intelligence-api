package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// categoryAlias maps the agent's coarse category vocabulary to the read API's
// taxonomy so a query for "robotics" hits "humanoid-robotics", etc.
var categoryAlias = map[string]string{
	"robotics": "humanoid-robotics",
	"medical":  "medical-devices",
	"health":   "digital-health",
	"ai":       "ai",
	"bio":      "bio",
}

// QueryEvents queries the live read API (events.nukk.net) with a Filter,
// pushing category and date bounds to the server and refining region/keyword
// client-side. With no date bound it defaults to upcoming events (list=venue)
// so the tool returns useful results rather than the oldest rows. The read API
// is public and read-only, so no auth is needed.
func QueryEvents(ctx context.Context, baseURL string, f Filter, max int, timeout time.Duration) ([]Event, error) {
	q := url.Values{}
	q.Set("limit", "100")
	if cat := strings.ToLower(strings.TrimSpace(f.Category)); cat != "" {
		if alias, ok := categoryAlias[cat]; ok {
			q.Set("category", alias)
		} else {
			q.Set("category", cat)
		}
	}
	if f.FromDate != "" {
		q.Set("since", f.FromDate)
	} else {
		q.Set("list", "venue") // default to upcoming
	}
	if f.ToDate != "" {
		q.Set("before", f.ToDate)
	}

	base := strings.TrimRight(baseURL, "/") + "/api/v1/events"
	var fetched []Event
	cursor := ""
	for len(fetched) < max {
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		page, err := fetchPage(ctx, base+"?"+q.Encode(), timeout)
		if err != nil {
			return fetched, err
		}
		for _, ae := range page.Data {
			fetched = append(fetched, ae.toEvent())
			if len(fetched) >= max {
				break
			}
		}
		if page.Page.NextCursor == "" {
			break
		}
		cursor = page.Page.NextCursor
	}
	// Refine region/keyword (and re-apply category/date) client-side; idempotent.
	return Match(fetched, f), nil
}

type apiResp struct {
	Data []apiEvent `json:"data"`
	Page struct {
		NextCursor string `json:"next_cursor"`
	} `json:"page"`
}

type apiEvent struct {
	EventID    string   `json:"event_id"`
	Name       string   `json:"name"`
	NameEn     *string  `json:"name_en"`
	StartDate  string   `json:"start_date"`
	EndDate    *string  `json:"end_date"`
	Categories []string `json:"categories"`
	Register   *string  `json:"register_url"`
	Homepage   *string  `json:"homepage_url"`
	Venue      *struct {
		Name string `json:"name"`
		City string `json:"city"`
	} `json:"venue"`
	Sources []struct {
		URL string `json:"url"`
	} `json:"sources"`
}

func (a apiEvent) toEvent() Event {
	e := Event{
		ID:          a.EventID,
		Name:        a.Name,
		NameEn:      deref(a.NameEn),
		StartDate:   a.StartDate,
		EndDate:     deref(a.EndDate),
		Category:    strings.Join(a.Categories, ","),
		RegisterURL: deref(a.Register),
	}
	if a.Venue != nil {
		e.Venue = a.Venue.Name
		e.City = a.Venue.City
	}
	if len(a.Sources) > 0 {
		e.SourceURL = a.Sources[0].URL
	} else {
		e.SourceURL = deref(a.Homepage)
	}
	return e
}

func fetchPage(ctx context.Context, u string, timeout time.Duration) (*apiResp, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "eventsintel-agent/0.1 (+https://events.nukk.net)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read API status %d", resp.StatusCode)
	}
	var r apiResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode read API: %w", err)
	}
	return &r, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
