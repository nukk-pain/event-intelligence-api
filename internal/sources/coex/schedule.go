package coex

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

const currentSchedulePath = "/event/full-schedules/"

const scheduleLookaheadDays = 365

const maxSchedulePages = 80

func (s *Source) discoverScheduleRefs(ctx context.Context, f *fetch.Fetcher) []sources.Ref {
	var refs []sources.Ref
	seenPages := make(map[string]struct{})
	queue := currentSchedulePaths(s.now())

	for len(queue) > 0 && len(seenPages) < maxSchedulePages {
		path := queue[0]
		queue = queue[1:]
		if _, dup := seenPages[path]; dup {
			continue
		}
		seenPages[path] = struct{}{}

		res, err := f.Fetch(ctx, s.baseURL+path, fetch.Conditional{})
		if err != nil || res.StatusCode != http.StatusOK {
			continue
		}
		refs = append(refs, currentScheduleRefs(s.baseURL, res.Body)...)
		for _, next := range schedulePagePaths(s.baseURL, res.Body) {
			if _, dup := seenPages[next]; !dup {
				queue = append(queue, next)
			}
		}
	}
	return refs
}

func currentSchedulePaths(now time.Time) []string {
	start := coexDate(now)
	end := start.AddDate(0, 0, scheduleLookaheadDays)
	rangePath := fmt.Sprintf(
		"%s?search_start_date=%s&search_end_date=%s&list_type=LIST",
		currentSchedulePath,
		start.Format("2006.01.02"),
		end.Format("2006.01.02"),
	)
	return []string{currentSchedulePath, rangePath}
}

func coexDate(now time.Time) time.Time {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		loc = time.FixedZone("KST", 9*60*60)
	}
	return now.In(loc)
}

func currentScheduleRefs(baseURL string, body []byte) []sources.Ref {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	refs := make([]sources.Ref, 0)
	seen := make(map[string]struct{})
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href, ok := sel.Attr("href")
		if !ok {
			return
		}
		ref, ok := scheduleHrefRef(baseURL, href)
		if !ok {
			return
		}
		if _, dup := seen[ref.EventID]; dup {
			return
		}
		seen[ref.EventID] = struct{}{}
		refs = append(refs, ref)
	})
	return refs
}

func schedulePagePaths(baseURL string, body []byte) []string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	doc.Find("a[href]").Each(func(_ int, sel *goquery.Selection) {
		href, ok := sel.Attr("href")
		if !ok {
			return
		}
		path, ok := schedulePagePath(baseURL, href)
		if !ok {
			return
		}
		if _, dup := seen[path]; dup {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	})
	return paths
}

func schedulePagePath(baseURL, href string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}
	if !u.IsAbs() {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if !strings.EqualFold(u.Host, base.Host) {
		return "", false
	}
	if strings.Trim(u.EscapedPath(), "/") != "event/full-schedules" {
		return "", false
	}
	u.Fragment = ""
	path := u.EscapedPath()
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return path, true
}

func scheduleHrefRef(baseURL, href string) (sources.Ref, bool) {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return sources.Ref{}, false
	}
	if !strings.HasPrefix(strings.Trim(u.EscapedPath(), "/"), "exhibitions/") {
		return sources.Ref{}, false
	}
	u.RawQuery = ""
	u.Fragment = ""
	if !u.IsAbs() {
		base, err := url.Parse(baseURL)
		if err != nil {
			return sources.Ref{}, false
		}
		u = base.ResolveReference(u)
	}
	slug, err := slugFromURL(u.String())
	if err != nil || slug == "" {
		return sources.Ref{}, false
	}
	return sources.Ref{EventID: "coex-" + slug, URL: u.String()}, true
}
