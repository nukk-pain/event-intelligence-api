package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Streamable HTTP transport for the MCP server, so any MCP-capable client can
// use the live event tools by registering a URL instead of installing a Go
// toolchain. The server is stateless: every POST is one JSON-RPC message, no
// session is issued, and GET (the optional server-push stream) answers 405,
// all of which the MCP spec permits.
const (
	httpMaxBody     = 16 << 10
	httpCallTimeout = 30 * time.Second
	// ask_events costs one Solar call per question on the operator's key, so it
	// is the only method that draws down the per-client quota.
	askPerTenMinutes  = 10
	askPerDay         = 60
	maxTrackedClients = 10000
)

// askQuota is a fixed-window per-client limiter, the same shape the anonymous
// eventscout service uses. That one's limits live in package constants tuned
// for a 2-4 call discovery loop, which is why it is not imported here.
// SIMPLIFIED: fixed windows, capped client map, no persistence — an abusive
// client is bounded per window and the map cannot grow unboundedly, which is
// enough for a free demo endpoint behind Cloudflare.
type askQuota struct {
	mu      sync.Mutex
	clients map[string]*askWindows
}

type askWindows struct {
	tenMinute quotaWindow
	daily     quotaWindow
}

type quotaWindow struct {
	start time.Time
	count int
}

func (q *askQuota) allow(client string) (bool, int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	w, ok := q.clients[client]
	if !ok {
		if len(q.clients) >= maxTrackedClients {
			q.clients = make(map[string]*askWindows)
		}
		w = &askWindows{}
		q.clients[client] = w
	}
	for _, pair := range []struct {
		window *quotaWindow
		span   time.Duration
	}{{&w.daily, 24 * time.Hour}, {&w.tenMinute, 10 * time.Minute}} {
		if now.Sub(pair.window.start) >= pair.span {
			pair.window.start = now
			pair.window.count = 0
		}
	}
	if w.daily.count >= askPerDay {
		return false, int(w.daily.start.Add(24 * time.Hour).Sub(now).Seconds())
	}
	if w.tenMinute.count >= askPerTenMinutes {
		return false, int(w.tenMinute.start.Add(10 * time.Minute).Sub(now).Seconds())
	}
	w.daily.count++
	w.tenMinute.count++
	return true, 0
}

// clientKey trusts the first X-Forwarded-For entry only when the peer is
// loopback, which is exactly the Caddy-in-front deployment shape. A direct
// remote connection is keyed by its own address.
func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		if fwd := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); fwd != "" {
			return fwd
		}
	}
	return host
}

func serveHTTP(addr string) error {
	// ask_events runs on the operator's Solar key. Refusing to start without it
	// follows the anonymous eventscout service: a misconfigured deployment must
	// fail loudly, not serve a half-working tool set.
	if os.Getenv("EVENTSINTEL_SOLAR_API_KEY") == "" {
		return errors.New("EVENTSINTEL_SOLAR_API_KEY is required in HTTP mode")
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	quota := &askQuota{clients: make(map[string]*askWindows)}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		mcpPost(w, r, quota)
	})

	server := &http.Server{
		Addr: addr, Handler: mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	logger.Info("eventmcp http listening", "addr", addr)
	return server.ListenAndServe()
}

// mcpPost handles one streamable-HTTP JSON-RPC message. Stateless by design:
// no session is issued and each request stands alone.
func mcpPost(w http.ResponseWriter, r *http.Request, quota *askQuota) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
	if isAskEvents(req) {
		allowed, retry := quota.allow(clientKey(r))
		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprint(max(retry, 1)))
			writeRPCStatus(w, http.StatusTooManyRequests, rpcResp{
				JSONRPC: "2.0", ID: req.ID,
				Error: &rpcErr{Code: -32000, Message: "ask_events quota exceeded; retry later"},
			})
			return
		}
	}
	done := make(chan rpcResp, 1)
	go func() {
		result, rerr, notification := handle(req)
		if notification {
			done <- rpcResp{}
			return
		}
		done <- rpcResp{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rerr}
	}()
	select {
	case resp := <-done:
		if resp.JSONRPC == "" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeRPC(w, resp)
	case <-time.After(httpCallTimeout):
		writeRPC(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Error: &rpcErr{Code: -32000, Message: "timeout"}})
	}
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

func writeRPC(w http.ResponseWriter, resp rpcResp) {
	writeRPCStatus(w, http.StatusOK, resp)
}

func writeRPCStatus(w http.ResponseWriter, status int, resp rpcResp) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}
