package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTavilySearch_Search_sends_contract_and_maps_results(t *testing.T) {
	// Given
	key := strings.Join([]string{"test", "credential"}, "-")
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got, want := req.Method, http.MethodPost; got != want {
				t.Errorf("method = %q, want %q", got, want)
			}
			if got, want := req.URL.String(), "https://api.tavily.com/search"; got != want {
				t.Errorf("URL = %q, want %q", got, want)
			}
			if got, want := req.Header.Get("Authorization"), "Bearer "+key; got != want {
				t.Errorf("authorization = %q, want %q", got, want)
			}
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			want := map[string]any{
				"query":               "AI [email removed] [phone removed] events",
				"search_depth":        "basic",
				"max_results":         float64(10),
				"include_answer":      false,
				"include_raw_content": false,
				"include_images":      false,
				"safe_search":         true,
			}
			if !mapsEqual(body, want) {
				t.Errorf("request body = %#v, want %#v", body, want)
			}
			return jsonResponse(http.StatusOK, `{"results":[{"title":"AI Expo speaker@example.com","url":"https://events.example/expo","content":"Call 010-9999-8888"}]}`), nil
		}),
	}
	search, err := NewTavilySearch(key, client)
	if err != nil {
		t.Fatalf("construct search: %v", err)
	}

	// When
	results, err := search.Search(context.Background(), "AI person@example.com 010-1234-5678 events")

	// Then
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	want := SearchResult{Title: "AI Expo [email removed]", URL: "https://events.example/expo", Snippet: "Call [phone removed]"}
	if results[0] != want {
		t.Fatalf("result = %#v, want %#v", results[0], want)
	}
}

func TestTavilySearch_Search_does_not_disclose_credentials_in_errors(t *testing.T) {
	responseDetail := strings.Join([]string{"response", "private", "detail"}, "-")
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "non-success status", statusCode: http.StatusUnauthorized, body: `{"error":"` + responseDetail + `"}`},
		{name: "malformed response", statusCode: http.StatusOK, body: `{"error":"` + responseDetail + `"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			key := strings.Join([]string{"private", "credential"}, "-")
			client := &http.Client{Timeout: 5 * time.Second, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(tt.statusCode, tt.body), nil
			})}
			search, err := NewTavilySearch(key, client)
			if err != nil {
				t.Fatalf("construct search: %v", err)
			}

			// When
			_, err = search.Search(context.Background(), "AI events")

			// Then
			if err == nil {
				t.Fatal("search error = nil, want error")
			}
			if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), responseDetail) {
				t.Fatalf("error disclosed sensitive response data: %q", err)
			}
		})
	}
}

func TestTavilySearch_Search_drops_invalid_and_unsafe_result_urls(t *testing.T) {
	// Given
	body := `{"results":[
		{"title":"valid","url":"https://events.example/calendar","content":"ok"},
		{"title":"malformed","url":"://broken","content":"bad"},
		{"title":"scheme","url":"ftp://events.example/calendar","content":"bad"},
		{"title":"userinfo","url":"https://user:pass@events.example/calendar","content":"bad"},
		{"title":"localhost","url":"http://localhost/events","content":"bad"},
		{"title":"private","url":"http://192.168.1.2/events","content":"bad"},
		{"title":"loopback","url":"http://[::1]/events","content":"bad"},
		{"title":"contact","url":"https://events.example/speaker@example.com","content":"bad"}
	]}`
	client := &http.Client{Timeout: 5 * time.Second, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})}
	search, err := NewTavilySearch("key", client)
	if err != nil {
		t.Fatalf("construct search: %v", err)
	}

	// When
	results, err := search.Search(context.Background(), "AI events")

	// Then
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("result count = %d, want %d", got, want)
	}
	if got, want := results[0].URL, "https://events.example/calendar"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}

func TestNewTavilySearch_rejects_missing_key_and_unbounded_client(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		client *http.Client
	}{
		{name: "missing key", key: "", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "whitespace key", key: "  ", client: &http.Client{Timeout: 5 * time.Second}},
		{name: "nil client", key: "key", client: nil},
		{name: "unbounded client", key: "key", client: &http.Client{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			_, err := NewTavilySearch(tt.key, tt.client)

			// Then
			if err == nil {
				t.Fatal("constructor error = nil, want error")
			}
			if strings.Contains(err.Error(), tt.key) && strings.TrimSpace(tt.key) != "" {
				t.Fatalf("constructor error disclosed key: %q", err)
			}
		})
	}
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func mapsEqual(got, want map[string]any) bool {
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	return string(gotJSON) == string(wantJSON)
}
