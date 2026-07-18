package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	tavilySearchURL     = "https://api.tavily.com/search"
	tavilyResponseLimit = 1 << 20
)

var (
	ErrTavilyAPIKeyRequired      = errors.New("tavily search: API key is required")
	ErrBoundedHTTPClientRequired = errors.New("tavily search: bounded HTTP client is required")
	ErrSearchQueryRequired       = errors.New("tavily search: query is required")
)

// TavilySearch searches Tavily's public web index.
type TavilySearch struct {
	apiKey string
	client *http.Client
}

// NewTavilySearch constructs a Tavily search adapter with an injected client.
func NewTavilySearch(apiKey string, client *http.Client) (*TavilySearch, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, ErrTavilyAPIKeyRequired
	}
	if client == nil || client.Timeout <= 0 {
		return nil, ErrBoundedHTTPClientRequired
	}
	return &TavilySearch{apiKey: apiKey, client: client}, nil
}

// Search executes one redacted Tavily query and returns safe public-web hits.
func (s *TavilySearch) Search(ctx context.Context, query string) ([]SearchResult, error) {
	redactedQuery := strings.TrimSpace(StripContacts(query))
	if redactedQuery == "" {
		return nil, ErrSearchQueryRequired
	}
	payload := struct {
		Query             string `json:"query"`
		SearchDepth       string `json:"search_depth"`
		MaxResults        int    `json:"max_results"`
		IncludeAnswer     bool   `json:"include_answer"`
		IncludeRawContent bool   `json:"include_raw_content"`
		IncludeImages     bool   `json:"include_images"`
		SafeSearch        bool   `json:"safe_search"`
	}{
		Query: redactedQuery, SearchDepth: "basic", MaxResults: 10, SafeSearch: true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tavily search: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilySearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tavily search: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily search: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("tavily search: unexpected status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, tavilyResponseLimit+1))
	if err != nil {
		return nil, fmt.Errorf("tavily search: read response: %w", err)
	}
	if len(raw) > tavilyResponseLimit {
		return nil, fmt.Errorf("tavily search: response exceeds %d bytes", tavilyResponseLimit)
	}
	var response struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("tavily search: decode response: %w", err)
	}

	results := make([]SearchResult, 0, len(response.Results))
	for _, result := range response.Results {
		if !safeSearchResultURL(result.URL) {
			continue
		}
		results = append(results, SearchResult{Title: StripContacts(result.Title), URL: result.URL, Snippet: StripContacts(result.Content)})
	}
	return results, nil
}

func safeSearchResultURL(raw string) bool {
	if StripContacts(raw) != raw {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate()
	}
	return true
}
