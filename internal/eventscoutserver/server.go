package eventscoutserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

var ErrInvalidServerConfig = errors.New("eventscout server: invalid HTTP server config")

type ServerOptions struct {
	Address           string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
}

func DefaultServerOptions(address string) ServerOptions {
	return ServerOptions{
		Address: address, ShutdownTimeout: 10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 65 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8 << 10,
	}
}

type Server struct {
	httpServer      *http.Server
	shutdownTimeout time.Duration
}

func NewServer(options ServerOptions, handler http.Handler) (*Server, error) {
	if handler == nil {
		return nil, ErrInvalidServerConfig
	}
	if err := validateServerOptions(options); err != nil {
		return nil, err
	}
	return &Server{
		httpServer: &http.Server{
			Addr: options.Address, Handler: handler,
			ReadHeaderTimeout: options.ReadHeaderTimeout, ReadTimeout: options.ReadTimeout,
			WriteTimeout: options.WriteTimeout, IdleTimeout: options.IdleTimeout,
			MaxHeaderBytes: options.MaxHeaderBytes, ErrorLog: log.New(io.Discard, "", 0),
		},
		shutdownTimeout: options.ShutdownTimeout,
	}, nil
}

func (server *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", server.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return server.serve(ctx, listener)
}

func (server *Server) serve(ctx context.Context, listener net.Listener) error {
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.httpServer.Serve(listener)
	}()
	select {
	case err := <-serveResult:
		return normalizedServeError(err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), server.shutdownTimeout)
		defer cancel()
		shutdownErr := server.httpServer.Shutdown(shutdownContext)
		if shutdownErr != nil {
			closeErr := server.httpServer.Close()
			serveErr := <-serveResult
			return errors.Join(shutdownErr, closeErr, normalizedServeError(serveErr))
		}
		return normalizedServeError(<-serveResult)
	}
}

func validateServerOptions(options ServerOptions) error {
	if strings.TrimSpace(options.Address) == "" || options.ShutdownTimeout <= 0 ||
		options.ReadHeaderTimeout <= 0 || options.ReadTimeout <= 0 || options.WriteTimeout < maximumRequestDeadline ||
		options.IdleTimeout <= 0 || options.MaxHeaderBytes <= 0 {
		return ErrInvalidServerConfig
	}
	return nil
}

func normalizedServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve: %w", err)
}
