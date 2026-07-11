// Package agent is the Solar-backed event-intelligence agent: it turns raw,
// unstructured Korean event text into structured facts and a founder-actionable
// brief. It is backend-agnostic (any OpenAI-compatible endpoint) so the same
// code runs against a local model today and Solar once Open 2 is available.
//
// Compliance (Upstage data policy): StripContacts removes phone/email from any
// text before it is sent to a model. The agent only ever handles public event
// facts, never personal contact info.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// Backend is one OpenAI-compatible chat endpoint.
type Backend struct {
	Name    string
	BaseURL string
	APIKey  string
	Model   string
}

// Usage is the token accounting returned by the endpoint.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// LoadBackends builds the backend list from EVENTSINTEL_* env vars. The local
// backend is on by default (set EVENTSINTEL_LOCAL_BASE_URL=off to skip); the
// solar backend appears only once EVENTSINTEL_SOLAR_API_KEY is set.
func LoadBackends() []Backend {
	var out []Backend
	if localURL := getenv("EVENTSINTEL_LOCAL_BASE_URL", "http://127.0.0.1:18900/v1"); !strings.EqualFold(localURL, "off") {
		out = append(out, Backend{
			Name:    "local",
			BaseURL: localURL,
			Model:   getenv("EVENTSINTEL_LOCAL_MODEL", "qwen36-dwq"),
		})
	}
	if key := os.Getenv("EVENTSINTEL_SOLAR_API_KEY"); key != "" {
		out = append(out, Backend{
			Name:    "solar",
			BaseURL: getenv("EVENTSINTEL_SOLAR_BASE_URL", "https://api.upstage.ai/v1"),
			APIKey:  key,
			Model:   getenv("EVENTSINTEL_SOLAR_MODEL", "solar-pro"),
		})
	}
	return out
}

// Chat sends a system+user prompt and returns the assistant content, token
// usage, and latency.
func (b Backend) Chat(ctx context.Context, system, user string, maxTokens int, timeout time.Duration) (string, Usage, time.Duration, error) {
	body, _ := json.Marshal(map[string]any{
		"model":       b.Model,
		"temperature": 0,
		"max_tokens":  maxTokens,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, strings.TrimRight(b.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", Usage{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.APIKey)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", Usage{}, 0, err
	}
	defer resp.Body.Close()
	lat := time.Since(start)

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", Usage{}, lat, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	u := Usage{PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens}
	if resp.StatusCode != http.StatusOK {
		return "", u, lat, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(out.Error)))
	}
	if len(out.Choices) == 0 {
		return "", u, lat, fmt.Errorf("no choices in response")
	}
	return out.Choices[0].Message.Content, u, lat, nil
}

// LastJSONObject returns the last balanced {...} object in s. Reasoning models
// emit thinking before the final answer, so the last object is the result.
func LastJSONObject(s string) string {
	depth, start, best := 0, -1, ""
	for i, r := range s {
		switch r {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					best = s[start : i+1]
				}
			}
		}
	}
	return best
}

var (
	reEmail = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	rePhone = regexp.MustCompile(`0\d{1,2}[-\s]?\d{3,4}[-\s]?\d{4}`)
)

// StripContacts removes phone/email from text before it is sent to a model.
func StripContacts(s string) string {
	s = reEmail.ReplaceAllString(s, "[email removed]")
	s = rePhone.ReplaceAllString(s, "[phone removed]")
	return s
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
