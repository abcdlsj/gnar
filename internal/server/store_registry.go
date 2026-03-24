package server

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/abcdlsj/gnar/internal/norm"
	"github.com/abcdlsj/gnar/pkg/api"
)

func (s *Store) Register(req api.RegisterTunnelRequest) (*api.RegisterTunnelResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tenant := norm.Tenant(req.Tenant)
	name := norm.Name(req.Name)
	if name == "" {
		name = "tunnel"
	}

	if tunnelID, ok := s.bySlug[tenant+"/"+name]; ok {
		if tunnel := s.byID[tunnelID]; tunnel != nil {
			s.removeLocked(tunnel.SessionID)
		}
	}

	slug := s.nextSlug(tenant, name)
	domains := norm.Domains(req.Domains)
	if err := validateDomains(domains, tenant, s.cfg); err != nil {
		return nil, err
	}
	if len(domains) == 0 && s.cfg.BaseDomain != "" {
		domains = []string{slug + "." + norm.Host(s.cfg.BaseDomain)}
	}

	for _, domain := range domains {
		if _, exists := s.byHost[domain]; exists {
			return nil, fmt.Errorf("domain already in use: %s", domain)
		}
	}

	now := time.Now()
	tunnelID := nextID()
	sessionID := nextID()
	urls := []string{joinPathURL(s.cfg.PublicURL, "/t/"+tenant+"/"+slug)}
	for _, domain := range domains {
		urls = append(urls, joinHostURL(s.cfg.PublicURL, domain))
	}

	publicURL := urls[0]
	if len(domains) > 0 {
		publicURL = joinHostURL(s.cfg.PublicURL, domains[0])
	}

	tunnel := &Tunnel{
		ID:        tunnelID,
		SessionID: sessionID,
		Tenant:    tenant,
		Name:      slug,
		Slug:      slug,
		Target:    req.Target,
		Domains:   domains,
		URLs:      urls,
		PublicURL: publicURL,
		CreatedAt: now,
		LastSeen:  now,
	}

	session := &Session{
		ID:       sessionID,
		TunnelID: tunnelID,
		events:   make(chan api.AgentEvent, 128),
		pending:  make(map[string]*PendingRequest),
		lastSeen: now,
	}

	s.byID[tunnelID] = tunnel
	s.bySlug[tenant+"/"+slug] = tunnelID
	for _, domain := range domains {
		s.byHost[domain] = tunnelID
	}
	s.sess[sessionID] = session

	return &api.RegisterTunnelResponse{
		SessionID: sessionID,
		TunnelID:  tunnelID,
		Tenant:    tenant,
		Name:      slug,
		PublicURL: publicURL,
		URLs:      urls,
	}, nil
}

func (s *Store) Resolve(host, requestPath string) (*Tunnel, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if tunnelID, ok := s.byHost[norm.Host(host)]; ok {
		tunnel := s.byID[tunnelID]
		if tunnel == nil {
			return nil, "", errors.New("tunnel not found")
		}
		return tunnel, requestPath, nil
	}

	tenant, slug, forwardedPath, ok := extractTenantSlug(requestPath)
	if !ok {
		return nil, "", errors.New("tunnel not found")
	}

	tunnelID, ok := s.bySlug[tenant+"/"+slug]
	if !ok {
		return nil, "", errors.New("tunnel not found")
	}

	tunnel := s.byID[tunnelID]
	if tunnel == nil {
		return nil, "", errors.New("tunnel not found")
	}

	return tunnel, forwardedPath, nil
}

func (s *Store) List(tenant string) []api.TunnelSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tunnels := make([]api.TunnelSummary, 0, len(s.byID))
	for _, tunnel := range s.byID {
		if tenant != "" && tunnel.Tenant != tenant {
			continue
		}
		tunnels = append(tunnels, snapshotTunnel(tunnel))
	}

	sort.Slice(tunnels, func(i, j int) bool {
		return tunnels[i].CreatedAt.Before(tunnels[j].CreatedAt)
	})
	return tunnels
}

func (s *Store) Detail(tenant, ref string) (api.TunnelDetailResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tunnel := s.findTunnelLocked(tenant, ref)
	if tunnel == nil {
		return api.TunnelDetailResponse{}, errors.New("tunnel not found")
	}

	logs := make([]api.RequestLogEntry, len(tunnel.RecentRequests))
	copy(logs, tunnel.RecentRequests)
	for left, right := 0, len(logs)-1; left < right; left, right = left+1, right-1 {
		logs[left], logs[right] = logs[right], logs[left]
	}

	return api.TunnelDetailResponse{
		Tunnel:         snapshotTunnel(tunnel),
		RecentRequests: logs,
	}, nil
}

func (s *Store) findTunnelLocked(tenant, ref string) *Tunnel {
	ref = norm.Name(ref)
	if ref == "" {
		return nil
	}

	if tunnel, ok := s.byID[ref]; ok {
		if tenant == "" || tunnel.Tenant == tenant {
			return tunnel
		}
	}

	if tenant != "" {
		key := norm.Tenant(tenant) + "/" + ref
		if tunnelID, ok := s.bySlug[key]; ok {
			return s.byID[tunnelID]
		}
	}

	for _, tunnel := range s.byID {
		if tunnel.Name == ref && (tenant == "" || tunnel.Tenant == tenant) {
			return tunnel
		}
	}
	return nil
}

func snapshotTunnel(tunnel *Tunnel) api.TunnelSummary {
	domains := make([]string, len(tunnel.Domains))
	copy(domains, tunnel.Domains)
	urls := make([]string, len(tunnel.URLs))
	copy(urls, tunnel.URLs)

	return api.TunnelSummary{
		ID:             tunnel.ID,
		Tenant:         tunnel.Tenant,
		Name:           tunnel.Name,
		Slug:           tunnel.Slug,
		Target:         tunnel.Target,
		Domains:        domains,
		URLs:           urls,
		PublicURL:      tunnel.PublicURL,
		Status:         "connected",
		CreatedAt:      tunnel.CreatedAt,
		LastSeen:       tunnel.LastSeen,
		TotalRequests:  tunnel.TotalRequests,
		ActiveRequests: tunnel.ActiveRequests,
		LastError:      tunnel.LastError,
		LastStatusCode: tunnel.LastStatusCode,
	}
}

func (s *Store) nextSlug(tenant, base string) string {
	slug := base
	if _, exists := s.bySlug[tenant+"/"+slug]; !exists {
		return slug
	}

	for i := 2; ; i++ {
		candidate := slug + "-" + strconv.Itoa(i)
		if _, exists := s.bySlug[tenant+"/"+candidate]; !exists {
			return candidate
		}
	}
}
