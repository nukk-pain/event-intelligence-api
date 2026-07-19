package fetch

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// CrawlBudgetLimits bounds one caller-owned public crawl job.
type CrawlBudgetLimits struct {
	MaxTransportAttempts  int64
	MaxAggregateBodyBytes int64
}

// CrawlBudgetUsage is an atomic snapshot suitable for result metadata.
type CrawlBudgetUsage struct {
	TransportAttempts     int64
	AggregateBodyBytes    int64
	MaxTransportAttempts  int64
	MaxAggregateBodyBytes int64
}

type crawlBudgetState struct {
	limits             CrawlBudgetLimits
	transportAttempts  atomic.Int64
	aggregateBodyBytes atomic.Int64
}

// CrawlBudget is a copy-safe handle to request-local accounting shared by every
// HTTP client and Fetcher participating in the same crawl. It has no reset.
type CrawlBudget struct {
	state *crawlBudgetState
}

// NewCrawlBudget validates and creates an empty request-local budget.
func NewCrawlBudget(limits CrawlBudgetLimits) (*CrawlBudget, error) {
	if limits.MaxTransportAttempts <= 0 || limits.MaxAggregateBodyBytes <= 0 {
		return nil, ErrInvalidCrawlBudget
	}
	return newCrawlBudget(limits), nil
}

// NewPublicCrawlBudget returns the plan's fixed anonymous-discovery defaults.
func NewPublicCrawlBudget() *CrawlBudget {
	return newCrawlBudget(CrawlBudgetLimits{
		MaxTransportAttempts:  defaultPublicMaxTransportAttempts,
		MaxAggregateBodyBytes: defaultPublicMaxAggregateBodyBytes,
	})
}

func newCrawlBudget(limits CrawlBudgetLimits) *CrawlBudget {
	return &CrawlBudget{state: &crawlBudgetState{limits: limits}}
}

func (b *CrawlBudget) valid() bool {
	return b != nil && b.state != nil
}

// Usage returns a race-safe point-in-time accounting snapshot.
func (b *CrawlBudget) Usage() CrawlBudgetUsage {
	if !b.valid() {
		return CrawlBudgetUsage{}
	}
	state := b.state
	return CrawlBudgetUsage{
		TransportAttempts:     state.transportAttempts.Load(),
		AggregateBodyBytes:    state.aggregateBodyBytes.Load(),
		MaxTransportAttempts:  state.limits.MaxTransportAttempts,
		MaxAggregateBodyBytes: state.limits.MaxAggregateBodyBytes,
	}
}

func (b *CrawlBudget) claimTransportAttempt() error {
	state := b.state
	for {
		used := state.transportAttempts.Load()
		if used >= state.limits.MaxTransportAttempts {
			return ErrTransportBudgetExhausted
		}
		if state.transportAttempts.CompareAndSwap(used, used+1) {
			return nil
		}
	}
}

func (b *CrawlBudget) reserveBodyBytes(requested int64) (int64, error) {
	state := b.state
	for {
		used := state.aggregateBodyBytes.Load()
		remaining := state.limits.MaxAggregateBodyBytes - used
		if remaining <= 0 {
			return 0, ErrAggregateBodyBudgetExhausted
		}
		reserved := min(requested, remaining)
		if state.aggregateBodyBytes.CompareAndSwap(used, used+reserved) {
			return reserved, nil
		}
	}
}

type budgetReadCloser struct {
	io.ReadCloser
	budget *CrawlBudget
}

func (r *budgetReadCloser) Read(p []byte) (int, error) {
	return readWithBudget(r.ReadCloser, r.budget, p)
}

type budgetReader struct {
	io.Reader
	budget *CrawlBudget
}

func (r *budgetReader) Read(p []byte) (int, error) {
	return readWithBudget(r.Reader, r.budget, p)
}

func readWithBudget(reader io.Reader, budget *CrawlBudget, p []byte) (int, error) {
	if len(p) == 0 {
		return reader.Read(p)
	}
	reserved, err := budget.reserveBodyBytes(int64(len(p)))
	if err != nil {
		return 0, err
	}
	n, readErr := reader.Read(p[:reserved])
	if unused := reserved - int64(n); unused > 0 {
		budget.state.aggregateBodyBytes.Add(-unused)
	}
	return n, readErr
}

type budgetRoundTripper struct {
	base   http.RoundTripper
	budget *CrawlBudget
}

func (t *budgetRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.budget.claimTransportAttempt(); err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("fetch: transport returned nil body")
	}
	resp.Body = &budgetReadCloser{ReadCloser: resp.Body, budget: t.budget}
	return resp, nil
}
