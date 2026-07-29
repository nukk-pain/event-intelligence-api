package enrich

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

var (
	registerKeywords = []string{"사전등록", "등록하기", "관람등록", "참관등록", "registration", "register", "ticket"}
	exhibitKeywords  = []string{"전시참가", "부스", "exhibit", "exhibitor", "booth"}
	sponsorKeywords  = []string{"스폰서", "후원", "sponsor", "sponsorship"}
	matchKeywords    = []string{"매치메이킹", "비즈니스 상담", "buyer meeting", "matchmaking"}
	startupKeywords  = []string{"스타트업", "startup", "pitch", "demo day"}
	// Korean pages write dates as 2026.08.26, 2026-08-26, 2026/08/15, and —
	// most commonly on organizer pages — 2026년 9월 1일. All carry an explicit
	// year+month+day, which is the bar for storing a deadline at all.
	dateRe = regexp.MustCompile(`20[0-9]{2}\s*년\s*[01]?[0-9]\s*월\s*[0-3]?[0-9]\s*일?|20[0-9]{2}[.\-/][01]?[0-9][.\-/][0-3]?[0-9]`)
)

func ExtractActions(pageURL string, body []byte) (sources.ActionSignals, error) {
	doc, err := fetch.ParseHTML(string(body))
	if err != nil {
		return sources.ActionSignals{}, err
	}

	var signals sources.ActionSignals
	text := strings.ToLower(doc.Text())
	signals.CostHint = costHint(text)
	signals.RegistrationDeadline = deadlineNear(text, registrationDeadlineLabels)
	signals.ExhibitorDeadline = deadlineNear(text, exhibitorDeadlineLabels)

	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href := strings.TrimSpace(a.AttrOr("href", ""))
		resolved, ok := resolveHTTPURL(pageURL, href)
		if !ok {
			return
		}
		label := strings.ToLower(strings.Join([]string{
			strings.TrimSpace(a.Text()),
			href,
		}, " "))

		switch {
		case hasAny(label, registerKeywords):
			setTrue(&signals.CanRegister)
			setString(&signals.RegisterURL, resolved)
		case hasAny(label, exhibitKeywords):
			setTrue(&signals.CanExhibit)
			setString(&signals.ExhibitURL, resolved)
		case hasAny(label, sponsorKeywords):
			setTrue(&signals.CanSponsor)
		case hasAny(label, matchKeywords):
			setTrue(&signals.HasMatchmaking)
		case hasAny(label, startupKeywords):
			setTrue(&signals.HasStartupProgram)
		}
	})
	return signals, nil
}

func resolveHTTPURL(pageURL, href string) (string, bool) {
	if strings.TrimSpace(href) == "" {
		return "", false
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(ref)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", false
	}
	if strings.TrimSpace(resolved.Host) == "" {
		return "", false
	}
	return resolved.String(), true
}

func costHint(text string) *string {
	hasFree := strings.Contains(text, "무료") || strings.Contains(text, "free")
	hasPaid := strings.Contains(text, "유료") ||
		strings.Contains(text, "paid") || strings.Contains(text, "fee")
	switch {
	case hasFree && hasPaid:
		return strPtr("mixed")
	case hasFree:
		return strPtr("free")
	case hasPaid:
		return strPtr("paid")
	default:
		return nil
	}
}

// Every label contains an explicit deadline word (마감/deadline). That gate is
// what keeps notice-post dates and event-run dates out of the deadline fields
// (migration 0010 exists because looser values misled users).
var (
	registrationDeadlineLabels = []string{
		"참가 신청 마감", "참가신청 마감", "사전등록 마감", "사전등록마감",
		"등록 마감", "등록마감", "신청 마감", "신청마감", "접수 마감", "접수마감",
		"registration deadline",
	}
	exhibitorDeadlineLabels = []string{
		"부스 신청 마감", "부스신청 마감", "부스 마감", "부스마감",
		"전시참가 마감", "전시참가마감", "출품 마감",
		"exhibitor deadline", "booth deadline",
	}
	// allDeadlineLabels bound the search window of one label so it cannot
	// read past the start of the next deadline fact.
	allDeadlineLabels = append(append([]string{}, registrationDeadlineLabels...), exhibitorDeadlineLabels...)
)

// deadlineNear finds a date after a deadline label ("마감: 날짜"). Only the
// after side is searched: goquery joins text nodes without whitespace, so the
// previous fact's date can touch the label at distance zero and a
// before-window would steal it (observed: "…2026.07.31부스 신청 마감…").
// The window also stops at the next deadline label so one fact's date cannot
// leak into another ("사전등록 마감부스마감 2026/08/15"). A missed deadline
// is recoverable; a wrong one misleads users.
func deadlineNear(text string, labels []string) *string {
	for _, label := range labels {
		idx := strings.Index(text, strings.ToLower(label))
		if idx < 0 {
			continue
		}
		winStart := idx + len(label)
		end := winStart + 80
		if end > len(text) {
			end = len(text)
		}
		window := text[winStart:end]
		for _, boundary := range allDeadlineLabels {
			if b := strings.Index(window, boundary); b >= 0 {
				window = window[:b]
			}
		}
		if found := dateRe.FindString(window); found != "" {
			return strPtr(canonicalDateText(found))
		}
	}
	return nil
}

// ActionPageKind tells DeadlineOnActionPage which deadline the page's purpose
// implies. A 사전등록 label on an exhibit page is the visitor deadline, not
// the exhibitor one — assigning it would repeat the wrong_type failure the
// enrichment audit caught (migration 0010).
type ActionPageKind int

const (
	RegisterPage ActionPageKind = iota
	ExhibitPage
)

// Enrollment-window labels whose range-end date is a deadline — only on a
// page whose purpose is enrollment. Callers must pass register_url or
// exhibit_url page bodies, never homepages, or the event-run period would be
// misread as a deadline.
var (
	registerPeriodLabels = []string{
		"사전등록", "등록 기간", "등록기간", "접수 기간", "접수기간", "신청 기간", "신청기간",
	}
	// Exhibit pages get only application-window labels: 사전등록/등록 there
	// usually means visitor registration.
	exhibitPeriodLabels = []string{
		"신청 기간", "신청기간", "접수 기간", "접수기간", "참가업체 모집",
	}
)

// DeadlineOnActionPage extracts a deadline from a registration/exhibit page.
// An explicit deadline label of the page's own kind wins; otherwise the last
// date after an enrollment period label counts when the window reads as a
// range (~ or 까지).
func DeadlineOnActionPage(body []byte, kind ActionPageKind) *string {
	doc, err := fetch.ParseHTML(string(body))
	if err != nil {
		return nil
	}
	text := strings.ToLower(doc.Text())

	deadlineLabels, periodLabels := registrationDeadlineLabels, registerPeriodLabels
	if kind == ExhibitPage {
		deadlineLabels, periodLabels = exhibitorDeadlineLabels, exhibitPeriodLabels
	}
	if found := deadlineNear(text, deadlineLabels); found != nil {
		return found
	}

	for _, label := range periodLabels {
		idx := strings.Index(text, label)
		if idx < 0 {
			continue
		}
		end := idx + len(label) + 120
		if end > len(text) {
			end = len(text)
		}
		window := text[idx:end]
		if !strings.Contains(window, "~") && !strings.Contains(window, "까지") {
			continue
		}
		dates := dateRe.FindAllString(window, -1)
		if len(dates) > 0 {
			return strPtr(canonicalDateText(dates[len(dates)-1]))
		}
	}
	return nil
}

func canonicalDateText(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "년") && strings.Contains(raw, "월") && !strings.HasSuffix(raw, "일") {
		return raw + "일"
	}
	return raw
}

func hasAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func setTrue(dst **bool) {
	v := true
	*dst = &v
}

func setString(dst **string, value string) {
	if *dst == nil {
		*dst = &value
	}
}

func strPtr(s string) *string { return &s }
