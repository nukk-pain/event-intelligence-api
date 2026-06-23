package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMetaIndex_ListKinds(t *testing.T) {
	base := metaServer(t)
	resp, err := http.Get(base + "/api/v1")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Vocabularies struct {
			ListKinds []string `json:"list_kinds"`
		} `json:"vocabularies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := map[string]bool{"all": true, "venue": true, "benchmark": true}
	if len(body.Vocabularies.ListKinds) != len(want) {
		t.Fatalf("vocabularies.list_kinds = %v, want all+venue+benchmark", body.Vocabularies.ListKinds)
	}
	for _, kind := range body.Vocabularies.ListKinds {
		if !want[kind] {
			t.Fatalf("unexpected list kind %q", kind)
		}
	}
}
