package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/smpain/event-intelligence-api/internal/model"
)

// ---------------------------------------------------------------------------
// Cross-source de-duplication (content_key + superseded)
// ---------------------------------------------------------------------------
//
// Two adapters can discover the SAME real-world event under different
// source-prefixed event_ids (e.g. "kintex-…" from list.do and "showala-…" from
// the SHOWALA portal). ContentKey computes a source-independent fingerprint so
// those rows can be clustered, and reconcileContentKey elects exactly one
// canonical row per cluster (superseded=0), hiding the rest (superseded=1).
//
// Properties (verified by dedup_test.go):
//   - order-independent: whichever adapter writes first, the final canonical is
//     the same — every applied event reconciles its whole cluster AFTER its own
//     upsert, so the last write in any cluster always sees every member.
//   - idempotent: re-applying a batch re-affirms the same superseded flags.
//   - single-source-safe: a cluster of one is always canonical; rows without a
//     content_key (no start_date / no venue_id) are never de-duped.
//   - soft: superseded never deletes a row; provenance/change_log/raw_snapshot
//     are preserved. It is excluded only from the public read path.

// sourceAuthority ranks the adapter that owns an event_id as a canonical
// candidate. Venue-native adapters (the venue's own site) outrank an aggregator
// portal, so the richer first-party row wins. Higher = more authoritative.
var sourceAuthority = map[string]int{
	"coex":      100,
	"kintex":    100,
	"benchmark": 100,
	"showala":   10,
}

// defaultAuthority is used for an unrecognized source prefix. It is HIGH on
// purpose: an unknown source is never auto-superseded by a known aggregator, so a
// classification gap can only ever fail safe (show a possible duplicate), never
// hide a real event.
const defaultAuthority = 100

// sourceOfEventID returns the adapter prefix of an event_id ("kintex-123" ->
// "kintex"), or "" when there is no prefix.
func sourceOfEventID(eventID string) string {
	if i := strings.IndexByte(eventID, '-'); i > 0 {
		return eventID[:i]
	}
	return ""
}

// authorityRank returns the canonical-candidate rank for an event_id.
func authorityRank(eventID string) int {
	if r, ok := sourceAuthority[sourceOfEventID(eventID)]; ok {
		return r
	}
	return defaultAuthority
}

var (
	// yearRe strips a 4-digit 20xx year (optionally with the Korean 년 suffix). The
	// year is already carried by start_date in the key, so dropping it from the
	// name lets "2026 로보월드" and "로보월드" align without widening the match.
	yearRe = regexp.MustCompile(`20\d{2}년?`)
	// editionKoRe strips a Korean edition marker like "제5회" / "제 5 회".
	editionKoRe = regexp.MustCompile(`제\s*\d+\s*회`)
	// editionEnRe strips an English ordinal edition like "5th" / "1st". Anchored
	// with word boundaries so it only matches a standalone ordinal token and never
	// shears a digit-then-"st/th" fragment out of the middle of another word.
	editionEnRe = regexp.MustCompile(`\b\d+(?:st|nd|rd|th)\b`)
	// nonAlnumRe strips everything that is not a letter or digit (spaces,
	// punctuation, ·, &, parentheses) so cosmetic spacing/punctuation differences
	// between two sources do not split the key.
	nonAlnumRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// normalizeEventName produces the name component of a content_key. It is
// deliberately CONSERVATIVE: it removes only the year, the edition marker, case,
// and non-alphanumerics. It does not stem, transliterate, or drop core words, so
// two genuinely different events do not collapse — a missed match (a visible
// duplicate) is the safe failure mode; a false merge (a hidden real event) is not.
func normalizeEventName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = yearRe.ReplaceAllString(s, "")
	s = editionKoRe.ReplaceAllString(s, "")
	s = editionEnRe.ReplaceAllString(s, "")
	s = nonAlnumRe.ReplaceAllString(s, "")
	return s
}

// ContentKey returns the cross-source fingerprint for an event, or "" when the
// event cannot be safely de-duped. The key is normalized-name|start_date|venue_id;
// all three must be present (a missing start_date or venue_id, or an empty
// normalized name, yields "" so the event always stands alone).
func ContentKey(e model.Event) string {
	if e.StartDate == nil || *e.StartDate == "" {
		return ""
	}
	if e.Venue == nil || e.Venue.VenueID == nil || *e.Venue.VenueID == "" {
		return ""
	}
	name := normalizeEventName(e.Name)
	if name == "" {
		return ""
	}
	return name + "|" + *e.StartDate + "|" + *e.Venue.VenueID
}

// reconcileContentKey re-elects the canonical row for one content_key cluster:
// the highest-authority event_id (ties broken by the lexicographically smallest
// id for stability) is set superseded=0, all others superseded=1. A NULL/empty
// key, or a cluster of one, is a no-op beyond ensuring the lone row is canonical.
// The SELECT+UPDATEs run in one transaction so concurrent single-writer batches
// observe a consistent cluster. The UPDATE touches only superseded, leaving
// updated_at (the change-feed cursor) and content_hash untouched.
func reconcileContentKey(ctx context.Context, db *sql.DB, contentKey string) error {
	if contentKey == "" {
		return nil
	}

	rows, err := db.QueryContext(ctx, `SELECT event_id FROM events WHERE content_key = ?`, contentKey)
	if err != nil {
		return fmt.Errorf("dedup: load cluster %q: %w", contentKey, err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("dedup: scan cluster member: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("dedup: iterate cluster: %w", err)
	}
	rows.Close()

	if len(ids) == 0 {
		return nil
	}

	canonical := ids[0]
	for _, id := range ids[1:] {
		switch {
		case authorityRank(id) > authorityRank(canonical):
			canonical = id
		case authorityRank(id) == authorityRank(canonical) && id < canonical:
			canonical = id
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dedup: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, id := range ids {
		want := 1
		if id == canonical {
			want = 0
		}
		if _, err = tx.ExecContext(ctx,
			`UPDATE events SET superseded=? WHERE event_id=? AND superseded<>?`,
			want, id, want); err != nil {
			return fmt.Errorf("dedup: set superseded for %s: %w", id, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("dedup: commit cluster %q: %w", contentKey, err)
	}
	return nil
}
