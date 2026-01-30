package tunnel

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ServerConfig configures the tunnel server.
type ServerConfig struct {
	ListenAddr string
	QUIC       QUICConfig
	HTTPS      HTTPSConfig
	Domain     DomainConfig
}

// HTTPSConfig configures HTTPS.
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

// Server represents the tunnel server.
type Server struct {
	config  ServerConfig
	auth    *authHandler
	tunnels *tunnelManager
	domains *domainManager
	ports   *portManager
	events  *eventEmitter
	mu      sync.RWMutex
	running bool
}

// authHandler handles authentication.
type authHandler struct {
	tokens     map[string]*tokenInfo
	accessTTL  time.Duration
	refreshTTL time.Duration
	mu         sync.RWMutex
}

type tokenInfo struct {
	UserID       string
	RefreshToken string
	CreatedAt    time.Time
}

// tunnelManager manages active tunnels.
type tunnelManager struct {
	tunnels map[string]*ServerTunnel
	mu      sync.RWMutex
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
	CreatedAt  time.Time
}

// domainManager manages domain allocation.
type domainManager struct {
	baseDomain  string
	randomLen   int
	usedDomains map[string]*domainInfo
	mu          sync.RWMutex
}

type domainInfo struct {
	Domain    string
	TunnelID  string
	CreatedAt time.Time
}

// portManager manages port allocation.
type portManager struct {
	startPort int
	endPort   int
	usedPorts map[int]string
	mu        sync.RWMutex
}

// NewServer creates a new server.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Domain.RandomLen == 0 {
		cfg.Domain.RandomLen = 8
	}

	return &Server{
		config:  cfg,
		auth:    newAuthHandler(),
		tunnels: newTunnelManager(),
		domains: newDomainManager(cfg.Domain.BaseDomain, cfg.Domain.RandomLen),
		ports:   newPortManager(10000, 65535),
		events:  newEventEmitter(),
	}, nil
}

func newAuthHandler() *authHandler {
	return &authHandler{
		tokens:     make(map[string]*tokenInfo),
		accessTTL:  time.Hour,
		refreshTTL: 365 * 24 * time.Hour,
	}
}

func newTunnelManager() *tunnelManager {
	return &tunnelManager{
		tunnels: make(map[string]*ServerTunnel),
	}
}

func newDomainManager(base string, randomLen int) *domainManager {
	return &domainManager{
		baseDomain:  base,
		randomLen:   randomLen,
		usedDomains: make(map[string]*domainInfo),
	}
}

func newPortManager(start, end int) *portManager {
	return &portManager{
		startPort: start,
		endPort:   end,
		usedPorts: make(map[int]string),
	}
}

// Run starts the server.
func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	// TODO: implement actual QUIC listener

	<-ctx.Done()
	return s.Shutdown(context.Background())
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}
	s.running = false

	// TODO: close all connections

	return nil
}

// GenerateToken generates a new token for a user.
func (s *Server) GenerateToken(userID string) string {
	refreshToken := uuid.New().String()
	s.auth.mu.Lock()
	s.auth.tokens[refreshToken] = &tokenInfo{
		UserID:       userID,
		RefreshToken: refreshToken,
		CreatedAt:    time.Now(),
	}
	s.auth.mu.Unlock()
	return refreshToken
}

func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}
