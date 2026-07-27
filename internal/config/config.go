// Package config holds runtime configuration for the eventsintel binary.
package config

import (
	"os"
	"strconv"
	"time"
)

// SourceConfig describes one crawlable source.
type SourceConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Enabled bool   `json:"enabled"`
}

// Config is the top-level runtime configuration with sane defaults.
type Config struct {
	// Sources to crawl.
	Sources []SourceConfig `json:"sources"`

	// CrawlInterval is how often the ingest batch should run.
	CrawlInterval time.Duration `json:"crawl_interval"`

	// IngestDeadline is the whole-run wall-clock cap for a single ingest batch.
	// Combined with the flock single-flight lock this guarantees a hung source
	// Discover or a slow cumulative crawl cannot let one run exceed the cron
	// interval and silently starve every subsequent scheduled run. When the
	// deadline fires the orchestrator stops between items/sources and the run is
	// marked truncated (no batch_stats baseline is recorded for cut-short
	// sources). Must be comfortably below CrawlInterval.
	IngestDeadline time.Duration `json:"ingest_deadline"`

	// UserAgent is sent on every outbound HTTP request.
	UserAgent string `json:"user_agent"`

	// RateLimitPerMinute caps outbound fetches per minute per host.
	RateLimitPerMinute int `json:"rate_limit_per_minute"`

	// SourceConcurrency caps how many sources run at once.
	SourceConcurrency int `json:"source_concurrency"`

	// DetailWorkers caps per-source detail fetch/parse workers.
	DetailWorkers int `json:"detail_workers"`

	// SolarEnrich turns on ingest-time date enrichment. It requires
	// SolarAPIKey as well, so a key alone never starts spending.
	SolarEnrich bool `json:"solar_enrich"`
	// SolarAPIKey is an operator secret. It is never logged or serialized.
	SolarAPIKey  string `json:"-"`
	SolarBaseURL string `json:"solar_base_url"`
	SolarModel   string `json:"solar_model"`
	// SolarMaxCalls caps enrichment attempts for one ingest run. The default
	// covers every event currently missing action fields, so one run reaches full
	// coverage rather than advancing a backlog.
	SolarMaxCalls int `json:"solar_max_calls"`
	// SolarMaxConcurrent bounds simultaneous model work below the per-minute
	// token ceiling.
	SolarMaxConcurrent int `json:"solar_max_concurrent"`

	// DBPath is the SQLite file path.
	DBPath string `json:"db_path"`

	// LockPath is the ingest single-flight lock file (flock). Overlapping cron
	// runs that cannot take this lock are skipped cleanly (log + exit 0).
	LockPath string `json:"lock_path"`

	// HTTPAddr is the listen address for the read API.
	HTTPAddr string `json:"http_addr"`

	// MaxDiscoverPerSource caps how many events one ingest batch processes per
	// source, keeping the newest (0 = unbounded). COEX's sitemap exposes ~8.7k
	// historical exhibitions; a freshness-oriented MVP only needs recent ones, and
	// crawling the full history every run is neither polite nor useful.
	MaxDiscoverPerSource int `json:"max_discover_per_source"`

	// CFPurgeZoneID and CFPurgeToken configure the optional Cloudflare cache purge
	// fired after an ingest that actually changed data. Both empty (the default)
	// disables purging entirely — a no-op. Set via EVENTSINTEL_CF_PURGE_ZONE and
	// EVENTSINTEL_CF_PURGE_TOKEN on the ingest unit only. The token is a secret:
	// it is read from the environment, never defaulted in code, and never logged.
	CFPurgeZoneID string `json:"-"`
	CFPurgeToken  string `json:"-"`
}

// Default returns a Config populated with sane defaults.
func Default() Config {
	return Config{
		Sources: []SourceConfig{
			{ID: "coex", Name: "COEX", BaseURL: "https://www.coex.co.kr", Enabled: true},
			{ID: "kintex", Name: "KINTEX", BaseURL: "https://www.kintex.com", Enabled: true},
			{ID: "benchmark", Name: "International Benchmarks", BaseURL: "https://events.nukk.net/benchmarks", Enabled: true},
			{ID: "showala", Name: "SHOWALA (KINTEX)", BaseURL: "https://showala.com", Enabled: true},
		},
		CrawlInterval:        24 * time.Hour,
		IngestDeadline:       30 * time.Minute,
		UserAgent:            "eventsintel/0.1 (+https://events.nukk.net)",
		RateLimitPerMinute:   30,
		SourceConcurrency:    2,
		DetailWorkers:        4,
		SolarBaseURL:         "https://api.upstage.ai/v1",
		SolarModel:           "solar-open2",
		SolarMaxCalls:        600,
		SolarMaxConcurrent:   6,
		DBPath:               "eventsintel.db",
		LockPath:             "eventsintel.lock",
		HTTPAddr:             ":8080",
		MaxDiscoverPerSource: 400,
	}
}

// FromEnv returns Default() overlaid with any EVENTSINTEL_* environment
// overrides. This is how the production systemd units configure the binary
// (HTTP_ADDR=127.0.0.1:3005, DB_PATH=/srv/developer/events-intel/data/events.db,
// etc.) without a config file. Unset or unparseable vars leave the default.
func FromEnv() Config {
	c := Default()
	if v := os.Getenv("EVENTSINTEL_HTTP_ADDR"); v != "" {
		c.HTTPAddr = v
	}
	if v := os.Getenv("EVENTSINTEL_DB_PATH"); v != "" {
		c.DBPath = v
	}
	if v := os.Getenv("EVENTSINTEL_LOCK_PATH"); v != "" {
		c.LockPath = v
	}
	if v := os.Getenv("EVENTSINTEL_USER_AGENT"); v != "" {
		c.UserAgent = v
	}
	if v := os.Getenv("EVENTSINTEL_RATE_PER_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.RateLimitPerMinute = n
		}
	}
	if v := os.Getenv("EVENTSINTEL_SOURCE_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.SourceConcurrency = n
		}
	}
	c.SolarAPIKey = os.Getenv("EVENTSINTEL_SOLAR_API_KEY")
	c.SolarEnrich = os.Getenv("EVENTSINTEL_SOLAR_ENRICH") == "1" && c.SolarAPIKey != ""
	if v := os.Getenv("EVENTSINTEL_SOLAR_BASE_URL"); v != "" {
		c.SolarBaseURL = v
	}
	if v := os.Getenv("EVENTSINTEL_SOLAR_MODEL"); v != "" {
		c.SolarModel = v
	}
	if v := os.Getenv("EVENTSINTEL_SOLAR_MAX_CALLS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.SolarMaxCalls = n
		}
	}
	if v := os.Getenv("EVENTSINTEL_SOLAR_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.SolarMaxConcurrent = n
		}
	}
	if v := os.Getenv("EVENTSINTEL_DETAIL_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.DetailWorkers = n
		}
	}
	if v := os.Getenv("EVENTSINTEL_INGEST_DEADLINE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.IngestDeadline = d
		}
	}
	if v := os.Getenv("EVENTSINTEL_MAX_DISCOVER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.MaxDiscoverPerSource = n
		}
	}
	// Cloudflare purge is opt-in: leave both empty to disable. No default — the
	// token is a secret and must come from the environment.
	c.CFPurgeZoneID = os.Getenv("EVENTSINTEL_CF_PURGE_ZONE")
	c.CFPurgeToken = os.Getenv("EVENTSINTEL_CF_PURGE_TOKEN")
	return c
}
