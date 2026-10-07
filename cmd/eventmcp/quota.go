package main

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	askPerTenMinutes       = 10
	askPerDay              = 60
	toolsPerMinute         = 10
	toolsPerDay            = 120
	sharedQueriesPerMinute = 20
	sharedQueriesPerDay    = 800
	maxTrackedClients      = 10000
)

type httpConfig struct {
	dailyLLM, concurrent, clientConcurrent, llmConcurrent int
	quotaDir                                              string
}

func loadHTTPConfig() (httpConfig, error) {
	c := httpConfig{quotaDir: os.Getenv("EVENTMCP_QUOTA_DIR")}
	for _, setting := range []struct {
		name          string
		value         *int
		def, min, max int
	}{
		{"EVENTMCP_LLM_DAILY_LIMIT", &c.dailyLLM, 200, 0, 10000},
		{"EVENTMCP_MAX_CONCURRENT", &c.concurrent, 8, 1, 64},
		{"EVENTMCP_CLIENT_CONCURRENT", &c.clientConcurrent, 2, 1, 8},
		{"EVENTMCP_LLM_CONCURRENT", &c.llmConcurrent, 2, 1, 16},
	} {
		*setting.value = setting.def
		if raw, exists := os.LookupEnv(setting.name); exists {
			v, err := strconv.Atoi(raw)
			if err != nil || v < setting.min || v > setting.max {
				return c, fmt.Errorf("%s must be an integer in %d..%d", setting.name, setting.min, setting.max)
			}
			*setting.value = v
		}
	}
	if c.quotaDir == "" {
		return c, fmt.Errorf("EVENTMCP_QUOTA_DIR is required in HTTP mode")
	}
	return c, nil
}

type quotaWindow struct {
	start time.Time
	count int
}

type windowLimit struct {
	w     *quotaWindow
	span  time.Duration
	limit int
}

func (w *quotaWindow) reset(now time.Time, span time.Duration) {
	if w.start.IsZero() || !now.Before(w.start.Add(span)) {
		w.start = now
		w.count = 0
	}
}

type toolWindows struct {
	minute, daily, askTenMinute, askDaily quotaWindow
	inFlight                              int
}

// toolQuota reserves execution without a waiting queue. Expiration must include
// every quota window and all active work, never merely time since last access.
type toolQuota struct {
	mu                        sync.Mutex
	cfg                       httpConfig
	clients                   map[string]*toolWindows
	sharedMinute, sharedDaily quotaWindow
	inFlight, llmInFlight     int
	now                       func() time.Time
	nextSweep                 time.Time
}

func newToolQuota(cfg httpConfig) *toolQuota {
	return &toolQuota{cfg: cfg, clients: map[string]*toolWindows{}, now: time.Now}
}

func (q *toolQuota) acquire(client string, ask bool) (release func(), retry int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	if !now.Before(q.nextSweep) {
		for k, w := range q.clients {
			if w.inFlight == 0 && !now.Before(w.daily.start.Add(24*time.Hour)) &&
				!now.Before(w.minute.start.Add(time.Minute)) && !now.Before(w.askDaily.start.Add(24*time.Hour)) &&
				!now.Before(w.askTenMinute.start.Add(10*time.Minute)) {
				delete(q.clients, k)
			}
		}
		q.nextSweep = now.Add(time.Minute)
	}
	w := q.clients[client]
	if w == nil {
		if len(q.clients) >= maxTrackedClients {
			return nil, 60
		}
		w = &toolWindows{}
		q.clients[client] = w
	}
	if q.inFlight >= q.cfg.concurrent || w.inFlight >= q.cfg.clientConcurrent || (ask && q.llmInFlight >= q.cfg.llmConcurrent) {
		return nil, 1
	}
	checks := []windowLimit{
		{&w.minute, time.Minute, toolsPerMinute}, {&w.daily, 24 * time.Hour, toolsPerDay},
		{&q.sharedMinute, time.Minute, sharedQueriesPerMinute}, {&q.sharedDaily, 24 * time.Hour, sharedQueriesPerDay},
	}
	if ask {
		checks = append(checks,
			windowLimit{&w.askTenMinute, 10 * time.Minute, askPerTenMinutes},
			windowLimit{&w.askDaily, 24 * time.Hour, askPerDay})
	}
	for _, check := range checks {
		check.w.reset(now, check.span)
		if check.w.count >= check.limit {
			return nil, retrySeconds(check.w.start.Add(check.span).Sub(now))
		}
	}
	for _, check := range checks {
		check.w.count++
	}
	w.inFlight++
	q.inFlight++
	if ask {
		q.llmInFlight++
	}
	return func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		w.inFlight--
		q.inFlight--
		if ask {
			q.llmInFlight--
		}
	}, 0
}

func retrySeconds(d time.Duration) int { return max(1, int((d+time.Second-1)/time.Second)) }
