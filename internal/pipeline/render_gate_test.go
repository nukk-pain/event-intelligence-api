package pipeline

import (
	"testing"

	"github.com/smpain/event-intelligence-api/internal/sources"
)

func evt(start string, reg, exh *string) *sources.ParsedEvent {
	home := "https://example.com"
	return &sources.ParsedEvent{
		EventID:     "e-" + start,
		StartRaw:    strptr(start),
		EndRaw:      strptr(start),
		HomepageURL: &home,
		Actions:     sources.ActionSignals{RegistrationDeadline: reg, ExhibitorDeadline: exh},
	}
}

const gateNow = "2026-07-29T00:00:00Z"

func TestRenderEligible_SkipsPastEvents(t *testing.T) {
	// 62% of the catalog is past events; rendering their homepages spent the
	// per-run browser budget on rows no reader will ever be recommended.
	if renderEligible(evt("2024-01-10", nil, nil), gateNow, 4) {
		t.Error("a past event must not consume a render slot")
	}
}

func TestRenderEligible_SkipsEventsThatAlreadyHaveADeadline(t *testing.T) {
	got := strptr("2026-08-01")
	if renderEligible(evt("2026-09-01", got, nil), gateNow, 4) {
		t.Error("nothing to gain: the deadline is already known")
	}
}

func TestRenderEligible_SkipsFarFutureEvents(t *testing.T) {
	if renderEligible(evt("2027-06-01", nil, nil), gateNow, 4) {
		t.Error("an event ten months out can wait for a later batch")
	}
}

func TestRenderEligible_RotatesAcrossBatches(t *testing.T) {
	// With more eligible events than the cap, a fixed processing order means
	// the same rows win every day and the rest are never rendered at all.
	// Buckets rotate by day so the whole pool is covered over a cycle.
	events := []*sources.ParsedEvent{}
	for _, d := range []string{"2026-08-05", "2026-08-06", "2026-08-07", "2026-08-08", "2026-08-09", "2026-08-10", "2026-08-11", "2026-08-12"} {
		events = append(events, evt(d, nil, nil))
	}
	seen := map[string]bool{}
	// A full rotation cycle must reach every eligible event exactly once.
	for _, day := range []string{"2026-07-29T00:00:00Z", "2026-07-30T00:00:00Z", "2026-07-31T00:00:00Z", "2026-08-01T00:00:00Z"} {
		for _, e := range events {
			if renderEligible(e, day, 4) {
				if seen[e.EventID] {
					t.Fatalf("%s rendered twice in one cycle", e.EventID)
				}
				seen[e.EventID] = true
			}
		}
	}
	if len(seen) != len(events) {
		t.Fatalf("covered %d of %d events in a full cycle", len(seen), len(events))
	}
}

func TestRenderEligible_SingleBucketRendersEverythingEligible(t *testing.T) {
	if !renderEligible(evt("2026-09-01", nil, nil), gateNow, 1) {
		t.Error("with rotation disabled every eligible event must pass")
	}
}
