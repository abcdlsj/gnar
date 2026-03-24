package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/abcdlsj/gnar/internal/norm"
	"github.com/abcdlsj/gnar/pkg/api"
)

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req api.RegisterTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	req.Tenant = norm.Tenant(req.Tenant)
	if !s.authorizeAgent(req.Tenant, req.Token) {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	if _, err := url.Parse(req.Target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid target")
		return
	}

	response, err := s.store.Register(req)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	event, err := s.store.WaitEvent(sessionID, s.cfg.PollTimeout)
	if err != nil {
		writeError(w, http.StatusGone, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, api.PollResponse{Event: event})
}

func (s *Server) handleRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	var response api.PostResponseRequest
	if err := json.NewDecoder(r.Body).Decode(&response); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if response.RequestID == "" {
		writeError(w, http.StatusBadRequest, "missing request_id")
		return
	}

	if err := s.store.Complete(sessionID, response); err != nil {
		writeError(w, http.StatusGone, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) handleUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	s.store.Remove(sessionID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handlePublic(w http.ResponseWriter, r *http.Request) {
	tunnel, forwardedPath, err := s.store.Resolve(r.Host, r.URL.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}

	body, err := readBody(r.Body, s.cfg.MaxBodyBytes)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	requestID := nextID()
	headers := cloneHeaders(r.Header)
	event := api.AgentEvent{
		Type: api.EventHTTPRequest,
		Request: &api.HTTPRequestEvent{
			RequestID:  requestID,
			Method:     r.Method,
			Path:       forwardedPath,
			RawQuery:   r.URL.RawQuery,
			Headers:    headers,
			Body:       body,
			Host:       r.Host,
			Scheme:     schemeForRequest(r),
			RemoteAddr: r.RemoteAddr,
		},
	}

	responseCh, err := s.store.Dispatch(tunnel.SessionID, requestID, event)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	select {
	case response, ok := <-responseCh:
		if !ok {
			writeError(w, http.StatusBadGateway, "tunnel closed before responding")
			return
		}

		writeForwardedResponse(w, response)
	case <-r.Context().Done():
		s.store.DropPending(tunnel.SessionID, requestID, "client canceled request", httpStatusClientCanceled)
	case <-timeAfter(s.cfg.RequestTimeout):
		s.store.DropPending(tunnel.SessionID, requestID, "agent response timed out", http.StatusGatewayTimeout)
		writeError(w, http.StatusGatewayTimeout, "agent response timed out")
	}
}

func (s *Server) handleTunnels(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeManage(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tenant := ""
	if value := r.URL.Query().Get("tenant"); value != "" {
		tenant = norm.Tenant(value)
	}
	writeJSON(w, http.StatusOK, api.ListTunnelsResponse{Tunnels: s.store.List(tenant)})
}

func (s *Server) handleTunnelByName(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeManage(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ref := strings.TrimPrefix(r.URL.Path, "/api/v1/tunnels/")
	ref = strings.Trim(ref, "/")
	if ref == "" {
		writeError(w, http.StatusBadRequest, "missing tunnel name")
		return
	}

	parts := strings.Split(ref, "/")
	ref = parts[0]
	tenant := ""
	if value := r.URL.Query().Get("tenant"); value != "" {
		tenant = norm.Tenant(value)
	}

	detail, err := s.store.Detail(tenant, ref)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if len(parts) == 2 && parts[1] == "logs" {
		limit := parseLimit(r.URL.Query().Get("limit"))
		if limit > 0 && len(detail.RecentRequests) > limit {
			detail.RecentRequests = detail.RecentRequests[:limit]
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tunnel":   detail.Tunnel,
			"requests": detail.RecentRequests,
		})
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func parseLimit(value string) int {
	if value == "" {
		return 20
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

var errBodyTooLarge = errors.New("request body exceeds max-body-bytes")

func readBody(body io.ReadCloser, limit int64) ([]byte, error) {
	defer body.Close()

	if limit <= 0 {
		return io.ReadAll(body)
	}

	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, errBodyTooLarge
	}
	return buf.Bytes(), nil
}

func cloneHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers {
		if shouldSkipHeader(key) {
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		result[key] = copied
	}
	return result
}

func writeForwardedResponse(w http.ResponseWriter, response api.PostResponseRequest) {
	headers := w.Header()
	for key, values := range response.Headers {
		if shouldSkipHeader(key) {
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

func shouldSkipHeader(key string) bool {
	key = strings.ToLower(key)
	switch key {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, api.ErrorResponse{Error: message})
}

func (s *Server) authorizeManage(w http.ResponseWriter, r *http.Request) bool {
	token := s.cfg.ManageToken
	if token == "" {
		token = s.cfg.AgentToken
	}
	if token == "" {
		return true
	}

	provided := api.TokenFromRequest(r)

	if provided != token {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return false
	}
	return true
}

func (s *Server) authorizeAgent(tenant, token string) bool {
	if len(s.cfg.AgentCredentials) > 0 {
		expected, ok := s.cfg.AgentCredentials[tenant]
		return ok && expected == token
	}
	if s.cfg.AgentToken == "" {
		return true
	}
	return token == s.cfg.AgentToken
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
