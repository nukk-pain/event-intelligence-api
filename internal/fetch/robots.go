package fetch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type robotsRules struct {
	disallow   []string
	allow      []string
	crawlDelay time.Duration
}

type robotsCacheEntry struct {
	rules     *robotsRules
	fetchedAt time.Time
}

type robotsInflight struct {
	done  chan struct{}
	rules *robotsRules
	err   error
}

// robotsAllows fetches+caches robots.txt for u's host and reports whether the
// configured UA may fetch u.Path, plus any Crawl-delay.
func (f *Fetcher) robotsAllows(ctx context.Context, u *url.URL) (bool, time.Duration, error) {
	rules, err := f.robotsFor(ctx, u)
	if err != nil {
		// Be permissive on robots fetch failure (network/404): allow.
		return true, 0, nil
	}
	if rules == nil {
		return true, 0, nil
	}
	return rules.allows(u.Path), rules.crawlDelay, nil
}

func (f *Fetcher) robotsFor(ctx context.Context, u *url.URL) (*robotsRules, error) {
	key := u.Scheme + "://" + u.Host

	f.robotsMu.Lock()
	if e, ok := f.robots[key]; ok && time.Since(e.fetchedAt) < f.robotsTTL {
		rules := e.rules
		f.robotsMu.Unlock()
		return rules, nil
	}
	if in, ok := f.robotsInflight[key]; ok {
		done := in.done
		f.robotsMu.Unlock()
		return waitRobotsInflight(ctx, in, done)
	}
	in := &robotsInflight{done: make(chan struct{})}
	f.robotsInflight[key] = in
	f.robotsMu.Unlock()

	robotsURL := *u
	sharedCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), f.timeout)
	go func() {
		defer cancel()
		rules, err := f.fetchRobots(sharedCtx, &robotsURL, key)
		f.robotsMu.Lock()
		if err == nil {
			f.robots[key] = &robotsCacheEntry{rules: rules, fetchedAt: time.Now()}
		}
		in.rules = rules
		in.err = err
		delete(f.robotsInflight, key)
		close(in.done)
		f.robotsMu.Unlock()
	}()
	return waitRobotsInflight(ctx, in, in.done)
}

func waitRobotsInflight(ctx context.Context, in *robotsInflight, done <-chan struct{}) (*robotsRules, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		return in.rules, in.err
	}
}

func (f *Fetcher) fetchRobots(ctx context.Context, u *url.URL, key string) (*robotsRules, error) {
	if err := f.waitHostRateLimit(ctx, u); err != nil {
		return nil, err
	}
	robotsURL := key + "/robots.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.ua)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rules *robotsRules
	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		rules = parseRobots(body, f.ua)
	} else {
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		rules = &robotsRules{}
	}

	return rules, nil
}

// allows applies a simple longest-match Disallow/Allow prefix policy. An empty
// Disallow rule ("Disallow:") means allow-all and is ignored.
func (r *robotsRules) allows(path string) bool {
	if path == "" {
		path = "/"
	}
	bestAllow := -1
	for _, p := range r.allow {
		if strings.HasPrefix(path, p) && len(p) > bestAllow {
			bestAllow = len(p)
		}
	}
	bestDisallow := -1
	for _, p := range r.disallow {
		if p == "" {
			continue
		}
		if strings.HasPrefix(path, p) && len(p) > bestDisallow {
			bestDisallow = len(p)
		}
	}
	if bestDisallow == -1 {
		return true
	}
	return bestAllow >= bestDisallow
}

// parseRobots extracts the rules applicable to ua, falling back to the "*"
// group. It honors Disallow, Allow, and Crawl-delay directives.
func parseRobots(body []byte, ua string) *robotsRules {
	uaToken := robotsUAToken(ua)

	type group struct {
		agents []string
		rules  robotsRules
	}
	var groups []*group
	var cur *group
	expectingAgents := false

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		field := strings.ToLower(strings.TrimSpace(line[:colon]))
		value := strings.TrimSpace(line[colon+1:])

		switch field {
		case "user-agent":
			if cur == nil || !expectingAgents {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(value))
			expectingAgents = true
		case "disallow":
			if cur != nil {
				cur.rules.disallow = append(cur.rules.disallow, value)
			}
			expectingAgents = false
		case "allow":
			if cur != nil {
				cur.rules.allow = append(cur.rules.allow, value)
			}
			expectingAgents = false
		case "crawl-delay":
			if cur != nil {
				if secs, err := strconv.ParseFloat(value, 64); err == nil && secs > 0 {
					cur.rules.crawlDelay = time.Duration(secs * float64(time.Second))
				}
			}
			expectingAgents = false
		default:
			expectingAgents = false
		}
	}

	var specific, star *robotsRules
	for _, g := range groups {
		for _, a := range g.agents {
			if a == "*" {
				star = &g.rules
			} else if uaToken != "" && strings.Contains(uaToken, a) {
				specific = &g.rules
			}
		}
	}
	if specific != nil {
		return specific
	}
	if star != nil {
		return star
	}
	return &robotsRules{}
}

func robotsUAToken(ua string) string {
	return strings.ToLower(ua)
}
