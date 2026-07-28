package aiia

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// bodyDateRe matches only fully explicit year-month-day text, the shapes the
// normalize stage can parse. Anything vaguer stays out of StartRaw entirely.
var bodyDateRe = regexp.MustCompile(`20\d{2}년\s?\d{1,2}월\s?\d{1,2}일|20\d{2}-\d{2}-\d{2}|20\d{2}\.\d{1,2}\.\d{1,2}`)

// organizerPrefixRe captures the "[주최기관]" prefix convention board titles use.
var organizerPrefixRe = regexp.MustCompile(`^\[([^\[\]]{1,40})\]`)

// Parse turns one fetched board VIEW page into a raw ParsedEvent. Dates are
// scanned only inside the #DivContents body container: the header carries the
// posting date, which is not an event date, and many posts are image-only, in
// which case StartRaw honestly stays nil (일정 미정).
func (s *Source) Parse(_ context.Context, raw *fetch.Result) (*sources.ParsedEvent, error) {
	m := viewNumRe.FindStringSubmatch(raw.URL)
	if m == nil {
		return nil, fmt.Errorf("aiia parse: no post num in URL %q", raw.URL)
	}
	num := m[1]

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw.Body))
	if err != nil {
		return nil, fmt.Errorf("aiia parse: %w", err)
	}
	title := strings.TrimSpace(doc.Find(".bbs_view .tit .left em").First().Text())
	if title == "" {
		return nil, fmt.Errorf("aiia parse: empty title at %s", raw.URL)
	}

	ev := &sources.ParsedEvent{
		SourceID:     sourceID,
		EventID:      sourceID + "-" + num,
		URL:          raw.URL,
		Name:         title,
		ClassifyText: title,
		RetrievedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	if om := organizerPrefixRe.FindStringSubmatch(title); om != nil {
		org := strings.TrimSpace(om[1])
		ev.Organizer = &org
		ev.ClassifyText = title + " " + org
	}

	body := doc.Find("#DivContents").First().Text()
	start, end := extractEventDates(body)
	ev.StartRaw = start
	ev.EndRaw = end
	return ev, nil
}

// dateLabelRe marks a line that announces the event's own schedule, as opposed
// to a passing mention of some other date (a past edition, a deadline in
// running text).
var dateLabelRe = regexp.MustCompile(`일시|일정|기간|개최일`)

// extractEventDates picks raw date text a reader would recognize as THE event
// dates. A 일시/기간-labeled line wins outright. Without a label, only a body
// whose dates all sit in one line is unambiguous; scattered dates (past
// editions, tentative mentions) yield nil, which the pipeline reports as
// 일정 미정 rather than guessing.
func extractEventDates(body string) (start, end *string) {
	lines := strings.Split(body, "\n")
	// Pass 1: a labeled line anywhere in the body wins.
	for _, line := range lines {
		if !dateLabelRe.MatchString(line) {
			continue
		}
		if dates := bodyDateRe.FindAllString(line, 2); len(dates) > 0 {
			return firstTwo(dates)
		}
	}
	// Pass 2: without a label, only a single dated line is unambiguous.
	var found []string
	for _, line := range lines {
		dates := bodyDateRe.FindAllString(line, 2)
		if len(dates) == 0 {
			continue
		}
		if found != nil {
			return nil, nil // dates on multiple unlabeled lines: ambiguous
		}
		found = dates
	}
	if found == nil {
		return nil, nil
	}
	return firstTwo(found)
}

func firstTwo(dates []string) (start, end *string) {
	s := dates[0]
	start = &s
	if len(dates) > 1 {
		e := dates[1]
		end = &e
	}
	return start, end
}
