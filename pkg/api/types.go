package api

import "time"

const (
	EventNoop        = "noop"
	EventHTTPRequest = "http_request"
)

type RegisterTunnelRequest struct {
	Token   string   `json:"token"`
	Tenant  string   `json:"tenant"`
	Name    string   `json:"name"`
	Target  string   `json:"target"`
	Domains []string `json:"domains"`
}

type RegisterTunnelResponse struct {
	SessionID string   `json:"session_id"`
	TunnelID  string   `json:"tunnel_id"`
	Tenant    string   `json:"tenant"`
	Name      string   `json:"name"`
	PublicURL string   `json:"public_url"`
	URLs      []string `json:"urls"`
}

type PollResponse struct {
	Event AgentEvent `json:"event"`
}

type AgentEvent struct {
	Type    string            `json:"type"`
	Request *HTTPRequestEvent `json:"request,omitempty"`
}

type HTTPRequestEvent struct {
	RequestID  string              `json:"request_id"`
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	RawQuery   string              `json:"raw_query"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
	Host       string              `json:"host"`
	Scheme     string              `json:"scheme"`
	RemoteAddr string              `json:"remote_addr"`
}

type PostResponseRequest struct {
	RequestID  string              `json:"request_id"`
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type TunnelSummary struct {
	ID             string    `json:"id"`
	Tenant         string    `json:"tenant"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Target         string    `json:"target"`
	Domains        []string  `json:"domains"`
	URLs           []string  `json:"urls"`
	PublicURL      string    `json:"public_url"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	LastSeen       time.Time `json:"last_seen"`
	TotalRequests  int       `json:"total_requests"`
	ActiveRequests int       `json:"active_requests"`
	LastError      string    `json:"last_error,omitempty"`
	LastStatusCode int       `json:"last_status_code,omitempty"`
}

type RequestLogEntry struct {
	RequestID   string    `json:"request_id"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	Host        string    `json:"host"`
	RemoteAddr  string    `json:"remote_addr"`
	StatusCode  int       `json:"status_code"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	DurationMS  int64     `json:"duration_ms"`
	Error       string    `json:"error,omitempty"`
}

type ListTunnelsResponse struct {
	Tunnels []TunnelSummary `json:"tunnels"`
}

type TunnelDetailResponse struct {
	Tunnel         TunnelSummary     `json:"tunnel"`
	RecentRequests []RequestLogEntry `json:"recent_requests"`
}
