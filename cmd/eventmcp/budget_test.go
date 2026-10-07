package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func privateQuotaDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testBudget(t *testing.T, limit int) *dailyBudget {
	t.Helper()
	b, err := openDailyBudget(privateQuotaDir(t), limit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

func TestBudgetParallel200AndRestart(t *testing.T) {
	dir := privateQuotaDir(t)
	b, err := openDailyBudget(dir, 200)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 7, 23, 59, 0, 0, time.UTC)
	b.now = func() time.Time { return now }
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 250 {
		wg.Go(func() {
			err := b.reserve()
			if err == nil {
				allowed.Add(1)
			} else if !errors.Is(err, errDailyBudget) {
				t.Errorf("reserve: %v", err)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 200 {
		t.Fatalf("allowed %d, want 200", allowed.Load())
	}
	if other, err := openDailyBudget(dir, 200); err == nil {
		other.Close()
		t.Fatal("second process owner was allowed")
	}
	b.Close()
	b, err = openDailyBudget(dir, 200)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.now = func() time.Time { return now }
	if !errors.Is(b.reserve(), errDailyBudget) {
		t.Fatal("restart replenished daily budget")
	}
	now = now.Add(time.Minute)
	if err := b.reserve(); err != nil {
		t.Fatal("UTC rollover:", err)
	}
	now = now.Add(-time.Minute)
	if err := b.reserve(); err == nil {
		t.Fatal("backwards clock replenished budget")
	}
}

func TestBudgetFailsClosed(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		if err := testBudget(t, 0).reserve(); !errors.Is(err, errDailyBudget) {
			t.Fatal(err)
		}
	})
	t.Run("storage failure", func(t *testing.T) {
		b := testBudget(t, 200)
		b.file.Close()
		if b.reserve() == nil || b.reserve() == nil || !b.failed {
			t.Fatal("write failure allowed provider reservation")
		}
	})
	for _, contents := range []string{"garbage\n", "{}\n", "\n", `{"ts":"2026-10-07T00:00:00Z","event":"provider_reserved","actor":"eventmcp","payload":{"utc_day":"2026-10-07","count":1}}`} {
		t.Run("corruption", func(t *testing.T) {
			dir := privateQuotaDir(t)
			if err := os.WriteFile(filepath.Join(dir, "reservations.jsonl"), []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if b, err := openDailyBudget(dir, 200); err == nil {
				b.Close()
				t.Fatal("corrupt state accepted")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		dir := privateQuotaDir(t)
		if err := os.Symlink(filepath.Join(dir, "other"), filepath.Join(dir, "reservations.jsonl")); err != nil {
			t.Fatal(err)
		}
		if b, err := openDailyBudget(dir, 200); err == nil {
			b.Close()
			t.Fatal("symlink journal accepted")
		}
	})
	t.Run("public file", func(t *testing.T) {
		dir := privateQuotaDir(t)
		path := filepath.Join(dir, "reservations.jsonl")
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
		if b, err := openDailyBudget(dir, 200); err == nil {
			b.Close()
			t.Fatal("non-private journal accepted")
		}
	})
}

func TestHTTPConfigFailsClosed(t *testing.T) {
	t.Setenv("EVENTMCP_QUOTA_DIR", privateQuotaDir(t))
	for name, def := range map[string]string{"EVENTMCP_LLM_DAILY_LIMIT": "200", "EVENTMCP_MAX_CONCURRENT": "8", "EVENTMCP_CLIENT_CONCURRENT": "2", "EVENTMCP_LLM_CONCURRENT": "2"} {
		t.Setenv(name, def)
	}
	if _, err := loadHTTPConfig(); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]string{
		"EVENTMCP_LLM_DAILY_LIMIT":   {"", "-1", "10001", "NaN"},
		"EVENTMCP_MAX_CONCURRENT":    {"0", "65", "x"},
		"EVENTMCP_CLIENT_CONCURRENT": {"0", "9"},
		"EVENTMCP_LLM_CONCURRENT":    {"0", "17"},
	} {
		for _, value := range bad {
			t.Run(name+value, func(t *testing.T) {
				t.Setenv(name, value)
				if _, err := loadHTTPConfig(); err == nil {
					t.Fatal("invalid setting accepted")
				}
			})
		}
	}
	t.Setenv("EVENTMCP_LLM_DAILY_LIMIT", "0")
	if c, err := loadHTTPConfig(); err != nil || c.dailyLLM != 0 {
		t.Fatal("zero must disable paid calls")
	}
	t.Setenv("EVENTMCP_QUOTA_DIR", "")
	if _, err := loadHTTPConfig(); err == nil {
		t.Fatal("missing state path accepted")
	}
}
