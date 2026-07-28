package akei

import (
	"context"
	"fmt"
	"regexp"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// wrIDRe extracts schedule post ids from listing hrefs. The listing wraps the
// real URL behind a "#" toggle anchor, so this matches the query pair rather
// than a full URL shape.
var wrIDRe = regexp.MustCompile(`bo_table=schedule[^"']*?wr_id=(\d+)`)

const (
	// monthsAhead is the rolling window: current month plus two, matching how
	// far ahead organizers reliably list on this board.
	monthsAhead = 3
	// maxPagesPerMonth bounds paging per month view; paging also stops early on
	// the first page that yields no new wr_id.
	maxPagesPerMonth = 6
)

// Discover walks the month views of the nationwide schedule board and returns
// one Ref per distinct schedule entry. Everything listed is returned — the
// taxonomy classifier downstream decides category and the excluded flag, the
// same policy the venue lists use.
func (s *Source) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	seen := map[string]bool{}
	var refs []sources.Ref

	month := s.now().UTC()
	for m := 0; m < monthsAhead; m++ {
		y, mo := month.Year(), int(month.Month())
		for page := 1; page <= maxPagesPerMonth; page++ {
			listURL := fmt.Sprintf("%s/bbs/board.php?bo_table=schedule&searchYear=%d&searchMonth=%02d&page=%d",
				s.baseURL, y, mo, page)
			res, err := f.Fetch(ctx, listURL, fetch.Conditional{})
			if err != nil {
				return nil, fmt.Errorf("akei list fetch %d-%02d p%d: %w", y, mo, page, err)
			}
			if res.StatusCode != 200 {
				return nil, fmt.Errorf("akei list fetch %d-%02d p%d: status %d", y, mo, page, res.StatusCode)
			}
			added := 0
			for _, m := range wrIDRe.FindAllStringSubmatch(string(res.Body), -1) {
				id := m[1]
				if seen[id] {
					continue
				}
				seen[id] = true
				added++
				refs = append(refs, sources.Ref{
					EventID: sourceID + "-" + id,
					URL:     s.baseURL + "/bbs/board.php?bo_table=schedule&wr_id=" + id,
				})
			}
			if added == 0 {
				break // this month is exhausted (or the page just repeats)
			}
		}
		month = month.AddDate(0, 1, 0)
	}
	return refs, nil
}
