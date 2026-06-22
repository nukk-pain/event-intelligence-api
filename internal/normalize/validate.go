package normalize

import (
	"fmt"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/classify"
	"github.com/smpain/event-intelligence-api/internal/model"
)

func Validate(e *model.Event) error {
	if e == nil {
		return fmt.Errorf("nil event")
	}
	if strings.TrimSpace(e.EventID) == "" {
		return fmt.Errorf("rule1: event_id required")
	}
	if e.SchemaVersion == "" {
		return fmt.Errorf("rule1: schema_version required")
	}
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("rule1: name required")
	}
	if strings.TrimSpace(e.Country) == "" {
		return fmt.Errorf("rule1: country required")
	}
	if e.Confidence == "" {
		return fmt.Errorf("rule1: confidence required")
	}
	if e.MissingFields == nil {
		return fmt.Errorf("rule1: missing_fields required (use empty slice, not nil)")
	}
	if e.LastCheckedAt == "" {
		return fmt.Errorf("rule1: last_checked_at required")
	}
	if !e.Excluded && len(e.Categories) == 0 {
		return fmt.Errorf("rule1: categories required (>=1) for non-excluded event")
	}
	for _, c := range e.Categories {
		if !classify.IsCategory(c) {
			return fmt.Errorf("rule4: category %q not in taxonomy", c)
		}
	}
	if e.StartDate != nil && e.EndDate != nil && *e.EndDate < *e.StartDate {
		return fmt.Errorf("rule3: end_date %q before start_date %q", *e.EndDate, *e.StartDate)
	}
	if len(e.Sources) == 0 {
		return fmt.Errorf("rule5: at least one source required")
	}
	for i, s := range e.Sources {
		if !isHTTPURL(s.URL) {
			return fmt.Errorf("rule5: source[%d] url %q not http(s)", i, s.URL)
		}
		if _, ok := validSourceTypes[s.Type]; !ok {
			return fmt.Errorf("rule5: source[%d] type %q not in enum", i, s.Type)
		}
		if strings.TrimSpace(s.Publisher) == "" {
			return fmt.Errorf("rule5: source[%d] publisher required", i)
		}
		if strings.TrimSpace(s.RetrievedAt) == "" {
			return fmt.Errorf("rule5: source[%d] retrieved_at required", i)
		}
	}
	return nil
}
