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
