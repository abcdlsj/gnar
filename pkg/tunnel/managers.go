package tunnel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

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
	CreatedAt int64
}

func newDomainManager(baseDomain string, randomLen int) *domainManager {
	return &domainManager{
		baseDomain:  baseDomain,
		randomLen:   randomLen,
		usedDomains: make(map[string]*domainInfo),
	}
}

// allocate allocates a domain for a tunnel.
func (m *domainManager) allocate(subdomain, tunnelID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If no subdomain requested, generate random
	if subdomain == "" {
		subdomain = m.generateRandomSubdomain()
	}

	fullDomain := fmt.Sprintf("%s.%s", subdomain, m.baseDomain)

	// Check if already in use
	if info, exists := m.usedDomains[fullDomain]; exists && info.TunnelID != tunnelID {
		return "", fmt.Errorf("domain already in use")
	}

	// Allocate
	m.usedDomains[fullDomain] = &domainInfo{
		Domain:    fullDomain,
		TunnelID:  tunnelID,
		CreatedAt: 0, // TODO: set timestamp
	}

	return fullDomain, nil
}

// release releases a domain.
func (m *domainManager) release(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.usedDomains, domain)
}

// isAvailable checks if domain is available.
func (m *domainManager) isAvailable(domain string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.usedDomains[domain]
	return !exists
}

// generateRandomSubdomain generates a random subdomain.
func (m *domainManager) generateRandomSubdomain() string {
	bytes := make([]byte, m.randomLen/2)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// portManager manages port allocation.
type portManager struct {
	startPort int
	endPort   int
	usedPorts map[int]string // port -> clientID
	mu        sync.RWMutex
}

func newPortManager(start, end int) *portManager {
	return &portManager{
		startPort: start,
		endPort:   end,
		usedPorts: make(map[int]string),
	}
}

// allocate allocates an available port.
func (m *portManager) allocate(clientID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for port := m.startPort; port <= m.endPort; port++ {
		if _, used := m.usedPorts[port]; !used {
			m.usedPorts[port] = clientID
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available port")
}

// release releases a port.
func (m *portManager) release(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.usedPorts, port)
}

// tunnelManager manages server-side tunnels.
type tunnelManager struct {
	tunnels map[string]*ServerTunnel
	mu      sync.RWMutex
}

func newTunnelManager() *tunnelManager {
	return &tunnelManager{
		tunnels: make(map[string]*ServerTunnel),
	}
}

// register registers a tunnel.
func (m *tunnelManager) register(tunnel *ServerTunnel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tunnels[tunnel.ID] = tunnel
}

// unregister unregisters a tunnel.
func (m *tunnelManager) unregister(tunnelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tunnels, tunnelID)
}

// get gets a tunnel by ID.
func (m *tunnelManager) get(tunnelID string) *ServerTunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tunnels[tunnelID]
}

// listByClient lists tunnels for a client.
func (m *tunnelManager) listByClient(clientID string) []*ServerTunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*ServerTunnel
	for _, t := range m.tunnels {
		if t.ClientID == clientID {
			result = append(result, t)
		}
	}
	return result
}
