package eventscoutserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

type applicationHandler struct {
	runner          DiscoveryRunner
	clock           Clock
	requestTimeout  time.Duration
	clientIdentity  clientIdentity
	quota           *quotaLimiter
	activeDiscovery chan struct{}
}

func NewHandler(options HandlerOptions, dependencies HandlerDependencies) (http.Handler, error) {
	if options.RequestTimeout <= 0 || options.RequestTimeout > maximumRequestDeadline ||
		dependencies.Runner == nil || dependencies.Clock == nil || dependencies.Logger == nil {
		return nil, ErrInvalidHandlerConfig
	}
	core := &applicationHandler{
		runner: dependencies.Runner, clock: dependencies.Clock, requestTimeout: options.RequestTimeout,
		clientIdentity: newClientIdentity(options.TrustedProxies), quota: newQuotaLimiter(dependencies.Clock),
		activeDiscovery: make(chan struct{}, activeDiscoveryLimit),
	}
	var handler http.Handler = core
	handler = recoverPanics(handler)
	handler = logRequests(handler, dependencies.Clock, dependencies.Logger)
	handler = assignRequestID(handler)
	return handler, nil
}

func (handler *applicationHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/v1/discover":
		handler.serveDiscoverRoute(writer, request)
	case "/healthz":
		serveStatusRoute(writer, request, "ok")
	case "/readyz":
		serveStatusRoute(writer, request, "ready")
	default:
		writeErrorResponse(writer, httpError{status: http.StatusNotFound, code: "not_found", message: "route not found"})
	}
}

func (handler *applicationHandler) serveDiscoverRoute(writer http.ResponseWriter, request *http.Request) {
	setDiscoveryCORS(writer)
	if request.Method == http.MethodOptions {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST, OPTIONS")
		writeErrorResponse(writer, httpError{
			status: http.StatusMethodNotAllowed, code: "method_not_allowed", message: "method not allowed",
		})
		return
	}
	goal, err := parseGoalRequest(request)
	if err != nil {
		writeErrorResponse(writer, httpError{
			status: http.StatusBadRequest, code: "invalid_request", message: "request must contain only a valid goal",
		})
		return
	}
	decision := handler.quota.allow(handler.clientIdentity.key(request))
	if !decision.allowed {
		writeErrorResponse(writer, httpError{
			status: http.StatusTooManyRequests, code: "rate_limited", message: "request quota exceeded",
			retryAfter: decision.retryAfter,
		})
		return
	}
	select {
	case handler.activeDiscovery <- struct{}{}:
		defer func() { <-handler.activeDiscovery }()
	default:
		writeErrorResponse(writer, httpError{
			status: http.StatusServiceUnavailable, code: "server_busy", message: "server is busy",
		})
		return
	}
	handler.runDiscovery(writer, request, goal)
}

func (handler *applicationHandler) runDiscovery(writer http.ResponseWriter, request *http.Request, goal Goal) {
	ctx, cancel := context.WithTimeout(request.Context(), handler.requestTimeout)
	defer cancel()
	started := handler.clock.Now()
	output, err := handler.runner.Discover(ctx, goal)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		writeErrorResponse(writer, httpError{
			status: http.StatusGatewayTimeout, code: "deadline_exceeded", message: "discovery deadline exceeded",
		})
		return
	}
	if err != nil {
		writeErrorResponse(writer, httpError{
			status: http.StatusInternalServerError, code: "internal_error", message: "internal server error",
		})
		return
	}
	sources := output.Sources
	if sources == nil {
		sources = []agent.DiscoveredSource{}
	}
	reasons := output.TruncationReasons
	if reasons == nil {
		reasons = []string{}
	}
	elapsed := handler.clock.Now().Sub(started).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	writeDiscoveryResponse(writer, discoverResponse{Sources: sources, Meta: discoverMeta{
		Provider: "public", Profile: agent.DiscoveryProfileEvents,
		TruncationReasons: append([]string{}, reasons...), ModelCalls: output.ModelCalls,
		PromptTokens: output.PromptTokens, CompletionTokens: output.CompletionTokens,
		ElapsedMilliseconds: elapsed,
	}})
}

func parseGoalRequest(request *http.Request) (Goal, error) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Goal{}, ErrInvalidGoal
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, requestBodyLimit+1))
	if err != nil || len(body) > requestBodyLimit || !utf8.Valid(body) {
		return Goal{}, ErrInvalidGoal
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return Goal{}, ErrInvalidGoal
	}
	goalText := ""
	goalSeen := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, keyIsString := keyToken.(string)
		if err != nil || !keyIsString || key != "goal" || goalSeen {
			return Goal{}, ErrInvalidGoal
		}
		if err := decoder.Decode(&goalText); err != nil {
			return Goal{}, ErrInvalidGoal
		}
		goalSeen = true
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !goalSeen {
		return Goal{}, ErrInvalidGoal
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Goal{}, ErrInvalidGoal
	}
	return NewGoal(goalText)
}

func serveStatusRoute(writer http.ResponseWriter, request *http.Request, status string) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", "GET")
		writeErrorResponse(writer, httpError{
			status: http.StatusMethodNotAllowed, code: "method_not_allowed", message: "method not allowed",
		})
		return
	}
	writeStatusResponse(writer, status)
}

func setDiscoveryCORS(writer http.ResponseWriter) {
	writer.Header().Set("Access-Control-Allow-Origin", "*")
	writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
