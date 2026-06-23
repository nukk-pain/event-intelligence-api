// Package cfpurge issues a Cloudflare cache purge after an ingest that changed
// data, so the edge stops serving stale event JSON/HTML immediately rather than
// waiting out the s-maxage window. It is deliberately tiny and dependency-free:
// one POST to the Cloudflare API using the standard library.
//
// Purging is OPTIONAL. When zoneID or token is empty the call is a no-op (returns
// nil), so local runs and the read-only serve path never need credentials. A
// purge failure is reported as an error for the caller to LOG, but it must never
// fail the ingest — fresh data is already in the store; only the edge is briefly
// stale.
package cfpurge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// endpoint builds the Cloudflare purge_cache URL for a zone.
func endpoint(zoneID string) string {
	return "https://api.cloudflare.com/client/v4/zones/" + zoneID + "/purge_cache"
}

// PurgeEverything purges the entire Cloudflare cache for the zone. It is used
// after a data-changing ingest. purge_everything is chosen over a targeted file
// list because the dataset changes at most once per day, so the cost of an
// occasional full purge is irrelevant and it cannot miss a path.
//
// A nil httpClient uses a 10s-timeout default. An empty zoneID or token disables
// the purge (returns nil). The token is sent as a Bearer credential and is never
// included in any returned error.
func PurgeEverything(ctx context.Context, zoneID, token string, httpClient *http.Client) error {
	if zoneID == "" || token == "" {
		return nil // purge disabled — no credentials configured
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	body, err := json.Marshal(map[string]any{"purge_everything": true})
	if err != nil {
		return fmt.Errorf("cfpurge: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(zoneID), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("cfpurge: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cfpurge: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read a bounded slice of the body for diagnostics (Cloudflare returns a
		// JSON error envelope). The token is in the request, not the response, so
		// echoing the body is safe.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("cfpurge: cloudflare returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
