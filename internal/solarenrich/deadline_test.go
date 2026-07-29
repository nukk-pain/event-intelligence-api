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

func TestTypedDateEvidence_RejectsWrongTypeContext(t *testing.T) {
	// Real wrong_type cases from the 2026-07-29 audit.
	battery := "전시자는 참가신청서 제출시 참가비 30%를 계약금으로 납부하여야 하며, 2026년 10월 6일까지 잔금을 납부한다"
	if typedDateEvidence([]string{battery}, "2026-10-06", exhibitorDeadlineKind) {
		t.Error("fee-balance date without booth-application context must not become an exhibitor deadline")
	}
	hometable := "2026 부스 참가 문의(조기신청기간 ~7/31까지) 바로가기 2026.07.31 안내"
	if typedDateEvidence([]string{hometable}, "2026-07-31", registrationDeadlineKind) {
		t.Error("booth early-application date must not become a visitor registration deadline")
	}
	if !typedDateEvidence([]string{hometable}, "2026-07-31", exhibitorDeadlineKind) {
		t.Error("booth context date must count as exhibitor evidence")
	}
}

func TestTypedDateEvidence_AcceptsRightTypeContext(t *testing.T) {
	page := "사전등록 마감: 2026년 9월 1일. 오시는 길 안내."
	if !typedDateEvidence([]string{page}, "2026-09-01", registrationDeadlineKind) {
		t.Error("registration context + deadline word must count as evidence")
	}
	if typedDateEvidence([]string{page}, "2026-09-01", exhibitorDeadlineKind) {
		t.Error("registration-only context must not back an exhibitor deadline")
	}
}

func TestTypedDateEvidence_BareDateRejected(t *testing.T) {
	page := "행사 기간: 2026년 9월 1일 ~ 3일, 코엑스"
	if typedDateEvidence([]string{page}, "2026-09-01", registrationDeadlineKind) {
		t.Error("event-period date without deadline wording must not be evidence")
	}
}
