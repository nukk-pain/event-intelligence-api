package httpserver

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestFiniteProductionTimeouts(t *testing.T) {
	s := New("", nil)
	if s.ReadHeaderTimeout != 5*time.Second || s.ReadTimeout != 10*time.Second || s.WriteTimeout != 45*time.Second || s.IdleTimeout != 60*time.Second {
		t.Fatal("production socket timeout contract changed")
	}
}

func TestSocketTimeouts(t *testing.T) {
	for _, kind := range []string{"header", "body", "idle", "write"} {
		t.Run(kind, func(t *testing.T) {
			s := New("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "body" {
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						w.WriteHeader(http.StatusRequestTimeout)
						return
					}
				}
				if kind == "write" {
					time.Sleep(180 * time.Millisecond)
				}
				_, _ = io.WriteString(w, "ok")
			}))
			s.ReadHeaderTimeout = 80 * time.Millisecond
			s.ReadTimeout = 120 * time.Millisecond
			s.WriteTimeout = 120 * time.Millisecond
			s.IdleTimeout = 80 * time.Millisecond
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go s.Serve(ln)
			defer s.Close()
			c, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(2 * time.Second))
			br := bufio.NewReader(c)
			switch kind {
			case "header":
				io.WriteString(c, "GET / HTTP/1.1\r\nHost: test\r\n")
			case "body":
				io.WriteString(c, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 10\r\n\r\nx")
			default:
				io.WriteString(c, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")
			}
			if kind == "idle" {
				resp, err := http.ReadResponse(br, nil)
				if err != nil {
					t.Fatal(err)
				}
				b, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || string(b) != "ok" {
					t.Fatal("normal response failed")
				}
			}
			// Read until the server closes. A client-side timeout is not evidence
			// that the configured server timeout actually terminated the socket.
			_, err = io.Copy(io.Discard, br)
			if err != nil {
				t.Fatalf("socket did not close before client deadline: %v", err)
			}
		})
	}
}
