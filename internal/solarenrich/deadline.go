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
