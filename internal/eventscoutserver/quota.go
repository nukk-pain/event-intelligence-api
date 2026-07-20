package eventscoutserver

import (
	"sync"
	"time"
)

type quotaLimiter struct {
	mu      sync.Mutex
	clock   Clock
	clients map[string]*clientQuota
}

type clientQuota struct {
	tenMinute quotaWindow
	daily     quotaWindow
}

type quotaWindow struct {
	start time.Time
	count int
}

type quotaDecision struct {
	allowed    bool
	retryAfter int
}

func newQuotaLimiter(clock Clock) *quotaLimiter {
	return &quotaLimiter{clock: clock, clients: make(map[string]*clientQuota)}
}

func (limiter *quotaLimiter) allow(clientKey string) quotaDecision {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.clock.Now()
	quota, exists := limiter.clients[clientKey]
	if !exists {
		quota = &clientQuota{}
		limiter.clients[clientKey] = quota
	}
	resetWindow(&quota.daily, now, 24*time.Hour)
	resetWindow(&quota.tenMinute, now, 10*time.Minute)
	if quota.daily.count >= dailyRequestLimit {
		return quotaDecision{retryAfter: boundedRetryAfter(quota.daily.start.Add(24 * time.Hour).Sub(now))}
	}
	if quota.tenMinute.count >= tenMinuteRequestLimit {
		return quotaDecision{retryAfter: boundedRetryAfter(quota.tenMinute.start.Add(10 * time.Minute).Sub(now))}
	}
	quota.daily.count++
	quota.tenMinute.count++
	return quotaDecision{allowed: true}
}

func resetWindow(window *quotaWindow, now time.Time, duration time.Duration) {
	if window.start.IsZero() || now.Before(window.start) || !now.Before(window.start.Add(duration)) {
		window.start = now
		window.count = 0
	}
}

func boundedRetryAfter(duration time.Duration) int {
	seconds := int((duration + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	if seconds > maximumRetryAfter {
		return maximumRetryAfter
	}
	return seconds
}
