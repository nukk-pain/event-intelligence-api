package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNamedDiscoveryProfiles_define_templates_requirements_patterns_and_hints(t *testing.T) {
	tests := []struct {
		name            DiscoveryProfileName
		requireDate     bool
		requireLocation bool
		allowPast       bool
	}{
		{name: DiscoveryProfileEvents},
		{name: DiscoveryProfileTraining, requireDate: true},
		{name: DiscoveryProfileConferences, requireDate: true, requireLocation: true},
		{name: DiscoveryProfileAnnouncements, allowPast: true},
	}
	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			profile, err := NamedDiscoveryProfile(tt.name)

			// Then
			if err != nil {
				t.Fatalf("NamedDiscoveryProfile() error = %v", err)
			}
			if profile.Name != tt.name {
				t.Fatalf("profile name = %q, want %q", profile.Name, tt.name)
			}
			if len(profile.QueryTemplates) == 0 || len(profile.AllowedURLPatterns) == 0 || len(profile.AllowedDomainPatterns) == 0 {
				t.Fatalf("profile lacks templates or URL/domain patterns: %#v", profile)
			}
			if len(profile.LocaleHints) == 0 || len(profile.LanguageHints) == 0 {
				t.Fatalf("profile lacks locale/language hints: %#v", profile)
			}
			if !profile.Requirements.Title || !profile.Requirements.Source {
				t.Fatalf("profile title/source requirements = %#v", profile.Requirements)
			}
			if profile.Requirements.Date != tt.requireDate || profile.Requirements.Location != tt.requireLocation {
				t.Fatalf("profile requirements = %#v", profile.Requirements)
			}
			if got := profile.PastDatePolicy == PastDatesAllowed; got != tt.allowPast {
				t.Fatalf("past dates allowed = %v, want %v", got, tt.allowPast)
			}
		})
	}
}

func TestDiscoverWithOptions_applies_selected_profile_required_fields(t *testing.T) {
	tests := []struct {
		name      string
		profile   DiscoveryProfileName
		result    SearchResult
		wantCount int
	}{
		{
			name: "events retain legacy undated candidate behavior", profile: DiscoveryProfileEvents,
			result: SearchResult{Title: "AI Expo", URL: "https://events.example/expo"}, wantCount: 1,
		},
		{
			name: "events require title", profile: DiscoveryProfileEvents,
			result: SearchResult{URL: "https://events.example/expo"}, wantCount: 0,
		},
		{
			name: "training requires date", profile: DiscoveryProfileTraining,
			result: SearchResult{Title: "AI Workshop", URL: "https://learn.example/training/ai"}, wantCount: 0,
		},
		{
			name: "training requires parseable date", profile: DiscoveryProfileTraining,
			result: SearchResult{Title: "AI Workshop", URL: "https://learn.example/training/ai", Date: "soon"}, wantCount: 0,
		},
		{
			name: "conference requires location", profile: DiscoveryProfileConferences,
			result: SearchResult{Title: "AI Summit", URL: "https://conf.example/conference/ai", Date: "2027-04-05"}, wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			options := testOptions(t, tt.profile)
			options.ReferenceTime = time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
			options.MaxTurns = 2
			backend, _ := newScriptedBackend(t, searchAction("profile query"), acceptAction(tt.result.URL))
			search := &staticSearch{results: []SearchResult{tt.result}}

			// When
			run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
				Backend: backend, Goal: "find sources", Tool: search, Options: options,
			})

			// Then
			if err != nil {
				t.Fatalf("DiscoverWithOptions() error = %v", err)
			}
			if got := len(run.Sources); got != tt.wantCount {
				t.Fatalf("source count = %d, want %d; sources=%#v", got, tt.wantCount, run.Sources)
			}
			if run.Metadata.Profile != tt.profile {
				t.Fatalf("metadata profile = %q, want %q", run.Metadata.Profile, tt.profile)
			}
		})
	}
}

func TestDiscoverWithOptions_enforces_profile_past_date_policy(t *testing.T) {
	// Given
	reference := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	result := SearchResult{Title: "Archive", URL: "https://news.example/announcements/archive", Date: "2025-01-01"}

	for _, name := range []DiscoveryProfileName{DiscoveryProfileEvents, DiscoveryProfileAnnouncements} {
		options := testOptions(t, name)
		options.MaxTurns = 2
		options.ReferenceTime = reference
		backend, _ := newScriptedBackend(t, searchAction("archive"), acceptAction(result.URL))
		run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
			Backend: backend, Goal: "archive", Tool: &staticSearch{results: []SearchResult{result}}, Options: options,
		})
		if err != nil {
			t.Fatalf("profile %q: %v", name, err)
		}
		want := 0
		if name == DiscoveryProfileAnnouncements {
			want = 1
		}
		if len(run.Sources) != want {
			t.Fatalf("profile %q source count = %d, want %d", name, len(run.Sources), want)
		}
	}
}

// One prompt now carries the whole rubric: the loop asks for an action, not for
// a proposal and then a judgement. Selecting a profile must still change what
// the model is told, on every turn, with no event-only values left behind.
func TestDiscoverWithOptions_GenericProfileChangesActionPrompt(t *testing.T) {
	// Given
	eventProfile, err := NamedDiscoveryProfile(DiscoveryProfileEvents)
	if err != nil {
		t.Fatalf("NamedDiscoveryProfile(events) error = %v", err)
	}
	genericProfile := genericPromptTestProfile(t)

	// When
	eventRequests := captureProfileModelRequests(t, eventProfile)
	genericRequests := captureProfileModelRequests(t, genericProfile)

	// Then
	for turn := range genericRequests {
		eventSystem := eventRequests[turn].Messages[0].Content
		genericSystem := genericRequests[turn].Messages[0].Content
		if genericSystem == eventSystem {
			t.Fatalf("turn %d: generic action system prompt equals the event one", turn)
		}
		for _, want := range []string{genericProfile.Purpose, genericProfile.AdmissibilityRubric, genericProfile.OutputLabel} {
			if !strings.Contains(genericSystem, want) {
				t.Fatalf("turn %d: generic action system prompt %q does not contain selected profile value %q",
					turn, genericSystem, want)
			}
		}
		for _, unwanted := range []string{eventProfile.Purpose, eventProfile.AdmissibilityRubric, eventProfile.OutputLabel} {
			if strings.Contains(genericSystem, unwanted) {
				t.Fatalf("turn %d: generic action system prompt retained event-only value %q: %q",
					turn, unwanted, genericSystem)
			}
		}
	}
	t.Logf("captured generic action markers: purpose=%q rubric=%q output_label=%q",
		genericProfile.Purpose, genericProfile.AdmissibilityRubric, genericProfile.OutputLabel)
}

func genericPromptTestProfile(t *testing.T) DiscoveryProfile {
	t.Helper()
	profile, err := NamedDiscoveryProfile(DiscoveryProfileEvents)
	if err != nil {
		t.Fatalf("NamedDiscoveryProfile(events) error = %v", err)
	}
	profile.Name = "generic_prompt_test"
	profile.Purpose = "generic-purpose-marker"
	profile.QueryTemplates = []string{"generic-template-one {goal}", "generic-template-two"}
	profile.AdmissibilityRubric = "generic-rubric-marker"
	profile.OutputLabel = "is_generic_source"
	profile.LocaleHints = []string{"generic-locale-marker"}
	profile.LanguageHints = []string{"generic-language-marker"}
	return profile
}

func captureProfileModelRequests(t *testing.T, profile DiscoveryProfile) []capturedChatRequest {
	t.Helper()
	options := DefaultDiscoverOptions(profile)
	options.MaxSearches = 1
	options.MaxTurns = 2
	candidateURL := "https://example.com/profile-source"
	backend, model := newScriptedBackend(t, searchAction("profile query"), acceptAction(candidateURL))
	run, err := DiscoverWithOptions(context.Background(), DiscoverRequest{
		Backend: backend,
		Goal:    "generic source goal",
		Tool:    &staticSearch{results: []SearchResult{{Title: "Profile Source", URL: candidateURL}}},
		Options: options,
	})
	if err != nil {
		t.Fatalf("DiscoverWithOptions() error = %v", err)
	}
	if len(run.Sources) != 1 {
		t.Fatalf("source count = %d, want 1", len(run.Sources))
	}
	if run.Metadata.Profile != profile.Name || run.Trace.Calls != 2 || run.Trace.Usage.CompletionTokens != 2 {
		t.Fatalf("profile/usage = profile %q calls %d completion_tokens %d, want %q/2/2",
			run.Metadata.Profile, run.Trace.Calls, run.Trace.Usage.CompletionTokens, profile.Name)
	}
	requests := model.capturedRequests()
	if len(requests) != 2 || len(requests[0].Messages) != 2 || len(requests[1].Messages) != 2 {
		t.Fatalf("captured model requests = %#v, want two system/user action turns", requests)
	}
	t.Logf("completed profile=%q model_calls=%d completion_tokens=%d accepted_sources=%d",
		run.Metadata.Profile, run.Trace.Calls, run.Trace.Usage.CompletionTokens, len(run.Sources))
	return requests
}
