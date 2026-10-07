package api

import (
	"fmt"
	"testing"
	"time"
)

func TestQuotaCapacityAndExpiration(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	q := &quotaMiddleware{cfg: MiddlewareConfig{PerDay: 1, MaxTrackedClients: 10000}.withDefaults(), clients: map[string]*clientBuckets{}}
	for i := 0; i < 10000; i++ {
		cb := q.bucketFor(fmt.Sprint(i), now)
		if cb == nil {
			t.Fatal("premature capacity refusal")
		}
		cb.day.allow(now)
	}
	if q.bucketFor("overflow", now) != nil || len(q.clients) != 10000 {
		t.Fatal("identity map exceeded its bound")
	}
	later := now.Add(2 * time.Hour)
	cb := q.bucketFor("0", later)
	if ok, _ := cb.day.allow(later); ok {
		t.Fatal("idle cleanup replenished live daily quota")
	}
	if len(q.clients) != 10000 {
		t.Fatal("idle cleanup evicted live identities")
	}
	// At exactly 24h these inactive windows have all expired, so cleanup may
	// free capacity without discarding any live daily budget.
	cb = q.bucketFor("fresh", now.Add(24*time.Hour))
	if cb == nil {
		t.Fatal("expired identities did not free capacity")
	}
	if ok, _ := cb.day.allow(now.Add(24 * time.Hour)); !ok {
		t.Fatal("new day refused")
	}
}
