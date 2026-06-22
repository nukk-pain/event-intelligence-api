package fetch

import (
	"context"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// hostThrottle records a host's effective Crawl-delay and the time of its last
// fetch so the next fetch to that host can be gated independently of other
// hosts.
type hostThrottle struct {
	delay     time.Duration
	lastFetch time.Time
}

func (f *Fetcher) waitHostRateLimit(ctx context.Context, u *url.URL) error {
	return f.limiterFor(u.Hostname()).Wait(ctx)
}

func (f *Fetcher) limiterFor(host string) *rate.Limiter {
	key := strings.ToLower(host)
	f.limiterMu.Lock()
	defer f.limiterMu.Unlock()
	limiter, ok := f.limiters[key]
	if ok {
		return limiter
	}
	perMinute := f.perMinute
	if perMinute <= 0 {
		perMinute = defaultPerMinute
	}
	limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(perMinute)), 1)
	f.limiters[key] = limiter
	return limiter
}

// recordCrawlDelay stores (or refreshes) the effective Crawl-delay for host.
// A non-positive delay clears any prior throttle for that host. Base request
// limiters are never mutated, so one host's Crawl-delay cannot bleed into others.
func (f *Fetcher) recordCrawlDelay(host string, d time.Duration) {
	host = strings.ToLower(host)
	f.hostGateMu.Lock()
	defer f.hostGateMu.Unlock()
	g, ok := f.hostGate[host]
	if !ok {
		g = &hostThrottle{}
		f.hostGate[host] = g
	}
	g.delay = d
}

// waitHostCrawlDelay blocks until at least the host's Crawl-delay has elapsed
// since that host's previous fetch, then marks the host as fetched-now. Hosts
// without a Crawl-delay are not gated. Only the named host is affected.
func (f *Fetcher) waitHostCrawlDelay(ctx context.Context, host string) error {
	host = strings.ToLower(host)

	f.hostGateMu.Lock()
	g, ok := f.hostGate[host]
	if !ok || g.delay <= 0 {
		if !ok {
			g = &hostThrottle{}
			f.hostGate[host] = g
		}
		g.lastFetch = time.Now()
		f.hostGateMu.Unlock()
		return nil
	}
	var wait time.Duration
	if !g.lastFetch.IsZero() {
		if elapsed := time.Since(g.lastFetch); elapsed < g.delay {
			wait = g.delay - elapsed
		}
	}
	g.lastFetch = time.Now().Add(wait)
	f.hostGateMu.Unlock()

	if wait <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}
