package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/abcdlsj/gnar/internal/httpx"
	"github.com/abcdlsj/gnar/internal/norm"
	"github.com/abcdlsj/gnar/pkg/api"
)

func (s *Server) handleTunnels(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeManage(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tenant := ""
	if value := r.URL.Query().Get("tenant"); value != "" {
		tenant = norm.Tenant(value)
	}
	httpx.WriteJSON(w, http.StatusOK, api.ListTunnelsResponse{Tunnels: s.store.List(tenant)})
}

func (s *Server) handleTunnelByName(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeManage(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ref := strings.TrimPrefix(r.URL.Path, "/api/v1/tunnels/")
	ref = strings.Trim(ref, "/")
	if ref == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing tunnel name")
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
		httpx.WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	if len(parts) == 2 && parts[1] == "logs" {
		limit := parseLimit(r.URL.Query().Get("limit"))
		if limit > 0 && len(detail.RecentRequests) > limit {
			detail.RecentRequests = detail.RecentRequests[:limit]
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"tunnel":   detail.Tunnel,
			"requests": detail.RecentRequests,
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (s *Server) authorizeManage(w http.ResponseWriter, r *http.Request) bool {
	token := s.cfg.ManageToken
	if token == "" {
		token = s.cfg.AgentToken
	}
	if token == "" {
		return true
	}

	if httpx.RequestToken(r) != token {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid token")
		return false
	}
	return true
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
