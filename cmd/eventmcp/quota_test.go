package main

import (
	"fmt"
	"testing"
	"time"
)

func testToolQuota() *toolQuota {
	return newToolQuota(httpConfig{concurrent: 8, clientConcurrent: 2, llmConcurrent: 2})
}

func TestToolQuotaConcurrency(t *testing.T) {
	q := testToolQuota()
	a, _ := q.acquire("a", false)
	b, _ := q.acquire("a", false)
	if a == nil || b == nil {
		t.Fatal("client slots unavailable")
	}
	if extra, retry := q.acquire("a", false); extra != nil || retry != 1 {
		t.Fatal("client concurrency exceeded")
	}
	b()
	var releases []func()
	for i := 0; i < 7; i++ {
		r, _ := q.acquire(fmt.Sprint(i), false)
		if r == nil {
			t.Fatal("global slot unavailable")
		}
		releases = append(releases, r)
	}
	if extra, _ := q.acquire("overflow", false); extra != nil {
		t.Fatal("global concurrency exceeded")
	}
	for _, release := range releases {
		release()
	}
	a()
	a, _ = q.acquire("llm1", true)
	b, _ = q.acquire("llm2", true)
	if extra, _ := q.acquire("llm3", true); extra != nil {
		t.Fatal("LLM concurrency exceeded")
	}
	r, _ := q.acquire("search", false)
	if r == nil {
		t.Fatal("search incorrectly consumes an LLM slot")
	}
	r()
	a()
	b()
	if q.inFlight != 0 || q.llmInFlight != 0 {
		t.Fatal("execution slot leak")
	}
}

func TestToolQuotaExpirationAndCapacity(t *testing.T) {
	q := testToolQuota()
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return now }
	for i := 0; i < maxTrackedClients; i++ {
		q.clients[fmt.Sprint(i)] = &toolWindows{daily: quotaWindow{start: now, count: 120}, minute: quotaWindow{start: now, count: 10}}
	}
	if r, _ := q.acquire("overflow", false); r != nil {
		t.Fatal("full map accepted a new identity")
	}
	now = now.Add(2 * time.Hour)
	if r, _ := q.acquire("0", false); r != nil {
		t.Fatal("idle cleanup replenished daily quota")
	}
	if len(q.clients) != maxTrackedClients {
		t.Fatal("active quota was evicted")
	}
	now = now.Add(22 * time.Hour)
	if r, _ := q.acquire("new-day", false); r == nil {
		t.Fatal("expired entries not cleaned")
	} else {
		r()
	}
}

func TestToolQuotaKeepsLaterAskWindowAndActiveWork(t *testing.T) {
	q := testToolQuota()
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return now }
	q.clients["ask"] = &toolWindows{daily: quotaWindow{start: now}, askDaily: quotaWindow{start: now.Add(23 * time.Hour), count: 60}}
	q.clients["active"] = &toolWindows{daily: quotaWindow{start: now}, inFlight: 1}
	now = now.Add(24 * time.Hour)
	r, _ := q.acquire("new", false)
	if r == nil {
		t.Fatal("new request refused")
	}
	r()
	if q.clients["ask"] == nil || q.clients["active"] == nil {
		t.Fatal("evicted a later live window or active operation")
	}
	if r, _ := q.acquire("ask", true); r != nil {
		t.Fatal("later ask daily window replenished")
	}
}

func TestToolQuotaPerClientDailyAndAskWindows(t *testing.T) {
	q := testToolQuota()
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return now }
	for range toolsPerDay {
		r, _ := q.acquire("search", false)
		if r == nil {
			t.Fatal("search refused early")
		}
		r()
		now = now.Add(time.Minute)
	}
	if r, _ := q.acquire("search", false); r != nil {
		t.Fatal("client daily cap bypassed")
	}
	for i := 0; i < askPerDay; i++ {
		if i > 0 && i%askPerTenMinutes == 0 {
			now = now.Add(10 * time.Minute)
		}
		r, _ := q.acquire("ask", true)
		if r == nil {
			t.Fatalf("ask %d refused early", i)
		}
		r()
		if i == 9 {
			if r, _ := q.acquire("ask", true); r != nil {
				t.Fatal("ask short window cap bypassed")
			}
		}
	}
	now = now.Add(10 * time.Minute)
	if r, _ := q.acquire("ask", true); r != nil {
		t.Fatal("ask daily cap bypassed")
	}
}

func TestToolQuotaSharedReadBudget(t *testing.T) {
	q := testToolQuota()
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return now }
	for i := 0; i < 800; i++ {
		if i > 0 && i%20 == 0 {
			now = now.Add(time.Minute)
		}
		r, _ := q.acquire(fmt.Sprint(i), i%2 == 0)
		if r == nil {
			t.Fatalf("request %d refused", i)
		}
		r()
		if i == 19 {
			if r, retry := q.acquire("minute-overflow", false); r != nil || retry < 1 {
				t.Fatal("shared minute cap bypassed")
			}
		}
	}
	now = now.Add(time.Minute)
	if r, retry := q.acquire("daily-overflow", false); r != nil || retry < 1 {
		t.Fatal("shared daily cap bypassed")
	}
}
