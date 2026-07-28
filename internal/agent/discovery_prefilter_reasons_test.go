package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The prefilter is the boundary where a crawled candidate is discarded before
// the model ever sees it. A single total made a live zero-source run
// undiagnosable, so each drop must be attributed to exactly one safe category.
func TestDiscoverWithOptions_attributesPrefilterDropsByReason(t *testing.T) {
	tests := []struct {
		name    string
		profile DiscoveryProfileName
		result  SearchResult
		want    PrefilterReasons
	}{
		{
			name:    "missing title",
			profile: DiscoveryProfileEvents,
			result:  SearchResult{URL: "https://events.example/expo-2026"},
			want:    PrefilterReasons{MissingTitle: 1},
		},
		{
			name:    "unparseable URL",
			profile: DiscoveryProfileEvents,
			result:  SearchResult{Title: "Expo", URL: "://not-a-url"},
			want:    PrefilterReasons{InvalidURL: 1},
		},
		{
			name:    "URL outside the profile pattern",
			profile: DiscoveryProfileTraining,
			result:  SearchResult{Title: "Expo", URL: "https://events.example/expo-2026", Date: "2026-09-01"},
			want:    PrefilterReasons{URLPattern: 1},
		},
		{
			name:    "missing required date",
			profile: DiscoveryProfileTraining,
			result:  SearchResult{Title: "Course", URL: "https://events.example/training/ai"},
			want:    PrefilterReasons{MissingDate: 1},
		},
		{
			name:    "missing required location",
			profile: DiscoveryProfileConferences,
			result: SearchResult{
				Title: "Summit", URL: "https://events.example/summit/ai", Date: "2026-09-01",
			},
			want: PrefilterReasons{MissingLocation: 1},
		},
		{
			name:    "past date rejected by profile policy",
			profile: DiscoveryProfileConferences,
			result: SearchResult{
				Title: "Summit", URL: "https://events.example/summit/ai",
				Date: "2020-01-01", Location: "Seoul",
			},
			want: PrefilterReasons{PastDate: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			options := testOptions(t, tt.profile)
			backend, _ := newScriptedBackend(t, searchAction("events"), doneAction())

			// When
			run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
				Backend: backend, Goal: "events",
				Tool: &staticSearch{results: []SearchResult{tt.result}}, Options: options,
			})

			// Then
			if err != nil {
				t.Fatalf("DiscoverWithOptions() error = %v", err)
			}
			if run.YieldTrace.PrefilterReasons != tt.want {
				t.Fatalf("prefilter reasons = %#v, want %#v", run.YieldTrace.PrefilterReasons, tt.want)
			}
			if run.YieldTrace.PrefilterDropped != 1 {
				t.Fatalf("prefilter dropped = %d, want 1", run.YieldTrace.PrefilterDropped)
			}
			if len(run.Sources) != 0 {
				t.Fatalf("sources = %#v, want none offered", run.Sources)
			}
		})
	}
}

// Every drop counted in the total must be attributable, or the breakdown would
// silently under-report the loss boundary it exists to identify.
func TestDiscoverWithOptions_prefilterReasonsSumToTotal(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	accepted := "https://events.example/calendar"
	backend, _ := newScriptedBackend(t, searchAction("events"), acceptAction(accepted), doneAction())

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "events",
		Tool: &staticSearch{results: []SearchResult{
			{Title: "Calendar", URL: accepted},
			{URL: "https://events.example/sitemap-child-1"},
			{URL: "https://events.example/sitemap-child-2"},
			{Title: "Broken", URL: "://not-a-url"},
		}},
		Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	reasons := run.YieldTrace.PrefilterReasons
	if reasons.Total() != run.YieldTrace.PrefilterDropped {
		t.Fatalf("reason total = %d, want prefilter dropped = %d", reasons.Total(), run.YieldTrace.PrefilterDropped)
	}
	want := PrefilterReasons{MissingTitle: 2, InvalidURL: 1}
	if reasons != want {
		t.Fatalf("prefilter reasons = %#v, want %#v", reasons, want)
	}
	if run.YieldTrace.Offered != 1 {
		t.Fatalf("offered = %d, want the one titled candidate", run.YieldTrace.Offered)
	}
}

// The breakdown is a classification aid, not a data export.
func TestDiscoverWithOptions_prefilterReasonsSerializeCountsOnly(t *testing.T) {
	// Given
	options := testOptions(t, DiscoveryProfileEvents)
	backend, _ := newScriptedBackend(t, searchAction("events"), doneAction())

	// When
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend, Goal: "private-goal-marker",
		Tool: &staticSearch{results: []SearchResult{
			{URL: "https://events.example/private-candidate-marker", Snippet: "private-page-marker"},
		}},
		Options: options,
	})

	// Then
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	encoded, err := json.Marshal(run.YieldTrace)
	if err != nil {
		t.Fatalf("marshal yield trace: %v", err)
	}
	for _, leaked := range []string{"private-goal-marker", "private-candidate-marker", "private-page-marker", "events.example"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("yield trace leaked %q: %s", leaked, encoded)
		}
	}
	var decoded struct {
		PrefilterReasons map[string]any `json:"prefilter_reasons"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode yield trace: %v", err)
	}
	wantKeys := map[string]struct{}{
		"invalid_url": {}, "url_pattern": {}, "missing_title": {},
		"missing_location": {}, "missing_date": {}, "past_date": {},
	}
	if len(decoded.PrefilterReasons) != len(wantKeys) {
		t.Fatalf("prefilter_reasons keys = %v, want exactly %d fixed keys", decoded.PrefilterReasons, len(wantKeys))
	}
	for key, value := range decoded.PrefilterReasons {
		if _, ok := wantKeys[key]; !ok {
			t.Fatalf("unexpected prefilter_reasons key %q", key)
		}
		if _, ok := value.(float64); !ok {
			t.Fatalf("prefilter_reasons[%q] = %T, want a count", key, value)
		}
	}
}
