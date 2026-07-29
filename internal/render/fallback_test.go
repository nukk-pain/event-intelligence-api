package render

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRenderer struct {
	body  []byte
	err   error
	calls int
}

func (r *fakeRenderer) Render(context.Context, string, []byte) ([]byte, error) {
	r.calls++
	return r.body, r.err
}

type fakeResourceFetcher struct {
	resources map[string]Resource
	calls     []string
}

func (f *fakeResourceFetcher) Fetch(_ context.Context, rawURL string) (Resource, error) {
	f.calls = append(f.calls, rawURL)
	resource, ok := f.resources[rawURL]
	if !ok {
		return Resource{}, fmt.Errorf("unexpected resource URL: %s", rawURL)
	}
	return resource, nil
}

func TestFallback_TextUsesRenderedDOMForJSShell(t *testing.T) {
	// Given
	renderer := &fakeRenderer{body: []byte(`<html><body><p>사전등록 마감 2026.08.26</p></body></html>`)}
	fallback, err := NewWithRenderer(Config{UserAgent: "eventsintel-test", MaxPages: 1, Timeout: time.Second}, renderer)
	if err != nil {
		t.Fatalf("NewWithRenderer: %v", err)
	}
	static := []byte(`<html><body><div id="root"></div><noscript>Enable JavaScript</noscript></body></html>`)

	// When
	got, err := fallback.Text(context.Background(), "https://events.example/#/registration", static)

	// Then
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if !strings.Contains(string(got), "사전등록 마감 2026.08.26") {
		t.Fatalf("Text = %q, want the rendered deadline", got)
	}
	if renderer.calls != 1 {
		t.Fatalf("renderer calls = %d, want 1", renderer.calls)
	}
}

func TestFallback_TextLimitsRenderedPages(t *testing.T) {
	// Given
	renderer := &fakeRenderer{body: []byte(`<html><body>rendered</body></html>`)}
	fallback, err := NewWithRenderer(Config{UserAgent: "eventsintel-test", MaxPages: 2, Timeout: time.Second}, renderer)
	if err != nil {
		t.Fatalf("NewWithRenderer: %v", err)
	}
	static := []byte(`<html><body><div id="app"></div></body></html>`)

	// When
	for i := range 3 {
		got, textErr := fallback.Text(context.Background(), "https://events.example/page/"+string(rune('a'+i)), static)
		if textErr != nil {
			t.Fatalf("Text(%d): %v", i, textErr)
		}
		if i < 2 && !strings.Contains(string(got), "rendered") {
			t.Fatalf("Text(%d) = %q, want rendered body", i, got)
		}
		if i == 2 && string(got) != string(static) {
			t.Fatalf("Text(%d) = %q, want static body after the cap", i, got)
		}
	}

	// Then
	if renderer.calls != 2 {
		t.Fatalf("renderer calls = %d, want cap 2", renderer.calls)
	}
}

func TestFallback_TextKeepsStaticTextWhenRendererFails(t *testing.T) {
	// Given
	renderer := &fakeRenderer{err: errors.New("Chrome exited")}
	fallback, err := NewWithRenderer(Config{UserAgent: "eventsintel-test", MaxPages: 1, Timeout: time.Second}, renderer)
	if err != nil {
		t.Fatalf("NewWithRenderer: %v", err)
	}
	static := []byte(`<html><body><a href="/register">사전등록</a></body></html>`)

	// When
	got, textErr := fallback.Text(context.Background(), "https://events.example/#/registration", static)

	// Then
	if textErr == nil {
		t.Fatal("Text error = nil, want renderer failure reported")
	}
	if string(got) != string(static) {
		t.Fatalf("Text = %q, want the original static body", got)
	}
}

func TestFallback_TextIgnoresAnalyticsNoscript(t *testing.T) {
	// Given
	renderer := &fakeRenderer{body: []byte(`<html><body>rendered</body></html>`)}
	fallback, err := NewWithRenderer(Config{UserAgent: "eventsintel-test", MaxPages: 1, Timeout: time.Second}, renderer)
	if err != nil {
		t.Fatalf("NewWithRenderer: %v", err)
	}
	static := []byte(`<html><body><p>` + strings.Repeat("static event information ", 30) + `</p><noscript><iframe src="https://www.googletagmanager.com/ns.html"></iframe></noscript></body></html>`)

	// When
	got, err := fallback.Text(context.Background(), "https://events.example/about", static)

	// Then
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if string(got) != string(static) {
		t.Fatalf("Text = %q, want static page", got)
	}
	if renderer.calls != 0 {
		t.Fatalf("renderer calls = %d, want 0", renderer.calls)
	}
}

func TestChromeRenderer_RendersJavaScriptDeadline(t *testing.T) {
	// Given
	resourceFetcher := &fakeResourceFetcher{resources: map[string]Resource{
		"https://events.example/assets/deadline.js": {
			ContentType: "application/javascript",
			Body:        []byte(`if (window.location.hash === "#/pre-reg/891") { document.getElementById("root").innerHTML = "<p>사전등록 마감 2026.08.26</p>" } else { document.getElementById("root").textContent = "wrong route" }`),
		},
	}}
	fallback, err := New(context.Background(), Config{UserAgent: "eventsintel-test", MaxPages: 1, Timeout: 15 * time.Second, ResourceFetcher: resourceFetcher})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(fallback.Close)

	// When
	got, err := fallback.Text(context.Background(), "https://events.example/registration/#/pre-reg/891", []byte(`<html><head><link rel="icon" href="data:,"></head><body><a href="https://events.example/register">사전등록</a><div id="root">loading</div><script src="https://events.example/assets/deadline.js"></script></body></html>`))

	// Then
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if !strings.Contains(string(got), "사전등록 마감 2026.08.26") {
		t.Fatalf("rendered DOM = %q, want JavaScript-injected deadline", got)
	}
	if !strings.Contains(string(got), `href="https://events.example/register"`) {
		t.Fatalf("rendered DOM = %q, want original action URL", got)
	}
	if len(resourceFetcher.calls) != 1 || resourceFetcher.calls[0] != "https://events.example/assets/deadline.js" {
		t.Fatalf("resource fetches = %v, want only the source-approved script", resourceFetcher.calls)
	}
}

func TestChromeRenderer_BlocksDirectBrowserEgress(t *testing.T) {
	// Given
	var externalHits atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		externalHits.Add(1)
	}))
	t.Cleanup(external.Close)
	fallback, err := New(context.Background(), Config{UserAgent: "eventsintel-test", MaxPages: 1, Timeout: 15 * time.Second, ResourceFetcher: &fakeResourceFetcher{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(fallback.Close)
	static := []byte(`<html><head><link rel="icon" href="data:,"></head><body><div id="root">loading</div><script>fetch("` + external.URL + `").then(function () { document.getElementById("root").textContent = "leaked" }).catch(function () { document.getElementById("root").textContent = "blocked" })</script></body></html>`)

	// When
	got, err := fallback.Text(context.Background(), "https://events.example/#/registration", static)

	// Then
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if externalHits.Load() != 0 {
		t.Fatalf("external browser hits = %d, want 0", externalHits.Load())
	}
	if !strings.Contains(string(got), ">blocked<") {
		t.Fatalf("rendered DOM = %q, want blocked direct egress", got)
	}
}

func TestFallback_CloseIsSafeBeforeFirstRender(t *testing.T) {
	// Given
	fallback, err := New(context.Background(), Config{UserAgent: "eventsintel-test", MaxPages: 1, Timeout: time.Second, ResourceFetcher: &fakeResourceFetcher{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// When
	fallback.Close()
	fallback.Close()

	// Then
}
