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
	// YieldTerminalMalformedAction ends a run whose model kept violating the
	// action contract: an unparseable action, a search with no search budget
	// left, or an action that is not currently offered — twice in a row, the
	// second time after an explicit correction.
	YieldTerminalMalformedAction  YieldTerminalReason = "malformed_action"
	YieldTerminalModelCallBudget  YieldTerminalReason = "model_call_budget"
	YieldTerminalTokenBudget      YieldTerminalReason = "token_budget"
	YieldTerminalRoundLimit       YieldTerminalReason = "round_limit"
	YieldTerminalContextCanceled  YieldTerminalReason = "context_canceled"
	YieldTerminalDeadlineExceeded YieldTerminalReason = "deadline_exceeded"
	YieldTerminalInvalidUsage     YieldTerminalReason = "invalid_usage"
	YieldTerminalNone             YieldTerminalReason = "none"
)

// PrefilterReason identifies why one search result was discarded before the
// model saw it. Exactly one reason is recorded per dropped result.
type PrefilterReason string

const (
	PrefilterInvalidURL      PrefilterReason = "invalid_url"
	PrefilterURLPattern      PrefilterReason = "url_pattern"
	PrefilterMissingTitle    PrefilterReason = "missing_title"
	PrefilterMissingLocation PrefilterReason = "missing_location"
	PrefilterMissingDate     PrefilterReason = "missing_date"
	PrefilterPastDate        PrefilterReason = "past_date"
)

// PrefilterReasons breaks the prefilter total into fixed, count-only
// categories. A bare total cannot distinguish a crawler that yields untitled
// candidates from a profile whose URL pattern excludes them, so the two look
// identical in a zero-source run. The key set is fixed so consumers can
// validate it strictly.
type PrefilterReasons struct {
	InvalidURL      int `json:"invalid_url"`
	URLPattern      int `json:"url_pattern"`
	MissingTitle    int `json:"missing_title"`
	MissingLocation int `json:"missing_location"`
	MissingDate     int `json:"missing_date"`
	PastDate        int `json:"past_date"`
}

// Total returns the attributed drop count. It equals YieldTrace.PrefilterDropped.
func (reasons PrefilterReasons) Total() int {
	return reasons.InvalidURL + reasons.URLPattern + reasons.MissingTitle +
		reasons.MissingLocation + reasons.MissingDate + reasons.PastDate
}

func (reasons *PrefilterReasons) add(reason PrefilterReason) {
	switch reason {
	case PrefilterInvalidURL:
		reasons.InvalidURL++
	case PrefilterURLPattern:
		reasons.URLPattern++
	case PrefilterMissingTitle:
		reasons.MissingTitle++
	case PrefilterMissingLocation:
		reasons.MissingLocation++
	case PrefilterMissingDate:
		reasons.MissingDate++
	case PrefilterPastDate:
		reasons.PastDate++
	}
}

// YieldTrace contains counts only. It must never contain candidate data or request content.
type YieldTrace struct {
	Outcome          YieldOutcome        `json:"outcome"`
	TerminalReason   YieldTerminalReason `json:"terminal_reason"`
	CrawlerValidated int                 `json:"crawler_validated"`
	Offered          int                 `json:"offered"`
	PrefilterDropped int                 `json:"prefilter_dropped"`
	PrefilterReasons PrefilterReasons    `json:"prefilter_reasons"`
	ProposalCalls    int                 `json:"proposal_calls"`
	JudgeCalls       int                 `json:"judge_calls"`
	// OpenCalls counts the page fetches actually attempted through the
	// PageOpener, failures included. A refused open never reaches the network,
	// so it is not counted here.
	OpenCalls           int `json:"open_calls"`
	JudgeEntriesParsed  int `json:"judge_entries_parsed"`
	JudgeEntriesDropped int `json:"judge_entries_dropped"`
	Accepted            int `json:"accepted"`
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
