package showala

import (
	"testing"
	"time"
)

func TestNewDefaultsAndID(t *testing.T) {
	s := New()
	if s.ID() != "showala" {
		t.Fatalf("ID() = %q, want showala", s.ID())
	}
	if s.listURL != listURL {
		t.Fatalf("default listURL = %q, want %q", s.listURL, listURL)
	}
	if s.procURL != procURL {
		t.Fatalf("default procURL = %q, want %q", s.procURL, procURL)
	}
	if s.now == nil {
		t.Fatal("default clock is nil")
	}
}

func TestOptionsOverride(t *testing.T) {
	fixed := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	s := New(
		WithListURL("http://x/list"),
		WithProcURL("http://x/proc"),
		WithClock(func() time.Time { return fixed }),
	)
	if s.listURL != "http://x/list" || s.procURL != "http://x/proc" {
		t.Fatalf("option override failed: list=%q proc=%q", s.listURL, s.procURL)
	}
	if !s.now().Equal(fixed) {
		t.Fatalf("clock override failed: %v", s.now())
	}
}
