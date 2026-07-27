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

// dateEvidence reports whether the ISO date is actually present, in any of the
// shapes Korean pages write it, in text the agent read. A deadline the model
// asserts but no read page contains is discarded: an accuracy audit of 26
// model-supplied deadlines found only 3 with page evidence and 1 that named
// the opposite deadline type, which is too wrong to store unchecked.
func dateEvidence(texts []string, iso string) bool {
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
	for _, text := range texts {
		for _, pattern := range patterns {
			if pattern.MatchString(text) {
				return true
			}
		}
	}
	return false
}
