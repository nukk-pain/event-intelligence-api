package pipeline

import (
	"context"
	"fmt"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/enrich"
)

type testTextSelector struct {
	body  []byte
	err   error
	calls int
}

func (s *testTextSelector) Text(context.Context, string, []byte) ([]byte, error) {
	s.calls++
	return s.body, s.err
}

func TestOfficialPageText_UsesRenderedDOMForJSShell(t *testing.T) {
	// Given
	selector := &testTextSelector{body: []byte(`<html><body><a href="/register">사전등록</a><p>사전등록 마감 2026.08.26</p></body></html>`)}
	p := New("batch-rendered-home").WithTextSelector(selector)
	static := []byte(`<html><body><div id="root"></div><noscript>Enable JavaScript</noscript></body></html>`)

	// When
	body := p.officialPageText(context.Background(), "https://events.example/#/registration", static)
	signals, err := enrich.ExtractActions("https://events.example/#/registration", body)

	// Then
	if err != nil {
		t.Fatalf("ExtractActions: %v", err)
	}
	if signals.RegisterURL == nil || *signals.RegisterURL != "https://events.example/register" {
		t.Fatalf("register URL = %v, want rendered action link", signals.RegisterURL)
	}
	if signals.RegistrationDeadline == nil || *signals.RegistrationDeadline != "2026.08.26" {
		t.Fatalf("registration deadline = %v, want rendered deadline", signals.RegistrationDeadline)
	}
	if deadline := enrich.DeadlineOnActionPage(body, enrich.RegisterPage); deadline == nil || *deadline != "2026.08.26" {
		t.Fatalf("DeadlineOnActionPage = %v, want rendered deadline", deadline)
	}
	if selector.calls != 1 {
		t.Fatalf("selector calls = %d, want 1", selector.calls)
	}
}

func TestOfficialPageText_KeepsStaticTextWhenRenderFails(t *testing.T) {
	// Given
	static := []byte(`<html><body><a href="/register">사전등록</a></body></html>`)
	p := New("batch-render-failure").WithTextSelector(&testTextSelector{body: static, err: fmt.Errorf("Chrome exited")})

	// When
	body := p.officialPageText(context.Background(), "https://events.example/#/registration", static)
	signals, err := enrich.ExtractActions("https://events.example/#/registration", body)

	// Then
	if err != nil {
		t.Fatalf("ExtractActions: %v", err)
	}
	if signals.RegisterURL == nil || *signals.RegisterURL != "https://events.example/register" {
		t.Fatalf("register URL = %v, want static fallback retained", signals.RegisterURL)
	}
}
