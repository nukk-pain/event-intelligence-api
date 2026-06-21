package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ---------------------------------------------------------------------------
// Operational bookkeeping for the ingest orchestrator (Task 4.1).
//
// These helpers back the per-item recover() audit trail (ingest_error), the
// per-source circuit-breaker record (fetch_anomaly), and the last-successful
// counts used to compute the discovery floor (batch_stats). They are thin,
// single-statement writers/readers — change detection and event persistence
// stay in store.go / diff.go.
// ---------------------------------------------------------------------------

// IngestError is one failed-item audit row (parse/normalize/panic isolation).
type IngestError struct {
	EventID string // durable id if known; "" -> NULL
	Source  string // adapter id
	Stage   string // fetch|parse|normalize|panic|store
	Message string // error / panic text
	BatchID string
}

// RecordIngestError appends one ingest_error row. EventID "" is stored as NULL.
func RecordIngestError(ctx context.Context, db *sql.DB, e IngestError) error {
	var eid any
	if e.EventID != "" {
		eid = e.EventID
	}
	const q = `
INSERT INTO ingest_error (event_id, source, stage, message, batch_id)
VALUES (?, ?, ?, ?, ?)`
	if _, err := db.ExecContext(ctx, q, eid, e.Source, e.Stage, e.Message, e.BatchID); err != nil {
		return fmt.Errorf("record ingest_error (%s/%s): %w", e.Source, e.Stage, err)
	}
	return nil
}

// FetchAnomaly is one circuit-breaker trip record: when present, NO diffs/
// upserts were written for that source in that batch (prior rows preserved).
type FetchAnomaly struct {
	Source     string
	Reason     string
	Discovered int
	Parsed     int
	PrevStored int
	BatchID    string
}

// RecordFetchAnomaly appends one fetch_anomaly row.
func RecordFetchAnomaly(ctx context.Context, db *sql.DB, a FetchAnomaly) error {
	const q = `
INSERT INTO fetch_anomaly (source, reason, discovered, parsed, prev_stored, batch_id)
VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := db.ExecContext(ctx, q,
		a.Source, a.Reason, a.Discovered, a.Parsed, a.PrevStored, a.BatchID); err != nil {
		return fmt.Errorf("record fetch_anomaly (%s): %w", a.Source, err)
	}
	return nil
}

// BatchStats is the last-successful per-source count snapshot, the basis for the
// next run's discovery floor.
type BatchStats struct {
	Source     string
	Discovered int
	Parsed     int
	Stored     int
	BatchID    string
}

// LoadBatchStats returns the last successful per-source counts and whether a row
// exists. A missing row (found=false) means there is no prior baseline yet, so
// the floor check must NOT trip on a first run.
func LoadBatchStats(ctx context.Context, db *sql.DB, source string) (BatchStats, bool, error) {
	const q = `SELECT discovered, parsed, stored FROM batch_stats WHERE source=?`
	var s BatchStats
	s.Source = source
	err := db.QueryRowContext(ctx, q, source).Scan(&s.Discovered, &s.Parsed, &s.Stored)
	if err == sql.ErrNoRows {
		return BatchStats{Source: source}, false, nil
	}
	if err != nil {
		return BatchStats{}, false, fmt.Errorf("load batch_stats (%s): %w", source, err)
	}
	return s, true, nil
}

// SaveBatchStats upserts the per-source last-successful counts. Call this only
// after a source's batch was applied WITHOUT tripping the breaker, so the floor
// for the next run reflects a real successful run.
func SaveBatchStats(ctx context.Context, db *sql.DB, s BatchStats) error {
	const q = `
INSERT INTO batch_stats (source, discovered, parsed, stored, batch_id, updated_at)
VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
ON CONFLICT(source) DO UPDATE SET
    discovered=excluded.discovered,
    parsed=excluded.parsed,
    stored=excluded.stored,
    batch_id=excluded.batch_id,
    updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`
	if _, err := db.ExecContext(ctx, q, s.Source, s.Discovered, s.Parsed, s.Stored, s.BatchID); err != nil {
		return fmt.Errorf("save batch_stats (%s): %w", s.Source, err)
	}
	return nil
}
