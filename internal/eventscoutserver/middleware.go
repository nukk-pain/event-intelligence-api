package eventscoutserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
)

type requestContextKey struct{}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (writer *statusWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.status = status
	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusWriter) Write(data []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(data)
}

func assignRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := newRequestID()
		writer.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(request.Context(), requestContextKey{}, requestID)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func logRequests(next http.Handler, clock Clock, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := clock.Now()
		captured := &statusWriter{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(captured, request)
		duration := clock.Now().Sub(started).Milliseconds()
		if duration < 0 {
			duration = 0
		}
		requestID, _ := request.Context().Value(requestContextKey{}).(string)
		logger.InfoContext(request.Context(), "http.request",
			slog.String("request_id", requestID),
			slog.Int("status", captured.status),
			slog.Int64("duration_ms", duration),
			slog.Int("active_limit", activeDiscoveryLimit),
			slog.Int("ten_minute_limit", tenMinuteRequestLimit),
			slog.Int("day_limit", dailyRequestLimit),
		)
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() != nil {
				writeErrorResponse(writer, httpError{
					status: http.StatusInternalServerError, code: "internal_error", message: "internal server error",
				})
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

var requestSequence atomic.Uint64

func newRequestID() string {
	var random [12]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	return "request-" + strconv.FormatUint(requestSequence.Add(1), 10)
}
