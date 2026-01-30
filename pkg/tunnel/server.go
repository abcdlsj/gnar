package tunnel

import (
	"context"
	"time"
)

// ServerConfig configures the tunnel server.
type ServerConfig struct {
	ListenAddr string
	QUIC       QUICConfig
	HTTPS      HTTPSConfig
	Domain     DomainConfig
}

// HTTPSConfig configures HTTPS support.
type HTTPSConfig struct {
	Enabled  bool
	AutoCert bool
	CertDir  string
}

// DomainConfig configures domain allocation.
type DomainConfig struct {
	BaseDomain string
	RandomLen  int
}

// Server represents the public server API.
type Server struct {
	quic *quicServer
}

// ServerTunnel represents a server-side tunnel.
type ServerTunnel struct {
	ID         string
	ClientID   string
	LocalAddr  string
	PublicURL  string
	ServerPort int
	Domain     string
	Status     TunnelStatus
	Stream     interface{} // quic.Stream - using interface{} to avoid import cycle
	CreatedAt  time.Time
}

// NewServer creates a new tunnel server.
func NewServer(cfg ServerConfig) (*Server, error) {
	quicSrv, err := newQUICServer(cfg)
	if err != nil {
		return nil, err
	}
	return &Server{quic: quicSrv}, nil
}

// Run starts the server.
func (s *Server) Run(ctx context.Context) error {
	return s.quic.Run(ctx)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.quic.Shutdown(ctx)
}

// IsRunning returns true if the server is running.
func (s *Server) IsRunning() bool {
	return s.quic.IsRunning()
}
