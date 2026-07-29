package enrich

import "testing"

func TestExtractActionsFindsOfficialPageSignals(t *testing.T) {
	html := []byte(`
		<html><body>
			<a href="/register">사전등록</a>
			<a href="https://expo.example.com/exhibit/apply">Exhibitor application</a>
			<a href="/sponsor">스폰서 안내</a>
			<a href="/program">컨퍼런스 프로그램</a>
			<p>입장료 무료, 일부 워크숍 유료</p>
			<p>참가 신청 마감: 2026.07.31</p>
			<p>부스 신청 마감: 2026-08-15</p>
		</body></html>`)

	signals, err := ExtractActions("https://expo.example.com/event/", html)
	if err != nil {
		t.Fatalf("ExtractActions: %v", err)
	}

	if signals.CanRegister == nil || !*signals.CanRegister {
		t.Fatalf("CanRegister = %v, want true", signals.CanRegister)
	}
	if signals.RegisterURL == nil || *signals.RegisterURL != "https://expo.example.com/register" {
		t.Fatalf("RegisterURL = %v", signals.RegisterURL)
	}
	if signals.CanExhibit == nil || !*signals.CanExhibit {
		t.Fatalf("CanExhibit = %v, want true", signals.CanExhibit)
	}
	if signals.ExhibitURL == nil || *signals.ExhibitURL != "https://expo.example.com/exhibit/apply" {
		t.Fatalf("ExhibitURL = %v", signals.ExhibitURL)
	}
	if signals.CanSponsor == nil || !*signals.CanSponsor {
		t.Fatalf("CanSponsor = %v, want true", signals.CanSponsor)
	}
	if signals.HasStartupProgram != nil {
		t.Fatalf("HasStartupProgram = %v, want unknown", signals.HasStartupProgram)
	}
	if signals.CostHint == nil || *signals.CostHint != "mixed" {
		t.Fatalf("CostHint = %v, want mixed", signals.CostHint)
	}
	if signals.RegistrationDeadline == nil || *signals.RegistrationDeadline != "2026.07.31" {
		t.Fatalf("RegistrationDeadline = %v, want 2026.07.31", signals.RegistrationDeadline)
	}
	if signals.ExhibitorDeadline == nil || *signals.ExhibitorDeadline != "2026-08-15" {
		t.Fatalf("ExhibitorDeadline = %v, want 2026-08-15", signals.ExhibitorDeadline)
	}
}

func TestExtractActionsRejectsUnsafeLinks(t *testing.T) {
	html := []byte(`<a href="javascript:alert(1)">등록하기</a>`)

	signals, err := ExtractActions("https://expo.example.com/event/", html)
	if err != nil {
		t.Fatalf("ExtractActions: %v", err)
	}

	if signals.CanRegister != nil {
		t.Fatalf("CanRegister = %v, want unknown because link was unsafe", signals.CanRegister)
	}
	if signals.RegisterURL != nil {
		t.Fatalf("RegisterURL = %v, want nil", signals.RegisterURL)
	}
}

func TestDeadlineNear_KoreanYearMonthDayShape(t *testing.T) {
	html := []byte(`<html><body><p>참가 신청 마감: 2026년 9월 1일</p></body></html>`)
	signals, err := ExtractActions("https://expo.example.com/", html)
	if err != nil {
		t.Fatalf("ExtractActions: %v", err)
	}
	if signals.RegistrationDeadline == nil || *signals.RegistrationDeadline != "2026년 9월 1일" {
		t.Fatalf("RegistrationDeadline = %v, want 2026년 9월 1일", signals.RegistrationDeadline)
	}
}

func TestDeadlineNear_LabelVariantsAfterOnly(t *testing.T) {
	// Dates before a label are deliberately NOT extracted: goquery joins text
	// nodes without whitespace, so a preceding fact's date can touch the
	// label and a before-window would steal it (wrong deadline > no deadline).
	html := []byte(`<html><body>
		<p>2026.08.20 사전등록 마감</p>
		<p>부스마감 2026/08/15</p>
	</body></html>`)
	signals, err := ExtractActions("https://expo.example.com/", html)
	if err != nil {
		t.Fatalf("ExtractActions: %v", err)
	}
	if signals.RegistrationDeadline != nil {
		t.Fatalf("RegistrationDeadline = %v, want nil (before-label date must not be trusted)", *signals.RegistrationDeadline)
	}
	if signals.ExhibitorDeadline == nil || *signals.ExhibitorDeadline != "2026/08/15" {
		t.Fatalf("ExhibitorDeadline = %v, want 2026/08/15 (부스마감 variant)", signals.ExhibitorDeadline)
	}
}

func TestDeadlineNear_NoticeDateNotMistakenForDeadline(t *testing.T) {
	// Real false-positive shape from dong-afairs.co.kr: a notice title with a
	// posting date but no 마감 label. Must stay nil.
	html := []byte(`<html><body><p>동아전람 무료관람신청 2026-06-18 공지</p></body></html>`)
	signals, err := ExtractActions("https://expo.example.com/", html)
	if err != nil {
		t.Fatalf("ExtractActions: %v", err)
	}
	if signals.RegistrationDeadline != nil {
		t.Fatalf("RegistrationDeadline = %v, want nil (notice date is not a deadline)", *signals.RegistrationDeadline)
	}
}

func TestDeadlineOnActionPage_RangeEndOnRegistrationContextPage(t *testing.T) {
	// Real shape from the kofurn registration page: a 기간 label with a
	// range-end date. Legitimate only because the caller fetched a
	// registration/exhibit URL, i.e. the page context is enrollment.
	html := []byte(`<html><body><p>무료 사전등록 가능 기간 ~2026.08.26(수) 23시 59분</p></body></html>`)
	got := DeadlineOnActionPage(html, RegisterPage)
	if got == nil || *got != "2026.08.26" {
		t.Fatalf("DeadlineOnActionPage = %v, want 2026.08.26", got)
	}
}

func TestDeadlineOnActionPage_ExplicitDeadlineLabelWins(t *testing.T) {
	html := []byte(`<html><body>
		<p>행사 기간 2026.10.21 ~ 2026.10.23</p>
		<p>신청 마감 2026년 8월 31일</p>
	</body></html>`)
	got := DeadlineOnActionPage(html, RegisterPage)
	if got == nil || *got != "2026년 8월 31일" {
		t.Fatalf("DeadlineOnActionPage = %v, want explicit 마감 label to win", got)
	}
}

func TestDeadlineOnActionPage_EventPeriodAloneIsNotADeadline(t *testing.T) {
	// 행사/전시 기간 is the event run, not an application window.
	html := []byte(`<html><body><p>전시 기간: 2026.10.21 ~ 2026.10.23</p><p>오시는 길</p></body></html>`)
	if got := DeadlineOnActionPage(html, RegisterPage); got != nil {
		t.Fatalf("DeadlineOnActionPage = %v, want nil for bare event period", *got)
	}
}

func TestDeadlineOnActionPage_ExhibitKindIgnoresVisitorRegistration(t *testing.T) {
	// On an exhibit page, 사전등록 belongs to visitors — it must not become
	// the exhibitor deadline (wrong_type guard).
	html := []byte(`<html><body><p>사전등록 마감 2026.08.20</p></body></html>`)
	if got := DeadlineOnActionPage(html, ExhibitPage); got != nil {
		t.Fatalf("DeadlineOnActionPage(exhibit) = %v, want nil for visitor-registration label", *got)
	}
	html2 := []byte(`<html><body><p>부스 신청 마감 2026.08.20</p></body></html>`)
	if got := DeadlineOnActionPage(html2, ExhibitPage); got == nil || *got != "2026.08.20" {
		t.Fatalf("DeadlineOnActionPage(exhibit) = %v, want 2026.08.20", got)
	}
}

func TestDeadlineOnActionPage_ExhibitRecruitmentWindow(t *testing.T) {
	// Given
	html := []byte(`<html><body><p>참가업체 모집은 2026년 7월 31(금)까지 입니다.</p></body></html>`)

	// When
	got := DeadlineOnActionPage(html, ExhibitPage)

	// Then
	if got == nil || *got != "2026년 7월 31일" {
		t.Fatalf("DeadlineOnActionPage(exhibit) = %v, want 2026년 7월 31일", got)
	}
}
