package aiia

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// viewNumRe extracts the durable post number from a board VIEW link.
var viewNumRe = regexp.MustCompile(`mode=VIEW&(?:amp;)?num=(\d+)`)

// eventTitleRe is the deterministic event filter. The board mixes event
// announcements with recruiting (모집) and administrative notices; only titles
// carrying an event-shaped noun are promoted. Conservative on purpose: a
// missed event costs one crawl cycle, a false positive pollutes the public
// dataset.
var eventTitleRe = regexp.MustCompile(`(?i)행사|세미나|컨퍼런스|콘퍼런스|설명회|교류회|포럼|박람회|전시회|데모데이|성과공유회|tech[- ]?day|밋업|meetup`)

// Discover lists event-like posts from the first (most recent) board page.
// One page (~15 posts) plus the daily cron keeps coverage complete without
// deep pagination against a slow origin.
func (s *Source) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	res, err := f.Fetch(ctx, s.baseURL+boardPath, fetch.Conditional{})
	if err != nil {
		return nil, fmt.Errorf("aiia list fetch: %w", err)
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("aiia list fetch: status %d", res.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(res.Body))
	if err != nil {
		return nil, fmt.Errorf("aiia list parse: %w", err)
	}

	seen := map[string]bool{}
	var refs []sources.Ref
	doc.Find(`a[href*="mode=VIEW"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		m := viewNumRe.FindStringSubmatch(href)
		if m == nil {
			return
		}
		num := m[1]
		title := strings.TrimSpace(a.Text())
		if title == "" || seen[num] || !eventTitleRe.MatchString(title) {
			return
		}
		seen[num] = true
		refs = append(refs, sources.Ref{
			EventID: sourceID + "-" + num,
			URL:     s.baseURL + "/bbs/board.php?tbl=bbs41&mode=VIEW&num=" + num,
		})
	})
	return refs, nil
}
