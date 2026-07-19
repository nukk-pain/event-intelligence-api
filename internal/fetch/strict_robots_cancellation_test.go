package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestStrictRobotsCallerContextStopsSharedInflightRequest(t *testing.T) {
	tests := []struct {
		name       string
		newContext func() (context.Context, context.CancelFunc, func())
		wantErr    error
	}{
		{
			name: "cancellation",
			newContext: func() (context.Context, context.CancelFunc, func()) {
				ctx, cancel := context.WithCancel(context.Background())
				return ctx, cancel, cancel
			},
			wantErr: context.Canceled,
		},
		{
			name: "deadline",
			newContext: func() (context.Context, context.CancelFunc, func()) {
				ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
				return ctx, cancel, func() {}
			},
			wantErr: context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			var robotsHits atomic.Int32
			var documentHits atomic.Int32
			requestStarted := make(chan struct{})
			requestStopped := make(chan struct{})
			laterRequest := make(chan struct{}, 1)
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/robots.txt" {
					documentHits.Add(1)
					w.WriteHeader(http.StatusOK)
					return
				}
				hit := robotsHits.Add(1)
				if hit == 1 {
					close(requestStarted)
				} else {
					select {
					case laterRequest <- struct{}{}:
					default:
					}
				}
				select {
				case <-r.Context().Done():
					if hit == 1 {
						close(requestStopped)
					}
				case <-release:
				}
			}))
			t.Cleanup(srv.Close)
			t.Cleanup(func() { close(release) })
			budget := NewPublicCrawlBudget()
			f := strictTestFetcher(t, budget, allowHost(t, srv), WithTimeout(2*time.Second))
			ctx, cancel, trigger := tt.newContext()
			defer cancel()
			const callers = 8
			start := make(chan struct{})
			errs := make(chan error, callers)
			for range callers {
				go func() {
					<-start
					_, err := f.Fetch(ctx, srv.URL+"/event", Conditional{})
					errs <- err
				}()
			}

			// When
			close(start)
			select {
			case <-requestStarted:
			case <-time.After(time.Second):
				t.Fatal("robots request did not start")
			}
			trigger()
			for range callers {
				select {
				case err := <-errs:
					if !errors.Is(err, tt.wantErr) {
						t.Fatalf("Fetch error = %v, want %v", err, tt.wantErr)
					}
				case <-time.After(time.Second):
					t.Fatal("Fetch did not return after caller context ended")
				}
			}

			// Then
			select {
			case <-requestStopped:
			case <-time.After(200 * time.Millisecond):
				t.Fatal("robots request continued after every strict caller returned")
			}
			select {
			case <-laterRequest:
				t.Fatal("a later robots request was emitted after caller context ended")
			case <-time.After(50 * time.Millisecond):
			}
			if robotsHits.Load() != 1 || documentHits.Load() != 0 {
				t.Fatalf("robots hits = %d document hits = %d, want one shared robots request and no document", robotsHits.Load(), documentHits.Load())
			}
			if usage := budget.Usage(); usage.TransportAttempts != 1 {
				t.Fatalf("budget usage = %+v, want one shared robots attempt", usage)
			}
		})
	}
}
