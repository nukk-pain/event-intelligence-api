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
	body := p.officialPageText(context.Background(), "https://events.example/#/registration", static, true)
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
	body := p.officialPageText(context.Background(), "https://events.example/#/registration", static, true)
	signals, err := enrich.ExtractActions("https://events.example/#/registration", body)

	// Then
	if err != nil {
		t.Fatalf("ExtractActions: %v", err)
	}
	if signals.RegisterURL == nil || *signals.RegisterURL != "https://events.example/register" {
		t.Fatalf("register URL = %v, want static fallback retained", signals.RegisterURL)
	}
}

func TestOfficialPageText_IneligiblePageNeverReachesTheBrowser(t *testing.T) {
	// The render budget is the scarcest resource in a batch; a page that
	// cannot benefit must not consume a slot or appear in the browser's own
	// counters.
	selector := &testTextSelector{body: []byte(`<html><body>rendered</body></html>`)}
	p := New("batch-gated").WithTextSelector(selector)
	static := []byte(`<html><body><div id="root"></div></body></html>`)

	body := p.officialPageText(context.Background(), "https://events.example/#/x", static, false)

	if selector.calls != 0 {
		t.Fatalf("selector calls = %d, want 0 for an ineligible page", selector.calls)
	}
	if string(body) != string(static) {
		t.Fatal("an ineligible page must be parsed from its static body")
	}
	eligible, gated := p.RenderGateStats()
	if eligible != 0 || gated != 1 {
		t.Fatalf("stats eligible=%d gated=%d, want 0/1", eligible, gated)
	}
}
