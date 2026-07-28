package agent

import (
	"errors"
	"fmt"
	"strings"
)

// errMalformedAction is the sentinel every parseActionResponse error wraps, so
// callers can identify a malformed model response via errors.Is regardless of
// which validation rule failed.
var errMalformedAction = errors.New("agent discovery: malformed action response")

// actionKind identifies the next step the model chose in the discovery loop.
type actionKind string

const (
	actionSearch actionKind = "search"
	actionOpen   actionKind = "open"
	actionAccept actionKind = "accept"
	actionDone   actionKind = "done"
)

// actionSelection is one accepted candidate URL from an "accept" action.
type actionSelection struct {
	URL    string
	Reason string
}

// discoveryAction is the parsed form of one model turn in the discovery action loop.
type discoveryAction struct {
	Kind       actionKind
	Query      string            // set when Kind == actionSearch
	URL        string            // set when Kind == actionOpen
	Selections []actionSelection // set when Kind == actionAccept
	// DroppedSelections counts accept entries the model sent that were
	// discarded here for a blank url, so the yield trace can still account
	// for every entry the model proposed.
	DroppedSelections int
}

// parseActionResponse decodes the model's last JSON object into a discoveryAction
// and validates it against the action loop's wire contract. Every returned error
// wraps errMalformedAction.
func parseActionResponse(content string) (discoveryAction, error) {
	var envelope struct {
		Action     string `json:"action"`
		Query      string `json:"query"`
		URL        string `json:"url"`
		Selections []struct {
			URL    string `json:"url"`
			Reason string `json:"reason"`
		} `json:"selections"`
	}
	if err := decodeLastModelObject(content, &envelope); err != nil {
		return discoveryAction{}, fmt.Errorf("%w: %w", errMalformedAction, err)
	}

	switch actionKind(envelope.Action) {
	case actionSearch:
		query := boundedRedactedText(envelope.Query, maxReasonRunes)
		if query == "" {
			return discoveryAction{}, fmt.Errorf("%w: search action requires a non-empty query", errMalformedAction)
		}
		return discoveryAction{Kind: actionSearch, Query: query}, nil

	case actionOpen:
		url := strings.TrimSpace(envelope.URL)
		if url == "" {
			return discoveryAction{}, fmt.Errorf("%w: open action requires a non-empty url", errMalformedAction)
		}
		return discoveryAction{Kind: actionOpen, URL: url}, nil

	case actionAccept:
		if len(envelope.Selections) == 0 {
			return discoveryAction{}, fmt.Errorf("%w: accept action requires at least one selection", errMalformedAction)
		}
		selections := make([]actionSelection, 0, len(envelope.Selections))
		dropped := 0
		for _, raw := range envelope.Selections {
			url := strings.TrimSpace(raw.URL)
			if url == "" {
				dropped++
				continue
			}
			selections = append(selections, actionSelection{URL: url, Reason: boundedRedactedText(raw.Reason, maxReasonRunes)})
		}
		if len(selections) == 0 {
			return discoveryAction{}, fmt.Errorf("%w: accept action selections all had an empty url", errMalformedAction)
		}
		return discoveryAction{Kind: actionAccept, Selections: selections, DroppedSelections: dropped}, nil

	case actionDone:
		return discoveryAction{Kind: actionDone}, nil

	default:
		return discoveryAction{}, fmt.Errorf("%w: unsupported action %q", errMalformedAction, envelope.Action)
	}
}
