// Package httpserver supplies finite socket timeouts for the public daemons.
package httpserver

import (
	"net/http"
	"time"
)

func New(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// Allow a 30s handler deadline plus body-read and error-response time.
		WriteTimeout: 45 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}
