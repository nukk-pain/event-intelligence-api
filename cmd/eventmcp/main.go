// Command eventmcp is a Model Context Protocol (MCP) server that exposes the
// event-intelligence data as tools other agents (Hermes, Claude Code, Cursor)
// can call. It speaks JSON-RPC 2.0 over stdio with newline-delimited messages —
// no external dependencies.
//
// Tools:
//
//	search_events  structured filter (category/region/keyword/date). No LLM.
//	ask_events     natural-language question. The model (Solar once configured,
//	               local otherwise) parses it into a filter, then the same
//	               model-free lookup runs. This keeps the data lookup LLM-free
//	               while showcasing the model as the natural-language front door.
//
// Events are loaded from the live read API (events.nukk.net) by default, so the
// MCP tools serve the real deployed data. Use -source <file> to load a fixture
// instead (offline).
//
// Only JSON-RPC messages go to stdout; logs go to stderr.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/smpain/event-intelligence-api/internal/agent"
)

const protocolVersion = "2025-06-18"

var (
	// query runs a Filter against the data source (live API or fixture) and
	// returns matching events. Set in main once the source is chosen.
	query func(agent.Filter) ([]agent.Event, error)
	// refDateFn returns the reference date for relative queries. The stdio CLI
	// pins it per process; the HTTP server must not, since it runs for days and
	// "다음 달" asked in September must not resolve against a July start date.
	refDateFn func() string
	maxTokens = 3000
	timeout   = 90 * time.Second
)

func main() {
	source := flag.String("source", "api", `"api" (live read API) or a fixture file path`)
	apiBase := flag.String("api-base", getenv("EVENTSINTEL_API_BASE", "https://events.nukk.net"), "read API base URL when -source=api")
	max := flag.Int("max", 200, "max events to pull per query")
	today := flag.String("today", "", "reference date for relative queries (default: current date at each query)")
	httpAddr := flag.String("http", "", "serve MCP over streamable HTTP on this address instead of stdio (requires EVENTSINTEL_SOLAR_API_KEY)")
	flag.Parse()

	if *today != "" {
		pinned := *today
		refDateFn = func() string { return pinned }
	} else {
		refDateFn = func() string { return time.Now().Format("2006-01-02") }
	}

	if *source == "api" {
		query = func(f agent.Filter) ([]agent.Event, error) {
			return agent.QueryEvents(context.Background(), *apiBase, f, *max, 20*time.Second)
		}
		fmt.Fprintf(os.Stderr, "eventmcp: querying live API %s, ready on stdio\n", *apiBase)
	} else {
		raw, err := os.ReadFile(*source)
		if err != nil {
			fmt.Fprintln(os.Stderr, "load events:", err)
			os.Exit(1)
		}
		var fixture []agent.Event
		if err := json.Unmarshal(raw, &fixture); err != nil {
			fmt.Fprintln(os.Stderr, "parse events:", err)
			os.Exit(1)
		}
		query = func(f agent.Filter) ([]agent.Event, error) { return agent.Match(fixture, f), nil }
		fmt.Fprintf(os.Stderr, "eventmcp: loaded %d fixture events, ready on stdio\n", len(fixture))
	}
	if *httpAddr != "" {
		if err := serveHTTP(*httpAddr); err != nil {
			fmt.Fprintln(os.Stderr, "eventmcp http:", err)
			os.Exit(1)
		}
		return
	}
	serve(os.Stdin, os.Stdout)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func serve(in *os.File, out *os.File) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			continue // ignore malformed line
		}
		result, rerr, isNotification := handle(req)
		if isNotification {
			continue // notifications get no response
		}
		resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
		if rerr != nil {
			resp.Error = rerr
		} else {
			resp.Result = result
		}
		_ = enc.Encode(resp) // Encoder writes one JSON object + newline
	}
}

func handle(req rpcReq) (result any, rerr *rpcErr, isNotification bool) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "eventmcp", "version": "0.1.0"},
		}, nil, false
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil, true
	case "tools/list":
		return map[string]any{"tools": toolSchemas()}, nil, false
	case "tools/call":
		return callTool(req.Params)
	default:
		if len(req.ID) == 0 {
			return nil, nil, true // unknown notification
		}
		return nil, &rpcErr{Code: -32601, Message: "method not found: " + req.Method}, false
	}
}

func toolSchemas() []map[string]any {
	return []map[string]any{
		{
			"name":        "search_events",
			"description": "Search industry events by structured filter (category, region, keyword, date range). Returns matching events as JSON. No natural-language parsing.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category":  map[string]any{"type": "string", "description": "ai|robotics|bio|medical|health, or empty"},
					"region":    map[string]any{"type": "string", "description": "city/venue keyword, or empty"},
					"keyword":   map[string]any{"type": "string", "description": "term to match in the event name, or empty"},
					"from_date": map[string]any{"type": "string", "description": "YYYY-MM-DD inclusive, or empty"},
					"to_date":   map[string]any{"type": "string", "description": "YYYY-MM-DD inclusive, or empty"},
				},
			},
		},
		{
			"name":        "ask_events",
			"description": "Ask about industry events in natural language (e.g. '다음 달 서울 로봇 행사'). The model parses the question into a filter, then returns matching events. Requires an LLM backend.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"question": map[string]any{"type": "string"}},
				"required":   []string{"question"},
			},
		},
	}
}

func callTool(params json.RawMessage) (any, *rpcErr, bool) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcErr{Code: -32602, Message: "invalid params"}, false
	}
	switch p.Name {
	case "search_events":
		var f agent.Filter
		_ = json.Unmarshal(p.Arguments, &f)
		matched, err := query(f)
		if err != nil {
			return toolError("query events: " + err.Error()), nil, false
		}
		return toolResult(matched, nil), nil, false
	case "ask_events":
		var a struct {
			Question string `json:"question"`
		}
		_ = json.Unmarshal(p.Arguments, &a)
		return askEvents(a.Question)
	default:
		return nil, &rpcErr{Code: -32602, Message: "unknown tool: " + p.Name}, false
	}
}

func askEvents(question string) (any, *rpcErr, bool) {
	backends := agent.LoadBackends()
	if len(backends) == 0 {
		return toolError("no LLM backend configured (set EVENTSINTEL_LOCAL_BASE_URL or EVENTSINTEL_SOLAR_API_KEY)"), nil, false
	}
	f, _, err := agent.ParseQuery(context.Background(), backends[0], question, refDateFn(), maxTokens, timeout)
	if err != nil {
		return toolError("parse question: " + err.Error()), nil, false
	}
	matched, err := query(f)
	if err != nil {
		return toolError("query events: " + err.Error()), nil, false
	}
	return toolResult(matched, &f), nil, false
}

// toolResult renders a tool result as MCP text content holding JSON (agents
// parse JSON; matches the project's machine-facing serialization policy).
func toolResult(matched []agent.Event, filter *agent.Filter) any {
	payload := map[string]any{"count": len(matched), "events": matched}
	if filter != nil {
		payload["interpreted_filter"] = filter
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
}

func toolError(msg string) any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{{"type": "text", "text": msg}},
	}
}
