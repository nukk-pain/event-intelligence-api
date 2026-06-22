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
	dateRe           = regexp.MustCompile(`20[0-9]{2}[.-][01]?[0-9][.-][0-3]?[0-9]`)
)

func ExtractActions(pageURL string, body []byte) (sources.ActionSignals, error) {
	doc, err := fetch.ParseHTML(string(body))
	if err != nil {
		return sources.ActionSignals{}, err
	}

	var signals sources.ActionSignals
	text := strings.ToLower(doc.Text())
	signals.CostHint = costHint(text)
	signals.RegistrationDeadline = deadlineNear(text, []string{"참가 신청 마감", "등록 마감", "registration deadline"})
	signals.ExhibitorDeadline = deadlineNear(text, []string{"부스 신청 마감", "전시참가 마감", "exhibitor deadline"})

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

func deadlineNear(text string, labels []string) *string {
	for _, label := range labels {
		idx := strings.Index(text, strings.ToLower(label))
		if idx < 0 {
			continue
		}
		end := idx + 80
		if end > len(text) {
			end = len(text)
		}
		if found := dateRe.FindString(text[idx:end]); found != "" {
			return strPtr(found)
		}
	}
	return nil
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
