package solarenrich

import "testing"

func TestNormalizeDeadline(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"2026-09-01", "2026-09-01", true},
		{"2026년 9월 1일", "2026-09-01", true},
		{"2026년 9월 1일까지", "2026-09-01", true},
		{"2026.9.1", "2026-09-01", true},
		{"2026/09/01", "2026-09-01", true},
		{"등록 마감 2026년 12월 31일", "2026-12-31", true},
		{"선착순 마감", "", false},
		{"예산 소진 시까지", "", false},
		{"9월 1일", "", false},
		{"1999년 9월 1일", "", false},
		{"2026년 13월 1일", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := normalizeDeadline(tt.raw)
		if ok != tt.ok || got != tt.want {
			t.Errorf("normalizeDeadline(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}
