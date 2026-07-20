package eventscoutserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

type readyListener struct {
	net.Listener
	once  sync.Once
	ready chan struct{}
}

func (listener *readyListener) Accept() (net.Conn, error) {
	listener.once.Do(func() { close(listener.ready) })
	return listener.Listener.Accept()
}

func TestServer_gracefully_stops_when_context_is_cancelled(t *testing.T) {
	// Given
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() {
		if err := baseListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("listener.Close() error = %v", err)
		}
	})
	listener := &readyListener{Listener: baseListener, ready: make(chan struct{})}
	options := DefaultServerOptions(baseListener.Addr().String())
	server, err := NewServer(options, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.serve(ctx, listener) }()
	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("server did not begin accepting connections")
	}
	response, err := http.Get("http://" + baseListener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("response.Body.Close() error = %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	// When
	cancel()

	// Then
	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop within shutdown bound")
	}
}
