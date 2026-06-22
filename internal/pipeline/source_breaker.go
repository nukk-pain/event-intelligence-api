package pipeline

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/smpain/event-intelligence-api/internal/model"
	"github.com/smpain/event-intelligence-api/internal/store"
)

func (p *Pipeline) shouldTrip(ctx context.Context, db *sql.DB, source string, discovered int, events []model.Event) (string, bool) {
	b := p.breaker
	if discovered == 0 {
		return "discovery returned 0 refs (floor); preserving prior rows", true
	}
	if b.AbsoluteFloor > 0 && discovered < b.AbsoluteFloor {
		return fmt.Sprintf("discovered %d < absolute floor %d", discovered, b.AbsoluteFloor), true
	}

	prev, found, err := store.LoadBatchStats(ctx, db, source)
	if err == nil && found && prev.Discovered > 0 && b.MinFloorFraction > 0 {
		floor := b.MinFloorFraction * float64(prev.Discovered)
		if float64(discovered) < floor {
			return fmt.Sprintf("discovered %d < %.0f%% of last successful %d",
				discovered, b.MinFloorFraction*100, prev.Discovered), true
		}
	}

	if b.MaxChangedFraction > 0 && len(events) > 0 {
		changed, total, prevRows, herr := p.changedFraction(ctx, db, events)
		if herr == nil && prevRows > 0 && total > 0 {
			frac := float64(changed) / float64(total)
			if frac > b.MaxChangedFraction {
				return fmt.Sprintf("changed fraction %.2f > threshold %.2f (parser regression guard)",
					frac, b.MaxChangedFraction), true
			}
		}
	}

	return "", false
}

func (p *Pipeline) changedFraction(ctx context.Context, db *sql.DB, events []model.Event) (changed, total, prevRows int, err error) {
	for i := range events {
		e := events[i]
		var stored string
		row := db.QueryRowContext(ctx, `SELECT content_hash FROM events WHERE event_id=?`, e.EventID)
		serr := row.Scan(&stored)
		if serr == sql.ErrNoRows {
			total++
			continue
		}
		if serr != nil {
			return 0, 0, 0, serr
		}
		prevRows++
		total++
		if store.ContentHash(e) != stored {
			changed++
		}
	}
	return changed, total, prevRows, nil
}
