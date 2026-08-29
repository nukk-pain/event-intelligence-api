package store

import (
	"testing"
	"time"

	"github.com/smpain/event-intelligence-api/internal/model"
)

func TestEffectiveStatusAt(t *testing.T) {
	ptr := func(s string) *string { return &s }
	tz := ptr("Asia/Seoul")
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		e    model.Event
		want string
	}{
		{"past range", model.Event{Status: "scheduled", EndDate: ptr("2026-08-28"), Timezone: tz}, "ended"},
		{"single day fallback", model.Event{Status: "scheduled", StartDate: ptr("2026-08-28"), Timezone: tz}, "ended"},
		{"end day remains active", model.Event{Status: "scheduled", EndDate: ptr("2026-08-29"), Timezone: tz}, "scheduled"},
		{"future", model.Event{Status: "scheduled", EndDate: ptr("2026-08-30"), Timezone: tz}, "scheduled"},
		{"undated", model.Event{Status: "scheduled", Timezone: tz}, "scheduled"},
		{"explicit cancellation wins", model.Event{Status: "cancelled", EndDate: ptr("2026-08-28"), Timezone: tz}, "cancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveStatusAt(tt.e, now); got != tt.want {
				t.Fatalf("effectiveStatusAt() = %q, want %q", got, tt.want)
			}
		})
	}
}
