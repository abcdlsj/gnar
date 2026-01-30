package components

import "github.com/abcdlsj/gnar/pkg/tunnel"

// ServerSelectedMsg is sent when a server is selected.
type ServerSelectedMsg struct {
	Server string
}

// AuthSuccessMsg is sent when authentication succeeds.
type AuthSuccessMsg struct {
	Token string
}

// ServiceSelectedMsg is sent when a service is selected.
type ServiceSelectedMsg struct {
	Port int
}

// ConnectionSuccessMsg is sent when connection succeeds.
type ConnectionSuccessMsg struct {
	Tunnel *tunnel.Tunnel
}

// ConnectionErrorMsg is sent when connection fails.
type ConnectionErrorMsg struct {
	Error error
}
