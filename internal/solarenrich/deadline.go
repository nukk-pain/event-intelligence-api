package solarenrich

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// koreanDate matches the shapes Korean venue pages use for a deadline, which
// the strict ISO gate discarded outright: "2026년 9월 1일", "2026.9.1",
// "2026/09/01", and the plain ISO form. Anything else — "선착순 마감",
// "예산 소진 시까지" — has no date in it and is still rejected rather than
// guessed at.
var koreanDate = regexp.MustCompile(`(\d{4})\s*[년./-]\s*(\d{1,2})\s*[월./-]\s*(\d{1,2})\s*일?`)

// normalizeDeadline converts a model-supplied deadline to ISO, or reports that
// it carries no usable date. It never invents a component: a string without an
// explicit year, month, and day is rejected.
func normalizeDeadline(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	match := koreanDate.FindStringSubmatch(raw)
	if match == nil {
		return "", false
	}
	year, _ := strconv.Atoi(match[1])
	month, _ := strconv.Atoi(match[2])
	day, _ := strconv.Atoi(match[3])
	if year < 2000 || year > 2100 || month < 1 || month > 12 || day < 1 || day > 31 {
		return "", false
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day), true
}

// deadlineKind names which deadline a date is claimed to be, so evidence can
// be checked against the right context words.
type deadlineKind int

const (
	registrationDeadlineKind deadlineKind = iota
	exhibitorDeadlineKind
)

// Context vocabularies mirror eval/audit.py. The 2026-07-29 audit found two
// stored values whose page context named the opposite deadline type (a fee
// balance date stored as a booth deadline, a booth early-application date
// stored as visitor registration), so type context is now checked at write
// time, not just at audit time.
var (
	deadlineWords   = []string{"마감", "까지", "접수", "신청", "deadline", "due", "close"}
	registerContext = []string{"등록", "참가 신청", "참가신청", "참관", "사전등록", "registration", "register"}
	exhibitContext  = []string{"부스", "출품", "전시 신청", "전시신청", "참가업체", "exhibitor", "booth", "exhibit"}
)

const evidenceWindowBytes = 120

// typedDateEvidence reports whether the ISO date appears in text the agent
// read, with BOTH a deadline word and this deadline type's own context words
// nearby. A date the model asserts but no read page supports — or one whose
// surrounding context belongs to the other deadline type — is discarded.
func typedDateEvidence(texts []string, iso string, kind deadlineKind) bool {
	match := isoDate.FindStringSubmatch(iso)
	if match == nil {
		return false
	}
	parts := strings.SplitN(iso, "-", 3)
	year, month, day := parts[0], strings.TrimPrefix(parts[1], "0"), strings.TrimPrefix(parts[2], "0")
	patterns := []*regexp.Regexp{
		regexp.MustCompile(year + `\s*[-./년]\s*0?` + month + `\s*[-./월]\s*0?` + day),
		regexp.MustCompile(`(^|[^0-9])0?` + month + `\s*[./월]\s*0?` + day + `($|[^0-9])`),
	}
	context := registerContext
	if kind == exhibitorDeadlineKind {
		context = exhibitContext
	}
	for _, text := range texts {
		lower := strings.ToLower(text)
		for _, pattern := range patterns {
			for _, loc := range pattern.FindAllStringIndex(lower, -1) {
				lo := loc[0] - evidenceWindowBytes
				if lo < 0 {
					lo = 0
				}
				hi := loc[1] + evidenceWindowBytes
				if hi > len(lower) {
					hi = len(lower)
				}
				window := lower[lo:hi]
				if containsAny(window, deadlineWords) && containsAny(window, context) {
					return true
				}
			}
		}
	}
	return false
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
