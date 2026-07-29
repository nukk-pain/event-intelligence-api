package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/pipeline"
	"github.com/smpain/event-intelligence-api/internal/solarenrich"
)

type mainTestTextSelector struct {
	body  []byte
	calls int
}

func (s *mainTestTextSelector) Text(context.Context, string, []byte) ([]byte, error) {
	s.calls++
	return s.body, nil
}

func TestReadOfficialPageText_UsesRenderedDOMForSolarEvidence(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div id="root"></div></body></html>`))
	}))
	t.Cleanup(server.Close)
	officialFetcher, err := fetch.NewFetcher(fetch.WithAnyPublicHost(true), fetch.WithAllowLoopback(true))
	if err != nil {
		t.Fatalf("NewFetcher: %v", err)
	}
	selector := &mainTestTextSelector{body: []byte(`<html><body><p>사전등록 마감 2026.08.26</p></body></html>`)}

	// When
	body, err := readOfficialPageText(context.Background(), officialFetcher, selector, server.URL+"#/registration")

	// Then
	if err != nil {
		t.Fatalf("readOfficialPageText: %v", err)
	}
	if !strings.Contains(body, "사전등록 마감 2026.08.26") {
		t.Fatalf("page body = %q, want rendered deadline for Solar readTexts", body)
	}
	if selector.calls != 1 {
		t.Fatalf("selector calls = %d, want 1", selector.calls)
	}
}

func TestIngestChangedData(t *testing.T) {
	cases := []struct {
		name string
		rep  pipeline.Report
		want bool
	}{
		{
			name: "stored events -> purge",
			rep:  pipeline.Report{Sources: []pipeline.SourceReport{{Source: "coex", Stored: 3}}},
			want: true,
		},
		{
			name: "one source stored, another aborted -> purge",
			rep: pipeline.Report{Sources: []pipeline.SourceReport{
				{Source: "coex", Stored: 0, Aborted: true},
				{Source: "kintex", Stored: 2},
			}},
			want: true,
		},
		{
			name: "nothing stored -> no purge",
			rep:  pipeline.Report{Sources: []pipeline.SourceReport{{Source: "coex", Stored: 0}}},
			want: false,
		},
		{
			name: "all aborted -> no purge",
			rep: pipeline.Report{Sources: []pipeline.SourceReport{
				{Source: "coex", Aborted: true},
				{Source: "kintex", Aborted: true},
			}},
			want: false,
		},
		{
			name: "empty report -> no purge",
			rep:  pipeline.Report{},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ingestChangedData(c.rep); got != c.want {
				t.Errorf("ingestChangedData = %v, want %v", got, c.want)
			}
		})
	}
}

// The pipeline stays backend-agnostic, so it owns its own copy of the
// enrichment publisher string; this is the one package that knows both sides
// and may pin their equality.
func TestEnrichmentPublisherStringsAgree(t *testing.T) {
	if pipeline.EnrichmentPublisher != solarenrich.ProvenancePublisher {
		t.Fatalf("pipeline %q != solarenrich %q",
			pipeline.EnrichmentPublisher, solarenrich.ProvenancePublisher)
	}
}
