package config

import (
	"os"
	"testing"
	"time"
)

func TestDefault_UsesDailyCrawlInterval(t *testing.T) {
	// Given / When
	cfg := Default()

	// Then
	if cfg.CrawlInterval != 24*time.Hour {
		t.Fatalf("CrawlInterval = %s, want 24h", cfg.CrawlInterval)
	}
}

func TestDefault_UsesConcurrencyDefaults(t *testing.T) {
	// Given / When
	cfg := Default()

	// Then
	if cfg.SourceConcurrency != 2 {
		t.Fatalf("SourceConcurrency = %d, want 2", cfg.SourceConcurrency)
	}
	if cfg.DetailWorkers != 4 {
		t.Fatalf("DetailWorkers = %d, want 4", cfg.DetailWorkers)
	}
}

func TestFromEnv_ConcurrencyOverrides(t *testing.T) {
	// Given
	t.Setenv("EVENTSINTEL_SOURCE_CONCURRENCY", "3")
	t.Setenv("EVENTSINTEL_DETAIL_WORKERS", "5")

	// When
	cfg := FromEnv()

	// Then
	t.Logf("SourceConcurrency=%d DetailWorkers=%d", cfg.SourceConcurrency, cfg.DetailWorkers)
	if cfg.SourceConcurrency != 3 {
		t.Fatalf("SourceConcurrency = %d, want 3", cfg.SourceConcurrency)
	}
	if cfg.DetailWorkers != 5 {
		t.Fatalf("DetailWorkers = %d, want 5", cfg.DetailWorkers)
	}
}

func TestFromEnv_ConcurrencyUsesDefaultsWhenUnset(t *testing.T) {
	// Given
	sourceConcurrency, hadSourceConcurrency := os.LookupEnv("EVENTSINTEL_SOURCE_CONCURRENCY")
	detailWorkers, hadDetailWorkers := os.LookupEnv("EVENTSINTEL_DETAIL_WORKERS")
	t.Cleanup(func() {
		if hadSourceConcurrency {
			if err := os.Setenv("EVENTSINTEL_SOURCE_CONCURRENCY", sourceConcurrency); err != nil {
				t.Fatalf("restore EVENTSINTEL_SOURCE_CONCURRENCY: %v", err)
			}
		} else {
			if err := os.Unsetenv("EVENTSINTEL_SOURCE_CONCURRENCY"); err != nil {
				t.Fatalf("unset EVENTSINTEL_SOURCE_CONCURRENCY: %v", err)
			}
		}
		if hadDetailWorkers {
			if err := os.Setenv("EVENTSINTEL_DETAIL_WORKERS", detailWorkers); err != nil {
				t.Fatalf("restore EVENTSINTEL_DETAIL_WORKERS: %v", err)
			}
		} else {
			if err := os.Unsetenv("EVENTSINTEL_DETAIL_WORKERS"); err != nil {
				t.Fatalf("unset EVENTSINTEL_DETAIL_WORKERS: %v", err)
			}
		}
	})
	if err := os.Unsetenv("EVENTSINTEL_SOURCE_CONCURRENCY"); err != nil {
		t.Fatalf("unset EVENTSINTEL_SOURCE_CONCURRENCY: %v", err)
	}
	if err := os.Unsetenv("EVENTSINTEL_DETAIL_WORKERS"); err != nil {
		t.Fatalf("unset EVENTSINTEL_DETAIL_WORKERS: %v", err)
	}

	// When
	cfg := FromEnv()

	// Then
	if cfg.SourceConcurrency != 2 {
		t.Fatalf("SourceConcurrency = %d, want 2", cfg.SourceConcurrency)
	}
	if cfg.DetailWorkers != 4 {
		t.Fatalf("DetailWorkers = %d, want 4", cfg.DetailWorkers)
	}
}

func TestFromEnv_ConcurrencyInvalidValuesFallBackToDefaults(t *testing.T) {
	tests := []struct {
		name              string
		sourceConcurrency string
		detailWorkers     string
	}{
		{name: "malformed", sourceConcurrency: "fast", detailWorkers: "many"},
		{name: "zero", sourceConcurrency: "0", detailWorkers: "0"},
		{name: "negative", sourceConcurrency: "-1", detailWorkers: "-4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			t.Setenv("EVENTSINTEL_SOURCE_CONCURRENCY", tt.sourceConcurrency)
			t.Setenv("EVENTSINTEL_DETAIL_WORKERS", tt.detailWorkers)

			// When
			cfg := FromEnv()

			// Then
			t.Logf("SourceConcurrency=%d DetailWorkers=%d", cfg.SourceConcurrency, cfg.DetailWorkers)
			if cfg.SourceConcurrency != 2 {
				t.Fatalf("SourceConcurrency = %d, want 2", cfg.SourceConcurrency)
			}
			if cfg.DetailWorkers != 4 {
				t.Fatalf("DetailWorkers = %d, want 4", cfg.DetailWorkers)
			}
		})
	}
}
