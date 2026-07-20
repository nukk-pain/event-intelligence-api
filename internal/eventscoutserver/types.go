// Package eventscoutserver implements the isolated anonymous Solar discovery service.
package eventscoutserver

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

const (
	requestBodyLimit       = 4 << 10
	maximumGoalRunes       = 800
	tenMinuteRequestLimit  = 2
	dailyRequestLimit      = 24
	activeDiscoveryLimit   = 2
	maximumRetryAfter      = 600
	maximumRequestDeadline = time.Minute
)

var (
	ErrInvalidGoal          = errors.New("eventscout server: invalid goal")
	ErrInvalidHandlerConfig = errors.New("eventscout server: invalid handler config")
)

type Goal struct {
	text string
}

func NewGoal(raw string) (Goal, error) {
	trimmed := strings.TrimSpace(raw)
	runes := utf8.RuneCountInString(trimmed)
	if runes < 1 || runes > maximumGoalRunes {
		return Goal{}, ErrInvalidGoal
	}
	return Goal{text: trimmed}, nil
}

func (goal Goal) String() string {
	return goal.text
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now()
}

type DiscoveryRunner interface {
	Discover(ctx context.Context, goal Goal) (DiscoveryOutput, error)
}

// DiscoveryOutput deliberately excludes the goal, page content, and backend credentials.
type DiscoveryOutput struct {
	Sources           []agent.DiscoveredSource
	TruncationReasons []string
	ModelCalls        int
	PromptTokens      int
	CompletionTokens  int
}

// HandlerOptions contains server-owned controls. Callers cannot override them over HTTP.
type HandlerOptions struct {
	RequestTimeout time.Duration
	TrustedProxies []netip.Prefix
}

func DefaultHandlerOptions() HandlerOptions {
	return HandlerOptions{RequestTimeout: maximumRequestDeadline}
}

type HandlerDependencies struct {
	Runner DiscoveryRunner
	Clock  Clock
	Logger *slog.Logger
}

type discoverResponse struct {
	Sources []agent.DiscoveredSource `json:"sources"`
	Meta    discoverMeta             `json:"meta"`
}

type discoverMeta struct {
	Provider            string                     `json:"provider"`
	Profile             agent.DiscoveryProfileName `json:"profile"`
	TruncationReasons   []string                   `json:"truncation_reasons"`
	ModelCalls          int                        `json:"model_calls"`
	PromptTokens        int                        `json:"prompt_tokens"`
	CompletionTokens    int                        `json:"completion_tokens"`
	ElapsedMilliseconds int64                      `json:"elapsed_milliseconds"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty"`
}

type statusResponse struct {
	Status string `json:"status"`
}
