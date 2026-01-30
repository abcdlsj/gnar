// Package tunnel provides a reusable tunnel library for exposing local services.
package tunnel

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ClientConfig configures the tunnel client.
type ClientConfig struct {
	ServerAddr string
	QUIC       QUICConfig
	AuthStore  AuthStore
}

// QUICConfig configures QUIC transport.
type QUICConfig struct {
	TLSCert          string
	TLSKey           string
	Port             int
	IdleTimeout      time.Duration
	HandshakeTimeout time.Duration
}

// ExposeOptions provides options for exposing a local port.
type ExposeOptions struct {
	Subdomain string
	Protocol  string // http or https
}

// ConnectionState represents the client's connection state.
type ConnectionState int

const (
	Disconnected ConnectionState = iota
	Connecting
	Connected
	Reconnecting
)

// Client is the tunnel client.
type Client struct {
	config  ClientConfig
	auth    *authManager
	tunnels map[string]*Tunnel
	events  *eventEmitter
	state   ConnectionState
	mu      sync.RWMutex
}

// NewClient creates a new tunnel client.
func NewClient(cfg ClientConfig) *Client {
	return &Client{
		config:  cfg,
		auth:    newAuthManager(cfg.AuthStore),
		tunnels: make(map[string]*Tunnel),
		events:  newEventEmitter(),
		state:   Disconnected,
	}
}

// Auth authenticates with the server using the provided token.
func (c *Client) Auth(ctx context.Context, token string) error {
	return c.auth.authenticate(ctx, c.config.ServerAddr, token)
}

// IsAuthenticated returns true if the client is authenticated.
func (c *Client) IsAuthenticated() bool {
	return c.auth.isAuthenticated()
}

// Connect establishes connection to the server.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == Connected {
		return nil
	}

	c.state = Connecting
	c.events.emit(&ConnectionStateChangedEvent{
		OldState: Disconnected,
		NewState: Connecting,
	})

	// TODO: implement actual QUIC connection

	c.state = Connected
	c.events.emit(&ConnectionStateChangedEvent{
		OldState: Connecting,
		NewState: Connected,
	})

	return nil
}

// Disconnect closes the connection.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == Disconnected {
		return nil
	}

	oldState := c.state
	c.state = Disconnected
	c.events.emit(&ConnectionStateChangedEvent{
		OldState: oldState,
		NewState: Disconnected,
	})

	return nil
}

// ConnectionState returns the current connection state.
func (c *Client) ConnectionState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Expose exposes a local port.
func (c *Client) Expose(ctx context.Context, localPort int, opts ExposeOptions) (*Tunnel, error) {
	if !c.IsAuthenticated() {
		return nil, ErrNotAuthenticated
	}

	if c.ConnectionState() != Connected {
		if err := c.Connect(ctx); err != nil {
			return nil, err
		}
	}

	tunnelID := uuid.New().String()
	tunnel := &Tunnel{
		ID:        tunnelID,
		LocalPort: localPort,
		Status:    TunnelStatusPending,
		client:    c,
		CreatedAt: time.Now(),
	}

	c.mu.Lock()
	c.tunnels[tunnelID] = tunnel
	c.mu.Unlock()

	// TODO: implement actual tunnel establishment

	c.events.emit(&TunnelEstablishedEvent{Tunnel: tunnel})
	return tunnel, nil
}

// CloseTunnel closes a specific tunnel.
func (c *Client) CloseTunnel(tunnelID string) error {
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

// Tunnels returns all active tunnels.
func (c *Client) Tunnels() []*Tunnel {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tunnels := make([]*Tunnel, 0, len(c.tunnels))
	for _, t := range c.tunnels {
		tunnels = append(tunnels, t)
	}
	return tunnels
}

// OnEvent subscribes to events.
func (c *Client) OnEvent(eventType EventType, handler EventHandler) {
	c.events.on(eventType, handler)
}

// OffEvent unsubscribes from events.
func (c *Client) OffEvent(eventType EventType, handler EventHandler) {
	c.events.off(eventType, handler)
}

// Close closes the client and all tunnels.
func (c *Client) Close() error {
	c.Disconnect()

	c.mu.Lock()
	tunnels := make([]*Tunnel, 0, len(c.tunnels))
	for _, t := range c.tunnels {
		tunnels = append(tunnels, t)
	}
	c.tunnels = make(map[string]*Tunnel)
	c.mu.Unlock()

	for _, t := range tunnels {
		t.Close()
	}

	return nil
}
