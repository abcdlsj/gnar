package tunnel

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"

	"github.com/abcdlsj/gnar/pkg/tunnel/protocol"
)

// quicClient implements QUIC transport for the tunnel client.
type quicClient struct {
	config  ClientConfig
	conn    quic.Connection
	authMgr *authManager
	tunnels map[string]*quicTunnel
	events  *eventEmitter
	state   ConnectionState
	mu      sync.RWMutex
}

// quicTunnel represents a single tunnel over QUIC.
type quicTunnel struct {
	id        string
	client    *quicClient
	stream    quic.Stream
	localPort int
	publicURL string
	status    TunnelStatus
	stats     TunnelStats
	mu        sync.RWMutex
}

// newQUICClient creates a new QUIC-based tunnel client.
func newQUICClient(cfg ClientConfig) *quicClient {
	return &quicClient{
		config:  cfg,
		authMgr: newAuthManager(cfg.AuthStore),
		tunnels: make(map[string]*quicTunnel),
		events:  newEventEmitter(),
		state:   Disconnected,
	}
}

// Auth authenticates with the server.
func (c *quicClient) Auth(ctx context.Context, token string) error {
	return c.authMgr.authenticate(ctx, c.config.ServerAddr, token)
}

// IsAuthenticated returns true if authenticated.
func (c *quicClient) IsAuthenticated() bool {
	return c.authMgr.isAuthenticated()
}

// Connect establishes QUIC connection.
func (c *quicClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.state == Connected {
		c.mu.Unlock()
		return nil
	}
	c.state = Connecting
	c.mu.Unlock()

	c.events.emit(&ConnectionStateChangedEvent{
		OldState: Disconnected,
		NewState: Connecting,
	})

	// Ensure we have a valid token
	token, err := c.authMgr.getAccessToken(ctx)
	if err != nil {
		c.setState(Disconnected)
		c.events.emit(&ConnectionStateChangedEvent{
			OldState: Connecting,
			NewState: Disconnected,
			Error:    err,
		})
		return err
	}

	// Parse server address
	addr := c.config.ServerAddr
	if _, err := url.Parse(addr); err != nil {
		addr = fmt.Sprintf("quic://%s", addr)
	}

	// Create TLS config
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // TODO: configure properly
		NextProtos:         []string{"gnar-tunnel"},
	}

	// Establish QUIC connection
	conn, err := quic.DialAddr(ctx, addr, tlsConfig, &quic.Config{
		MaxIdleTimeout:       c.config.QUIC.IdleTimeout,
		HandshakeIdleTimeout: c.config.QUIC.HandshakeTimeout,
	})
	if err != nil {
		c.setState(Disconnected)
		c.events.emit(&ConnectionStateChangedEvent{
			OldState: Connecting,
			NewState: Disconnected,
			Error:    err,
		})
		return fmt.Errorf("dial: %w", err)
	}

	// Open auth stream and authenticate
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		conn.CloseWithError(0, "auth failed")
		c.setState(Disconnected)
		return fmt.Errorf("open auth stream: %w", err)
	}

	encoder := protocol.NewEncoder(stream)
	decoder := protocol.NewDecoder(stream)

	// Send auth packet
	authPkt := &protocol.AuthPacket{
		AccessToken: token,
		Version:     "2.0.0",
	}
	if err := encoder.Encode(authPkt); err != nil {
		stream.CancelWrite(0)
		conn.CloseWithError(0, "auth failed")
		c.setState(Disconnected)
		return fmt.Errorf("send auth: %w", err)
	}

	// Read auth response
	resp, _, err := decoder.Decode()
	if err != nil {
		stream.CancelWrite(0)
		conn.CloseWithError(0, "auth failed")
		c.setState(Disconnected)
		return fmt.Errorf("read auth resp: %w", err)
	}

	authResp, ok := resp.(*protocol.AuthResponse)
	if !ok {
		stream.CancelWrite(0)
		conn.CloseWithError(0, "auth failed")
		c.setState(Disconnected)
		return fmt.Errorf("unexpected packet type")
	}

	if !authResp.Success {
		stream.CancelWrite(0)
		conn.CloseWithError(0, "auth failed")
		c.setState(Disconnected)
		return &Error{Code: ErrCodeAuthFailed, Message: authResp.Error}
	}

	// Auth successful
	stream.Close()

	c.mu.Lock()
	c.conn = conn
	c.state = Connected
	c.mu.Unlock()

	// Start connection maintenance
	go c.maintainConnection()

	c.events.emit(&ConnectionStateChangedEvent{
		OldState: Connecting,
		NewState: Connected,
	})

	return nil
}

// Expose creates a new tunnel.
func (c *quicClient) Expose(ctx context.Context, localPort int, opts ExposeOptions) (*quicTunnel, error) {
	if !c.IsAuthenticated() {
		return nil, ErrNotAuthenticated
	}

	if c.ConnectionState() != Connected {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}

	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil, ErrConnectionClosed
	}

	// Open a new stream for this tunnel
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}

	tunnelID := uuid.New().String()
	tunnel := &quicTunnel{
		id:        tunnelID,
		client:    c,
		stream:    stream,
		localPort: localPort,
		status:    TunnelStatusPending,
		stats:     TunnelStats{LastActive: time.Now()},
	}

	c.mu.Lock()
	c.tunnels[tunnelID] = tunnel
	c.mu.Unlock()

	// Send tunnel request
	encoder := protocol.NewEncoder(stream)
	decoder := protocol.NewDecoder(stream)

	req := &protocol.TunnelRequest{
		ReqID:     tunnelID,
		LocalPort: localPort,
		Subdomain: opts.Subdomain,
		Protocol:  opts.Protocol,
	}

	if err := encoder.Encode(req); err != nil {
		tunnel.Close()
		return nil, fmt.Errorf("send tunnel req: %w", err)
	}

	// Read response
	resp, _, err := decoder.Decode()
	if err != nil {
		tunnel.Close()
		return nil, fmt.Errorf("read tunnel resp: %w", err)
	}

	tunnelResp, ok := resp.(*protocol.TunnelResponse)
	if !ok {
		tunnel.Close()
		return nil, fmt.Errorf("unexpected response type")
	}

	if !tunnelResp.Success {
		tunnel.Close()
		code := ErrCodeTunnelFailed
		if tunnelResp.Error == "domain already in use" {
			code = ErrCodeDomainTaken
		}
		return nil, &Error{Code: code, Message: tunnelResp.Error}
	}

	// Tunnel established
	tunnel.publicURL = tunnelResp.PublicURL
	tunnel.mu.Lock()
	tunnel.status = TunnelStatusActive
	tunnel.mu.Unlock()

	c.events.emit(&TunnelEstablishedEvent{
		Tunnel: &Tunnel{
			ID:         tunnelID,
			LocalPort:  localPort,
			PublicURL:  tunnelResp.PublicURL,
			ServerPort: tunnelResp.ServerPort,
			Status:     TunnelStatusActive,
			Stats:      &tunnel.stats,
		},
	})

	// Start handling data
	go tunnel.handleData()

	return tunnel, nil
}

// CloseTunnel closes a specific tunnel.
func (c *quicClient) CloseTunnel(tunnelID string) error {
	c.mu.Lock()
	tunnel, ok := c.tunnels[tunnelID]
	c.mu.Unlock()

	if !ok {
		return nil
	}

	if err := tunnel.Close(); err != nil {
		return err
	}

	c.mu.Lock()
	delete(c.tunnels, tunnelID)
	c.mu.Unlock()

	c.events.emit(&TunnelClosedEvent{TunnelID: tunnelID})
	return nil
}

// Disconnect closes the connection.
func (c *quicClient) Disconnect() error {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	tunnels := make(map[string]*quicTunnel)
	for k, v := range c.tunnels {
		tunnels[k] = v
	}
	c.tunnels = make(map[string]*quicTunnel)
	oldState := c.state
	c.state = Disconnected
	c.mu.Unlock()

	// Close all tunnels
	for _, t := range tunnels {
		t.Close()
	}

	// Close connection
	if conn != nil {
		conn.CloseWithError(0, "client disconnect")
	}

	if oldState != Disconnected {
		c.events.emit(&ConnectionStateChangedEvent{
			OldState: oldState,
			NewState: Disconnected,
		})
	}

	return nil
}

// ConnectionState returns current state.
func (c *quicClient) ConnectionState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// setState updates state safely.
func (c *quicClient) setState(state ConnectionState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = state
}

// maintainConnection keeps connection alive.
func (c *quicClient) maintainConnection() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return
	}

	for {
		select {
		case <-ticker.C:
			c.mu.RLock()
			currentConn := c.conn
			c.mu.RUnlock()

			if currentConn == nil {
				return
			}

			// TODO: send heartbeat

		case <-conn.Context().Done():
			c.Disconnect()
			return
		}
	}
}

// handleData processes incoming data for the tunnel.
func (t *quicTunnel) handleData() {
	// TODO: implement HTTP proxy logic
	// Read from stream -> forward to local port
	// Read from local port -> write to stream
}

// Close closes the tunnel.
func (t *quicTunnel) Close() error {
	t.mu.Lock()
	if t.status == TunnelStatusClosed {
		t.mu.Unlock()
		return nil
	}
	t.status = TunnelStatusClosed
	stream := t.stream
	t.mu.Unlock()

	if stream != nil {
		stream.CancelWrite(0)
		stream.CancelRead(0)
	}

	return nil
}
