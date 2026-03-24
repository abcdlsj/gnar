package server

import (
	"errors"
	"net/http"

	"github.com/abcdlsj/gnar/internal/httpx"
	"github.com/abcdlsj/gnar/pkg/api"
)

func (s *Server) handlePublic(w http.ResponseWriter, r *http.Request) {
	tunnel, forwardedPath, err := s.store.Resolve(r.Host, r.URL.Path)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "tunnel not found")
		return
	}

	body, err := readBody(r.Body, s.cfg.MaxBodyBytes)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	requestID := nextID()
	event := api.AgentEvent{
		Type: api.EventHTTPRequest,
		Request: &api.HTTPRequestEvent{
			RequestID:  requestID,
			Method:     r.Method,
			Path:       forwardedPath,
			RawQuery:   r.URL.RawQuery,
			Headers:    httpx.HeaderToMap(r.Header),
			Body:       body,
			Host:       r.Host,
			Scheme:     schemeForRequest(r),
			RemoteAddr: r.RemoteAddr,
		},
	}

	responseCh, err := s.store.Dispatch(tunnel.SessionID, requestID, event)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	select {
	case response, ok := <-responseCh:
		if !ok {
			httpx.WriteError(w, http.StatusBadGateway, "tunnel closed before responding")
			return
		}
		writeForwardedResponse(w, response)
	case <-r.Context().Done():
		s.store.DropPending(tunnel.SessionID, requestID, "client canceled request", httpStatusClientCanceled)
	case <-timeAfter(s.cfg.RequestTimeout):
		s.store.DropPending(tunnel.SessionID, requestID, "agent response timed out", http.StatusGatewayTimeout)
		httpx.WriteError(w, http.StatusGatewayTimeout, "agent response timed out")
	}
}

func writeForwardedResponse(w http.ResponseWriter, response api.PostResponseRequest) {
	headers := w.Header()
	for key, values := range response.Headers {
		if httpx.ShouldSkipHeader(key) {
			continue
		}
		headers[key] = append([]string(nil), values...)
	}

	if response.StatusCode == 0 {
		response.StatusCode = http.StatusOK
	}

	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(response.Body)
}

func schemeForRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		return forwarded
	}
	return "http"
}
