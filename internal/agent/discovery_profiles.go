package agent

import (
	"fmt"
	"regexp"
	"strings"
)

type validatedDiscoverOptions struct {
	DiscoverOptions
	urlPatterns    []*regexp.Regexp
	domainPatterns []*regexp.Regexp
}

// NamedDiscoveryProfile returns a fresh copy of a built-in profile definition.
func NamedDiscoveryProfile(name DiscoveryProfileName) (DiscoveryProfile, error) {
	for _, profile := range namedDiscoveryProfiles() {
		if profile.Name == name {
			return cloneDiscoveryProfile(profile), nil
		}
	}
	return DiscoveryProfile{}, fmt.Errorf("profile %q: %w", name, ErrInvalidDiscoveryProfile)
}

func namedDiscoveryProfiles() []DiscoveryProfile {
	return []DiscoveryProfile{
		{
			Name:                DiscoveryProfileEvents,
			Purpose:             "Find sources that list or announce AI, robotics, bio, digital-health, and medical-device industry events in Korea and key international benchmarks.",
			QueryTemplates:      []string{"{goal} official event calendar", "{goal} organizer conference expo"},
			AdmissibilityRubric: "Accept venue calendars, organizer sites, and conference pages. Reject one-off news articles, personal blogs, and unrelated pages.",
			OutputLabel:         "is_event_source", Requirements: CandidateRequirements{Title: true, Source: true},
			AllowedURLPatterns: []string{`^https?://`}, AllowedDomainPatterns: []string{`.+`},
			PastDatePolicy: PastDatesRejected, LocaleHints: []string{"ko-KR", "en"}, LanguageHints: []string{"ko", "en"},
		},
		{
			Name:                DiscoveryProfileTraining,
			Purpose:             "Find official training, workshop, course, academy, bootcamp, and webinar sources.",
			QueryTemplates:      []string{"{goal} official training course", "{goal} workshop academy webinar"},
			AdmissibilityRubric: "Accept official pages for a scheduled training offering. Reject generic articles and undated resource pages.",
			OutputLabel:         "is_training_source", Requirements: CandidateRequirements{Title: true, Date: true, Source: true},
			AllowedURLPatterns: []string{`(?i)^https?://[^?#]*(training|course|learn|academy|workshop|bootcamp|webinar)`}, AllowedDomainPatterns: []string{`.+`},
			PastDatePolicy: PastDatesRejected, LocaleHints: []string{"ko-KR", "en"}, LanguageHints: []string{"ko", "en"},
		},
		{
			Name:                DiscoveryProfileConferences,
			Purpose:             "Find official conference, congress, summit, symposium, forum, and annual-meeting sources.",
			QueryTemplates:      []string{"{goal} official conference", "{goal} congress summit symposium forum"},
			AdmissibilityRubric: "Accept official scheduled conference pages with a stated place. Reject commentary, recaps, and event-free organization pages.",
			OutputLabel:         "is_conference_source", Requirements: CandidateRequirements{Title: true, Date: true, Location: true, Source: true},
			AllowedURLPatterns: []string{`(?i)^https?://[^?#]*(conference|congress|summit|symposium|forum|annual-meeting)`}, AllowedDomainPatterns: []string{`.+`},
			PastDatePolicy: PastDatesRejected, LocaleHints: []string{"ko-KR", "en"}, LanguageHints: []string{"ko", "en"},
		},
		{
			Name:                DiscoveryProfileAnnouncements,
			Purpose:             "Find official announcements, notices, press releases, and newsroom posts.",
			QueryTemplates:      []string{"{goal} official announcement", "{goal} press release notice newsroom"},
			AdmissibilityRubric: "Accept first-party announcement or notice pages. Reject reposts, commentary, and unrelated pages.",
			OutputLabel:         "is_announcement_source", Requirements: CandidateRequirements{Title: true, Source: true},
			AllowedURLPatterns: []string{`(?i)^https?://[^?#]*(announce|news|notice|press|media)`}, AllowedDomainPatterns: []string{`.+`},
			PastDatePolicy: PastDatesAllowed, LocaleHints: []string{"ko-KR", "en"}, LanguageHints: []string{"ko", "en"},
		},
	}
}

func validateDiscoverOptions(options DiscoverOptions) (validatedDiscoverOptions, error) {
	profile, urlPatterns, domainPatterns, err := validateDiscoveryProfile(options.Profile)
	if err != nil {
		return validatedDiscoverOptions{}, err
	}
	options.Profile = profile
	if options.MaxSearches <= 0 || options.MaxTurns <= 0 || options.MaxModelCalls <= 0 || options.MaxCompletionTokens <= 0 ||
		options.MaxTokensPerCall <= 0 || options.MaxCandidates <= 0 || options.MaxCandidatesPerSource <= 0 ||
		options.GoalMaxRunes <= 0 || options.PerCallTimeout <= 0 || options.ReferenceTime.IsZero() ||
		options.MaxOpens < 0 {
		return validatedDiscoverOptions{}, ErrInvalidDiscoverOptions
	}
	options.MaxSearches = min(options.MaxSearches, hardMaxSearches)
	options.MaxTurns = min(options.MaxTurns, hardMaxDiscoveryTurns)
	// Zero stays zero: it is the documented way to disable the open action.
	options.MaxOpens = min(options.MaxOpens, hardMaxDiscoveryOpens)
	options.MaxModelCalls = min(options.MaxModelCalls, hardMaxDiscoveryModelCalls)
	options.MaxCompletionTokens = min(options.MaxCompletionTokens, hardMaxDiscoveryCompletionTokens)
	options.MaxTokensPerCall = min(options.MaxTokensPerCall, options.MaxCompletionTokens)
	options.MaxCandidates = min(options.MaxCandidates, hardMaxDiscoveryCandidates)
	options.MaxCandidatesPerSource = min(options.MaxCandidatesPerSource, options.MaxCandidates)
	options.GoalMaxRunes = min(options.GoalMaxRunes, hardMaxDiscoveryGoalRunes)
	options.PerCallTimeout = min(options.PerCallTimeout, hardMaxDiscoveryPerCallTimeout)
	return validatedDiscoverOptions{DiscoverOptions: options, urlPatterns: urlPatterns, domainPatterns: domainPatterns}, nil
}

func validateDiscoveryProfile(profile DiscoveryProfile) (DiscoveryProfile, []*regexp.Regexp, []*regexp.Regexp, error) {
	if strings.TrimSpace(string(profile.Name)) == "" || strings.TrimSpace(profile.Purpose) == "" ||
		strings.TrimSpace(profile.AdmissibilityRubric) == "" || !profile.Requirements.Source ||
		len(profile.QueryTemplates) == 0 || len(profile.AllowedURLPatterns) == 0 || len(profile.AllowedDomainPatterns) == 0 ||
		len(profile.LocaleHints) == 0 || len(profile.LanguageHints) == 0 || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`).MatchString(profile.OutputLabel) ||
		profile.OutputLabel == "url" || profile.OutputLabel == "reason" {
		return DiscoveryProfile{}, nil, nil, ErrInvalidDiscoveryProfile
	}
	if profile.PastDatePolicy != PastDatesAllowed && profile.PastDatePolicy != PastDatesRejected {
		return DiscoveryProfile{}, nil, nil, ErrInvalidDiscoveryProfile
	}
	urls, err := compilePatterns(profile.AllowedURLPatterns)
	if err != nil {
		return DiscoveryProfile{}, nil, nil, fmt.Errorf("profile URL patterns: %w", err)
	}
	domains, err := compilePatterns(profile.AllowedDomainPatterns)
	if err != nil {
		return DiscoveryProfile{}, nil, nil, fmt.Errorf("profile domain patterns: %w", err)
	}
	return cloneDiscoveryProfile(profile), urls, domains, nil
}

func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile %q: %w", pattern, ErrInvalidDiscoveryProfile)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

func cloneDiscoveryProfile(profile DiscoveryProfile) DiscoveryProfile {
	profile.QueryTemplates = append([]string(nil), profile.QueryTemplates...)
	profile.AllowedURLPatterns = append([]string(nil), profile.AllowedURLPatterns...)
	profile.AllowedDomainPatterns = append([]string(nil), profile.AllowedDomainPatterns...)
	profile.LocaleHints = append([]string(nil), profile.LocaleHints...)
	profile.LanguageHints = append([]string(nil), profile.LanguageHints...)
	return profile
}
