package benchmark

import (
	"context"

	"github.com/smpain/event-intelligence-api/internal/fetch"
	"github.com/smpain/event-intelligence-api/internal/sources"
)

type Source struct{}

func New() *Source { return &Source{} }

func (s *Source) ID() string { return sourceID }

func (s *Source) Discover(ctx context.Context, f *fetch.Fetcher) ([]sources.Ref, error) {
	return refs(), nil
}

var _ sources.Source = (*Source)(nil)
