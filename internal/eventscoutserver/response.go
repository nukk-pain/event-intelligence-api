package eventscoutserver

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func writeDiscoveryResponse(writer http.ResponseWriter, response discoverResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		writeErrorResponse(writer, httpError{
			status: http.StatusInternalServerError, code: "internal_error", message: "internal server error",
		})
		return
	}
	writeJSONBytes(writer, http.StatusOK, data)
}

func writeStatusResponse(writer http.ResponseWriter, status string) {
	data, err := json.Marshal(statusResponse{Status: status})
	if err != nil {
		writeErrorResponse(writer, httpError{
			status: http.StatusInternalServerError, code: "internal_error", message: "internal server error",
		})
		return
	}
	writeJSONBytes(writer, http.StatusOK, data)
}

type httpError struct {
	status     int
	code       string
	message    string
	retryAfter int
}

func writeErrorResponse(writer http.ResponseWriter, response httpError) {
	body := errorEnvelope{Error: errorBody{Code: response.code, Message: response.message}}
	if response.retryAfter > 0 {
		body.Error.RetryAfterSeconds = &response.retryAfter
		writer.Header().Set("Retry-After", strconv.Itoa(response.retryAfter))
	}
	data, err := json.Marshal(body)
	if err != nil {
		data = []byte(`{"error":{"code":"internal_error","message":"internal server error"}}`)
		response.status = http.StatusInternalServerError
	}
	writeJSONBytes(writer, response.status, data)
}

func writeJSONBytes(writer http.ResponseWriter, status int, data []byte) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if _, err := writer.Write(data); err != nil {
		return
	}
}
