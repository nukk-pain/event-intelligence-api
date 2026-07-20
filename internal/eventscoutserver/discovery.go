package eventscoutserver

import (
	"context"
	"fmt"

	"github.com/smpain/event-intelligence-api/internal/agent"
	"github.com/smpain/event-intelligence-api/internal/publicdiscovery"
)

type publicSearchTool interface {
	agent.SearchTool
	Snapshot() publicdiscovery.Result
	RestoreProvenance([]agent.DiscoveredSource) []agent.DiscoveredSource
}

type publicSearchToolFactory func() (publicSearchTool, error)

type SolarDiscoveryRunner struct {
	backend     agent.Backend
	toolFactory publicSearchToolFactory
	clock       Clock
}

func NewSolarDiscoveryRunner(backend agent.Backend) (*SolarDiscoveryRunner, error) {
	return newSolarDiscoveryRunner(backend, func() (publicSearchTool, error) {
		return publicdiscovery.NewAgentSearchTool()
	}, SystemClock{})
}

func newSolarDiscoveryRunner(
	backend agent.Backend,
	toolFactory publicSearchToolFactory,
	clock Clock,
) (*SolarDiscoveryRunner, error) {
	if err := validateSolarBackend(backend); err != nil {
		return nil, err
	}
	if toolFactory == nil || clock == nil {
		return nil, ErrInvalidHandlerConfig
	}
	return &SolarDiscoveryRunner{backend: backend, toolFactory: toolFactory, clock: clock}, nil
}

func (runner *SolarDiscoveryRunner) Discover(ctx context.Context, goal Goal) (DiscoveryOutput, error) {
	profile, err := agent.NamedDiscoveryProfile(agent.DiscoveryProfileEvents)
	if err != nil {
		return DiscoveryOutput{}, fmt.Errorf("event profile: %w", err)
	}
	tool, err := runner.toolFactory()
	if err != nil {
		return DiscoveryOutput{}, fmt.Errorf("public provider: %w", err)
	}
	options := agent.DefaultDiscoverOptions(profile)
	options.ReferenceTime = runner.clock.Now().UTC()
	result, err := agent.DiscoverWithOptions(ctx, agent.DiscoverRequest{
		Backend: runner.backend, Goal: goal.String(), Tool: tool, Options: options,
	})
	if err != nil {
		return DiscoveryOutput{}, fmt.Errorf("discover: %w", err)
	}
	result.Sources = tool.RestoreProvenance(result.Sources)
	reasons := mergeTruncationReasons(tool.Snapshot(), result.Metadata.TruncationReasons)
	return DiscoveryOutput{
		Sources: result.Sources, TruncationReasons: reasons, ModelCalls: result.Trace.Calls,
		PromptTokens: result.Trace.Usage.PromptTokens, CompletionTokens: result.Trace.Usage.CompletionTokens,
	}, nil
}

func mergeTruncationReasons(publicResult publicdiscovery.Result, agentReasons []agent.TruncationReason) []string {
	reasons := make([]string, 0, len(publicResult.Budget.TruncationReasons)+len(agentReasons))
	seen := make(map[string]struct{}, cap(reasons))
	for _, reason := range publicResult.Budget.TruncationReasons {
		reasons = appendUniqueReason(reasons, seen, string(reason))
	}
	for _, reason := range agentReasons {
		reasons = appendUniqueReason(reasons, seen, string(reason))
	}
	return reasons
}

func appendUniqueReason(reasons []string, seen map[string]struct{}, reason string) []string {
	if _, exists := seen[reason]; exists {
		return reasons
	}
	seen[reason] = struct{}{}
	return append(reasons, reason)
}
