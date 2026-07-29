package pipeline

import (
	"hash/crc32"
	"time"

	"github.com/smpain/event-intelligence-api/internal/normalize"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

// Exported so the ingest log can state the policy it just applied.
const (
	RenderHorizonDays     = renderHorizonDays
	RenderRotationBuckets = renderRotationBuckets
)

const (
	// renderHorizonDays bounds how far ahead a missing deadline is worth a
	// browser render. An event ten months out will come back into range on a
	// later batch, and deadlines carry forward once found.
	renderHorizonDays = 120
	// renderRotationBuckets spreads the eligible pool across daily batches.
	// The per-run render cap is far smaller than the pool (127 eligible vs 30
	// slots), and a fixed processing order meant the same rows won every day
	// while the rest were never rendered at all.
	renderRotationBuckets = 4
)

// renderEligible reports whether spending a headless render on this event can
// change anything. Rendering is the scarcest resource in a batch: it costs a
// browser page load under a 1.9GB VPS, so it goes only to upcoming events that
// still lack a deadline, within a horizon, and only in this batch's rotation
// bucket.
func renderEligible(parsed *sources.ParsedEvent, now string, buckets int) bool {
	if parsed == nil || !eventUpcoming(parsed, now) {
		return false
	}
	if parsed.Actions.RegistrationDeadline != nil || parsed.Actions.ExhibitorDeadline != nil {
		return false // nothing left for a render to discover
	}
	if !withinRenderHorizon(parsed, now) {
		return false
	}
	if buckets <= 1 {
		return true
	}
	return renderBucket(parsed.EventID, buckets) == rotationIndex(now, buckets)
}

func withinRenderHorizon(parsed *sources.ParsedEvent, now string) bool {
	raw := parsed.StartRaw
	if raw == nil || *raw == "" {
		raw = parsed.EndRaw
	}
	if raw == nil || *raw == "" {
		return false
	}
	iso, ok := normalize.ParseDate(*raw)
	if !ok || len(now) < 10 {
		return false
	}
	start, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return false
	}
	today, err := time.Parse("2006-01-02", now[:10])
	if err != nil {
		return false
	}
	return !start.After(today.AddDate(0, 0, renderHorizonDays))
}

// renderBucket is stable per event so an event stays in the same rotation slot
// across batches instead of drifting.
func renderBucket(eventID string, buckets int) int {
	return int(crc32.ChecksumIEEE([]byte(eventID))) % buckets
}

// rotationIndex advances once per day, so a full cycle covers the whole
// eligible pool.
func rotationIndex(now string, buckets int) int {
	day, err := time.Parse("2006-01-02", now[:10])
	if err != nil {
		return 0
	}
	return int(day.Unix()/86400) % buckets
}
