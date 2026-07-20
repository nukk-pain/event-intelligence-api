package eventscoutserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManualHTTP_client_posts_goal_over_real_httptest_server(t *testing.T) {
	// Given
	handler, _ := newTestHandler(t, &stubRunner{}, defaultTestHandlerSettings())
	server := httptest.NewServer(handler)
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/discover", bytes.NewBufferString(`{"goal":"official Korean robotics event sources"}`))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// When
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer response.Body.Close()

	// Then
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body discoverResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.Meta.Provider != "public" {
		t.Fatalf("provider = %q, want public", body.Meta.Provider)
	}
}
