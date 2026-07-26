package agent

import (
	"context"
	"errors"
)

// YieldOutcome classifies the furthest useful boundary reached by a completed discovery.
type YieldOutcome string

const (
	YieldOutcomeAccepted       YieldOutcome = "accepted"
	YieldOutcomeError          YieldOutcome = "error"
	YieldOutcomeBudgetStopped  YieldOutcome = "budget_stopped"
	YieldOutcomeCandidateEmpty YieldOutcome = "candidate_empty"
	YieldOutcomeOfferedEmpty   YieldOutcome = "offered_empty"
	YieldOutcomeJudgeEmpty     YieldOutcome = "judge_empty"
)

// YieldTerminalReason identifies the request-local branch that ended discovery.
type YieldTerminalReason string

const (
	YieldTerminalProposalDone           YieldTerminalReason = "proposal_done"
	YieldTerminalProposalError          YieldTerminalReason = "proposal_error"
	YieldTerminalSearchError            YieldTerminalReason = "search_error"
	YieldTerminalCandidateEncodeError   YieldTerminalReason = "candidate_encode_error"
	YieldTerminalJudgeError             YieldTerminalReason = "judge_error"
	YieldTerminalMalformedJudgeEnvelope YieldTerminalReason = "malformed_judge_envelope"
	YieldTerminalModelCallBudget        YieldTerminalReason = "model_call_budget"
	YieldTerminalTokenBudget            YieldTerminalReason = "token_budget"
	YieldTerminalRoundLimit             YieldTerminalReason = "round_limit"
	YieldTerminalContextCanceled        YieldTerminalReason = "context_canceled"
	YieldTerminalDeadlineExceeded       YieldTerminalReason = "deadline_exceeded"
	YieldTerminalInvalidUsage           YieldTerminalReason = "invalid_usage"
	YieldTerminalNone                   YieldTerminalReason = "none"
)

// YieldTrace contains counts only. It must never contain candidate data or request content.
type YieldTrace struct {
	Outcome             YieldOutcome        `json:"outcome"`
	TerminalReason      YieldTerminalReason `json:"terminal_reason"`
	CrawlerValidated    int                 `json:"crawler_validated"`
	Offered             int                 `json:"offered"`
	PrefilterDropped    int                 `json:"prefilter_dropped"`
	ProposalCalls       int                 `json:"proposal_calls"`
	JudgeCalls          int                 `json:"judge_calls"`
	JudgeEntriesParsed  int                 `json:"judge_entries_parsed"`
	JudgeEntriesDropped int                 `json:"judge_entries_dropped"`
	Accepted            int                 `json:"accepted"`
}

// WithCrawlerValidated merges the orchestration-owned validated candidate count.
func (trace YieldTrace) WithCrawlerValidated(count int) YieldTrace {
	trace.CrawlerValidated = count
	trace.setOutcome(trace.Outcome == YieldOutcomeError)
	return trace
}

func (trace *YieldTrace) finish(reason YieldTerminalReason, completedWithError bool) {
	trace.TerminalReason = reason
	trace.setOutcome(completedWithError)
}

func (trace *YieldTrace) setOutcome(completedWithError bool) {
	switch {
	case trace.Accepted > 0:
		trace.Outcome = YieldOutcomeAccepted
	case completedWithError:
		trace.Outcome = YieldOutcomeError
	case trace.TerminalReason == YieldTerminalModelCallBudget ||
		trace.TerminalReason == YieldTerminalTokenBudget || trace.TerminalReason == YieldTerminalRoundLimit:
		trace.Outcome = YieldOutcomeBudgetStopped
	case trace.CrawlerValidated == 0 && trace.Offered == 0 && trace.PrefilterDropped == 0:
		trace.Outcome = YieldOutcomeCandidateEmpty
	case trace.Offered == 0:
		trace.Outcome = YieldOutcomeOfferedEmpty
	default:
		trace.Outcome = YieldOutcomeJudgeEmpty
	}
}

func terminalReasonForError(err error, fallback YieldTerminalReason) YieldTerminalReason {
	switch {
	case errors.Is(err, context.Canceled):
		return YieldTerminalContextCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return YieldTerminalDeadlineExceeded
	case errors.Is(err, ErrInvalidModelUsage):
		return YieldTerminalInvalidUsage
	default:
		return fallback
	}
}
