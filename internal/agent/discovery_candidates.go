package agent

import (
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxCandidateTitleRunes    = 300
	maxCandidateSnippetRunes  = 1200
	maxCandidateMetadataRunes = 300
	maxReasonRunes            = 500
)

type offeredCandidate struct {
	canonicalURL string
	sourceKey    string
	result       SearchResult
}

func prepareCandidate(result SearchResult, options validatedDiscoverOptions) (offeredCandidate, bool) {
	canonicalURL, err := canonicalCandidateURL(result.URL)
	if err != nil {
		return offeredCandidate{}, false
	}
	parsed, err := url.Parse(canonicalURL)
	if err != nil {
		return offeredCandidate{}, false
	}
	if !matchesAny(options.urlPatterns, canonicalURL) || !matchesAny(options.domainPatterns, parsed.Hostname()) {
		return offeredCandidate{}, false
	}
	result.URL = canonicalURL
	result.Title = boundedRedactedText(result.Title, maxCandidateTitleRunes)
	result.Snippet = boundedRedactedText(result.Snippet, maxCandidateSnippetRunes)
	result.Date = boundedRedactedText(result.Date, maxCandidateMetadataRunes)
	result.Location = boundedRedactedText(result.Location, maxCandidateMetadataRunes)
	result.Locale = boundedRedactedText(result.Locale, maxCandidateMetadataRunes)
	result.Language = boundedRedactedText(result.Language, maxCandidateMetadataRunes)
	if !candidateMeetsRequirements(result, options) {
		return offeredCandidate{}, false
	}
	result.Provenance = validProvenance(canonicalURL, result.Provenance)
	return offeredCandidate{canonicalURL: canonicalURL, sourceKey: strings.ToLower(parsed.Hostname()), result: result}, true
}

func candidateMeetsRequirements(result SearchResult, options validatedDiscoverOptions) bool {
	requirements := options.Profile.Requirements
	if requirements.Title && result.Title == "" {
		return false
	}
	if requirements.Source && result.URL == "" {
		return false
	}
	if requirements.Location && result.Location == "" {
		return false
	}
	date, hasDate := parseCandidateDate(result.Date)
	if requirements.Date && !hasDate {
		return false
	}
	if options.Profile.PastDatePolicy == PastDatesRejected && hasDate {
		reference := options.ReferenceTime.UTC()
		referenceDate := time.Date(reference.Year(), reference.Month(), reference.Day(), 0, 0, 0, 0, time.UTC)
		if date.Before(referenceDate) {
			return false
		}
	}
	return true
}

func parseCandidateDate(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func validProvenance(candidateURL string, provenance *SourceProvenance) *SourceProvenance {
	if provenance == nil {
		return nil
	}
	record := *provenance
	record.Provider = boundedRedactedText(record.Provider, maxCandidateMetadataRunes)
	record.Relation = boundedRedactedText(record.Relation, maxCandidateMetadataRunes)
	if !validRelatedURL(record.SeedURL) || !validRelatedURL(record.ParentURL) {
		return nil
	}
	if record.RawURL != "" {
		canonicalRaw, err := canonicalCandidateURL(record.RawURL)
		if err != nil || canonicalRaw != candidateURL {
			return nil
		}
		record.RawURL = strings.TrimSpace(record.RawURL)
	}
	if record.SeedURL != "" {
		record.SeedURL, _ = canonicalCandidateURL(record.SeedURL)
	}
	if record.ParentURL != "" {
		record.ParentURL, _ = canonicalCandidateURL(record.ParentURL)
	}
	if record.Provider == "" && record.SeedURL == "" && record.ParentURL == "" && record.Relation == "" && record.RawURL == "" {
		return nil
	}
	return &record
}

func validRelatedURL(raw string) bool {
	if raw == "" {
		return true
	}
	_, err := canonicalCandidateURL(raw)
	return err == nil
}

func matchesAny(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func boundedRedactedText(raw string, maxRunes int) string {
	redacted := strings.TrimSpace(StripContacts(raw))
	if utf8.RuneCountInString(redacted) <= maxRunes {
		return redacted
	}
	runes := []rune(redacted)
	return string(runes[:maxRunes])
}
