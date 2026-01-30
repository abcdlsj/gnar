package tunnel

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/abcdlsj/gnar/pkg/tunnel/protocol"
)

// quicServer implements QUIC transport for the tunnel server.
type quicServer struct {
	config   ServerConfig
	listener *quic.Listener
	auth     *authHandler
	tunnels  *tunnelManager
	domains  *domainManager
	ports    *portManager
	https    *httpsRouter
	events   *eventEmitter
	mu       sync.RWMutex
	running  bool
}

// httpsRouter routes HTTPS requests to tunnels.
type httpsRouter struct {
	server  *quicServer
	handler http.Handler
}

// newQUICServer creates a new QUIC server.
func newQUICServer(cfg ServerConfig) (*quicServer, error) {
	if cfg.Domain.RandomLen == 0 {
		cfg.Domain.RandomLen = 8
	}

	s := &quicServer{
		config:  cfg,
		auth:    newAuthHandler(),
		tunnels: newTunnelManager(),
		domains: newDomainManager(cfg.Domain.BaseDomain, cfg.Domain.RandomLen),
		ports:   newPortManager(10000, 65535),
		events:  newEventEmitter(),
	}

	if cfg.HTTPS.Enabled {
		s.https = &httpsRouter{server: s}
	}

	return s, nil
}

// Run starts the QUIC server.
func (s *quicServer) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	// Create QUIC listener
	// TODO: configure TLS properly
	// tlsConfig := s.createTLSConfig()
	// listener, err := quic.ListenAddr(s.config.ListenAddr, tlsConfig, nil)
	// if err != nil {
	// 	s.mu.Lock()
	// 	s.running = false
	// 	s.mu.Unlock()
	// 	return fmt.Errorf("listen: %w", err)
	// }
	// s.listener = listener

	// Accept connections
	go s.acceptLoop(ctx)

	// Start HTTPS server if enabled
	if s.config.HTTPS.Enabled {
		go s.runHTTPS(ctx)
	}

	<-ctx.Done()
	return s.Shutdown(context.Background())
}

// acceptLoop accepts incoming connections.
func (s *quicServer) acceptLoop(ctx context.Context) {
	for {
		s.mu.RLock()
		listener := s.listener
		s.mu.RUnlock()

		if listener == nil {
			return
		}

		conn, err := listener.Accept(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		go s.handleConnection(ctx, conn)
	}
}

// handleConnection handles a client connection.
func (s *quicServer) handleConnection(ctx context.Context, conn quic.Connection) {
	defer conn.CloseWithError(0, "done")

	// Accept the auth stream (first stream)
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return
	}

	// Handle authentication
	encoder := protocol.NewEncoder(stream)
	decoder := protocol.NewDecoder(stream)

	pkt, _, err := decoder.Decode()
	if err != nil {
		_ = encoder.Encode(&protocol.AuthResponse{Success: false, Error: "auth failed"})
		return
	}

	authPkt, ok := pkt.(*protocol.AuthPacket)
	if !ok {
		_ = encoder.Encode(&protocol.AuthResponse{Success: false, Error: "expected auth packet"})
		return
	}

	// Validate token
	if !s.auth.validateToken(authPkt.AccessToken) {
		_ = encoder.Encode(&protocol.AuthResponse{Success: false, Error: "invalid token"})
		return
	}

	// Auth success
	if err := encoder.Encode(&protocol.AuthResponse{Success: true}); err != nil {
		return
	}
	stream.Close()

	// Get client ID from token
	clientID := s.auth.getClientID(authPkt.AccessToken)

	// Accept tunnel streams
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			return
		}

		go s.handleTunnelStream(ctx, clientID, stream)
	}
}

// handleTunnelStream handles a tunnel creation request.
func (s *quicServer) handleTunnelStream(ctx context.Context, clientID string, stream quic.Stream) {
	defer stream.Close()

	encoder := protocol.NewEncoder(stream)
	decoder := protocol.NewDecoder(stream)

	// Read tunnel request
	pkt, _, err := decoder.Decode()
	if err != nil {
		return
	}

	req, ok := pkt.(*protocol.TunnelRequest)
	if !ok {
		_ = encoder.Encode(&protocol.TunnelResponse{
			ReqID:   "",
			Success: false,
			Error:   "expected tunnel request",
		})
		return
	}

	// Allocate port
	port, err := s.ports.allocate(clientID)
	if err != nil {
		_ = encoder.Encode(&protocol.TunnelResponse{
			ReqID:   req.ReqID,
			Success: false,
			Error:   "no available port",
		})
		return
	}

	// Allocate domain
	domain, err := s.domains.allocate(req.Subdomain, req.ReqID)
	if err != nil {
		s.ports.release(port)
		_ = encoder.Encode(&protocol.TunnelResponse{
			ReqID:   req.ReqID,
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	publicURL := fmt.Sprintf("https://%s", domain)

	// Create tunnel
	tunnel := &ServerTunnel{
		ID:         req.ReqID,
		ClientID:   clientID,
		LocalAddr:  fmt.Sprintf("localhost:%d", req.LocalPort),
		PublicURL:  publicURL,
		ServerPort: port,
		Domain:     domain,
		Status:     TunnelStatusActive,
		CreatedAt:  time.Now(),
	}

	s.tunnels.register(tunnel)

	// Send response
	_ = encoder.Encode(&protocol.TunnelResponse{
		ReqID:      req.ReqID,
		Success:    true,
		TunnelID:   req.ReqID,
		PublicURL:  publicURL,
		ServerPort: port,
	})

	s.events.emit(&TunnelEstablishedEvent{
		Tunnel: &Tunnel{
			ID:         tunnel.ID,
			LocalPort:  req.LocalPort,
			PublicURL:  publicURL,
			ServerPort: port,
			Status:     TunnelStatusActive,
			CreatedAt:  tunnel.CreatedAt,
		},
	})

	// Keep stream open for data
	// TODO: handle data forwarding
	<-ctx.Done()

	// Cleanup
	s.tunnels.unregister(tunnel.ID)
	s.domains.release(domain)
	s.ports.release(port)

	s.events.emit(&TunnelClosedEvent{TunnelID: tunnel.ID})
}

// Shutdown gracefully shuts down the server.
func (s *quicServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}
	s.running = false

	// Close listener
	if s.listener != nil {
		s.listener.Close()
		s.listener = nil
	}

	return nil
}

// runHTTPS runs the HTTPS server.
func (s *quicServer) runHTTPS(ctx context.Context) {
	// TODO: implement autocert HTTPS server
}

// IsRunning returns true if server is running.
func (s *quicServer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}
