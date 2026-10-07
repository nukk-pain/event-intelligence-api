package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundedQueryCountsEmptyPagesAndRejectsRedirects(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"data":[],"page":{"next_cursor":"again"}}`)
	}))
	defer s.Close()
	if _, err := QueryEventsBounded(context.Background(), s.URL, Filter{}, 200, time.Second, 2); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("empty page escaped two-GET budget")
	}
	r := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { http.Redirect(w, req, s.URL, http.StatusFound) }))
	defer r.Close()
	if _, err := QueryEventsBounded(context.Background(), r.URL, Filter{}, 200, time.Second, 2); err == nil {
		t.Fatal("redirect bypassed request accounting")
	}
	if calls.Load() != 2 {
		t.Fatal("followed an unbudgeted redirect")
	}
}
