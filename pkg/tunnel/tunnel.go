package tunnel

import (
	"sync"
	"time"
)

// TunnelStatus represents the status of a tunnel.
type TunnelStatus int

const (
	TunnelStatusPending TunnelStatus = iota
	TunnelStatusActive
	TunnelStatusClosed
	TunnelStatusError
)

// Tunnel represents an established tunnel.
type Tunnel struct {
	ID         string
	LocalPort  int
	PublicURL  string
	ServerPort int
	Status     TunnelStatus
	CreatedAt  time.Time
	Stats      *TunnelStats

	client *Client
	mu     sync.RWMutex
}

// TunnelStats contains traffic statistics.
type TunnelStats struct {
	BytesSent   int64
	BytesRecv   int64
	Connections int
	LastActive  time.Time
}

// Close closes the tunnel.
func (t *Tunnel) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Status == TunnelStatusClosed {
		return nil
	}

	t.Status = TunnelStatusClosed
	return nil
}

// GetStatus returns the tunnel status.
func (t *Tunnel) GetStatus() TunnelStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

// GetStats returns tunnel statistics.
func (t *Tunnel) GetStats() TunnelStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.Stats == nil {
		return TunnelStats{}
	}
	return *t.Stats
}
