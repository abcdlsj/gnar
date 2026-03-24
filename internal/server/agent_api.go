package server

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/abcdlsj/gnar/internal/httpx"
	"github.com/abcdlsj/gnar/internal/norm"
	"github.com/abcdlsj/gnar/pkg/api"
)

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req api.RegisterTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request")
		return
	}

	req.Tenant = norm.Tenant(req.Tenant)
	if !s.authorizeAgent(req.Tenant, req.Token) {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	if _, err := url.Parse(req.Target); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid target")
		return
	}

	response, err := s.store.Register(req)
	if err != nil {
		httpx.WriteError(w, http.StatusConflict, err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

func (s *Server) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	event, err := s.store.WaitEvent(sessionID, s.cfg.PollTimeout)
	if err != nil {
		httpx.WriteError(w, http.StatusGone, err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, api.PollResponse{Event: event})
}

func (s *Server) handleRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	var response api.PostResponseRequest
	if err := json.NewDecoder(r.Body).Decode(&response); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if response.RequestID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing request_id")
		return
	}

	if err := s.store.Complete(sessionID, response); err != nil {
		httpx.WriteError(w, http.StatusGone, err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) handleUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	s.store.Remove(sessionID)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "removed"})
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
