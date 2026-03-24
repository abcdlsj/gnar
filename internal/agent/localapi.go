package agent

import "time"

type StartTunnelRequest struct {
	ServerURL        string        `json:"server_url"`
	TargetURL        string        `json:"target_url"`
	Tenant           string        `json:"tenant"`
	Name             string        `json:"name"`
	Domains          []string      `json:"domains"`
	Token            string        `json:"token"`
	RequestTimeout   time.Duration `json:"request_timeout"`
	PollRetryBackoff time.Duration `json:"poll_retry_backoff"`
	MaxResponseBytes int64         `json:"max_response_bytes"`
}

type ManagedTunnel struct {
	Tenant      string    `json:"tenant"`
	Name        string    `json:"name"`
	TargetURL   string    `json:"target_url"`
	ServerURL   string    `json:"server_url"`
	Domains     []string  `json:"domains"`
	PublicURL   string    `json:"public_url"`
	URLs        []string  `json:"urls"`
	Status      string    `json:"status"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ConnectedAt time.Time `json:"connected_at,omitempty"`
}

type StartTunnelResponse struct {
	Tunnel ManagedTunnel `json:"tunnel"`
}

type ListManagedResponse struct {
	Tunnels []ManagedTunnel `json:"tunnels"`
}
