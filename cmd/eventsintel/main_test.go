package main

import (
	"testing"

	"github.com/smpain/event-intelligence-api/internal/pipeline"
	"github.com/smpain/event-intelligence-api/internal/solarenrich"
)

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
