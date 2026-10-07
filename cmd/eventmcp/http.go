package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/smpain/event-intelligence-api/internal/clientidentity"
	"github.com/smpain/event-intelligence-api/internal/httpserver"
)

const (
	httpMaxBody     = 16 << 10
	httpCallTimeout = 30 * time.Second
)

func clientKey(r *http.Request) string { return clientidentity.Key(r) }

func serveHTTP(addr string) error {
	if os.Getenv("EVENTSINTEL_SOLAR_API_KEY") == "" {
		return errors.New("EVENTSINTEL_SOLAR_API_KEY is required in HTTP mode")
	}
	cfg, err := loadHTTPConfig()
	if err != nil {
		return err
	}
	budget, err := openDailyBudget(cfg.quotaDir, cfg.dailyLLM)
	if err != nil {
		return fmt.Errorf("open MCP quota: %w", err)
	}
	defer budget.Close()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := httpserver.New(addr, newHTTPHandler(newToolQuota(cfg), budget))
	logger.Info("eventmcp http listening", "addr", addr)
	return server.ListenAndServe()
}

func newHTTPHandler(quota *toolQuota, budget *dailyBudget) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) { mcpPost(w, r, quota, budget) })
	return mux
}

// One independent JSON-RPC message per POST; no sessions are issued or required.
func mcpPost(w http.ResponseWriter, r *http.Request, quota *toolQuota, budget *dailyBudget) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), httpCallTimeout)
	defer cancel()
	body, err := io.ReadAll(io.LimitReader(r.Body, httpMaxBody+1))
	if err != nil || len(body) > httpMaxBody {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	var req rpcReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPC(w, rpcResp{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: "parse error"}})
		return
	}
	if req.Method == "tools/call" {
		if len(req.ID) == 0 || string(req.ID) == "null" {
			writeRPC(w, rpcResp{JSONRPC: "2.0", Error: &rpcErr{Code: -32600, Message: "tool calls require a request id"}})
			return
		}
		release, retry := quota.acquire(clientKey(r), isAskEvents(req))
		if release == nil {
			w.Header().Set("Retry-After", fmt.Sprint(retry))
			writeRPCStatus(w, http.StatusTooManyRequests, rpcResp{JSONRPC: "2.0", ID: req.ID, Error: &rpcErr{Code: -32000, Message: "tool quota or capacity exceeded; retry later"}})
			return
		}
		// Hold slots until the underlying work really returns, even on cancellation.
		defer release()
	}
	result, rerr, notification := handle(ctx, req, budget)
	if notification {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if ctx.Err() != nil {
		message := "request cancelled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			message = "timeout"
		}
		rerr = &rpcErr{Code: -32000, Message: message}
		result = nil
		slog.Info("mcp request ended", "reason", message, "method", req.Method)
	}
	status := http.StatusOK
	if rerr != nil && rerr.status != 0 {
		status = rerr.status
		w.Header().Set("Retry-After", fmt.Sprint(rerr.retry))
	}
	writeRPCStatus(w, status, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rerr})
}

func isAskEvents(req rpcReq) bool {
	if req.Method != "tools/call" {
		return false
	}
	var p struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(req.Params, &p)
	return p.Name == "ask_events"
}

func writeRPC(w http.ResponseWriter, resp rpcResp) { writeRPCStatus(w, http.StatusOK, resp) }

func writeRPCStatus(w http.ResponseWriter, status int, resp rpcResp) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}
