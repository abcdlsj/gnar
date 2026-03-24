package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abcdlsj/gnar/pkg/api"
)

type Tunnel struct {
	ID             string
	SessionID      string
	Tenant         string
	Name           string
	Slug           string
	Target         string
	Domains        []string
	URLs           []string
	PublicURL      string
	CreatedAt      time.Time
	LastSeen       time.Time
	TotalRequests  int
	ActiveRequests int
	LastError      string
	LastStatusCode int
	RecentRequests []api.RequestLogEntry
}

type Session struct {
	ID       string
	TunnelID string
	events   chan api.AgentEvent
	pending  map[string]*PendingRequest
	lastSeen time.Time
}

type PendingRequest struct {
	responseCh chan api.PostResponseRequest
	log        api.RequestLogEntry
}

type Store struct {
	cfg    Config
	mu     sync.RWMutex
	byID   map[string]*Tunnel
	byHost map[string]string
	bySlug map[string]string
	sess   map[string]*Session
}

func NewStore(cfg Config) *Store {
	return &Store{
		cfg:    cfg,
		byID:   make(map[string]*Tunnel),
		byHost: make(map[string]string),
		bySlug: make(map[string]string),
		sess:   make(map[string]*Session),
	}
}

func (s *Store) Register(req api.RegisterTunnelRequest) (*api.RegisterTunnelResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tenant := normalizeTenant(req.Tenant)
	name := normalizeName(req.Name)
	if name == "" {
		name = "tunnel"
	}

	if tunnelID, ok := s.bySlug[tenant+"/"+name]; ok {
		if tunnel := s.byID[tunnelID]; tunnel != nil {
			s.removeLocked(tunnel.SessionID)
		}
	}

	slug := s.nextSlug(tenant, name)
	domains := normalizeDomains(req.Domains)
	if err := validateDomains(domains, tenant, s.cfg); err != nil {
		return nil, err
	}
	if len(domains) == 0 && s.cfg.BaseDomain != "" {
		domains = []string{slug + "." + normalizeHost(s.cfg.BaseDomain)}
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

	if tunnelID, ok := s.byHost[normalizeHost(host)]; ok {
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

func (s *Store) Dispatch(sessionID, requestID string, event api.AgentEvent) (<-chan api.PostResponseRequest, error) {
	s.mu.Lock()
	session := s.sess[sessionID]
	if session == nil {
		s.mu.Unlock()
		return nil, errors.New("session offline")
	}

	responseCh := make(chan api.PostResponseRequest, 1)
	session.lastSeen = time.Now()
	tunnel := s.byID[session.TunnelID]
	if tunnel != nil {
		tunnel.LastSeen = session.lastSeen
		tunnel.TotalRequests++
		tunnel.ActiveRequests++
		tunnel.LastError = ""
	}

	pending := &PendingRequest{
		responseCh: responseCh,
		log: api.RequestLogEntry{
			RequestID:   requestID,
			StartedAt:   session.lastSeen,
			CompletedAt: time.Time{},
		},
	}
	if event.Request != nil {
		pending.log.Method = event.Request.Method
		pending.log.Path = event.Request.Path
		pending.log.Host = event.Request.Host
		pending.log.RemoteAddr = event.Request.RemoteAddr
	}
	session.pending[requestID] = pending
	s.mu.Unlock()

	select {
	case session.events <- event:
		return responseCh, nil
	default:
		s.mu.Lock()
		delete(session.pending, requestID)
		if tunnel != nil && tunnel.ActiveRequests > 0 {
			tunnel.ActiveRequests--
		}
		s.mu.Unlock()
		return nil, errors.New("agent queue is full")
	}
}

func (s *Store) WaitEvent(sessionID string, timeout time.Duration) (api.AgentEvent, error) {
	s.mu.Lock()
	session := s.sess[sessionID]
	if session == nil {
		s.mu.Unlock()
		return api.AgentEvent{}, errors.New("session offline")
	}
	session.lastSeen = time.Now()
	tunnel := s.byID[session.TunnelID]
	if tunnel != nil {
		tunnel.LastSeen = session.lastSeen
	}
	events := session.events
	s.mu.Unlock()

	select {
	case event := <-events:
		return event, nil
	case <-time.After(timeout):
		return api.AgentEvent{Type: api.EventNoop}, nil
	}
}

func (s *Store) Complete(sessionID string, response api.PostResponseRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := s.sess[sessionID]
	if session == nil {
		return errors.New("session offline")
	}

	pending, ok := session.pending[response.RequestID]
	if !ok {
		return errors.New("request not found")
	}

	delete(session.pending, response.RequestID)
	session.lastSeen = time.Now()
	tunnel := s.byID[session.TunnelID]
	if tunnel != nil {
		tunnel.LastSeen = session.lastSeen
		if tunnel.ActiveRequests > 0 {
			tunnel.ActiveRequests--
		}
		pending.log.StatusCode = response.StatusCode
		pending.log.CompletedAt = session.lastSeen
		pending.log.DurationMS = pending.log.CompletedAt.Sub(pending.log.StartedAt).Milliseconds()
		tunnel.LastStatusCode = response.StatusCode
		tunnel.LastError = ""
		s.appendLog(tunnel, pending.log)
	}
	pending.responseCh <- response
	close(pending.responseCh)
	return nil
}

func (s *Store) DropPending(sessionID, requestID, reason string, statusCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := s.sess[sessionID]
	if session == nil {
		return
	}

	pending, ok := session.pending[requestID]
	if !ok {
		return
	}

	delete(session.pending, requestID)
	session.lastSeen = time.Now()
	tunnel := s.byID[session.TunnelID]
	if tunnel != nil {
		tunnel.LastSeen = session.lastSeen
		if tunnel.ActiveRequests > 0 {
			tunnel.ActiveRequests--
		}
		pending.log.StatusCode = statusCode
		pending.log.CompletedAt = session.lastSeen
		pending.log.DurationMS = pending.log.CompletedAt.Sub(pending.log.StartedAt).Milliseconds()
		pending.log.Error = reason
		tunnel.LastStatusCode = statusCode
		tunnel.LastError = reason
		s.appendLog(tunnel, pending.log)
	}
	close(pending.responseCh)
}

func (s *Store) Remove(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeLocked(sessionID)
}

func (s *Store) Cleaner(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpired()
		}
	}
}

func (s *Store) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for sessionID, session := range s.sess {
		if now.Sub(session.lastSeen) <= s.cfg.IdleTimeout {
			continue
		}
		s.removeLocked(sessionID)
	}
}

func (s *Store) removeLocked(sessionID string) {
	session := s.sess[sessionID]
	if session == nil {
		return
	}

	delete(s.sess, sessionID)
	tunnel := s.byID[session.TunnelID]
	for requestID, pending := range session.pending {
		delete(session.pending, requestID)
		if tunnel != nil {
			if tunnel.ActiveRequests > 0 {
				tunnel.ActiveRequests--
			}
			pending.log.StatusCode = httpStatusTunnelClosed
			pending.log.CompletedAt = time.Now()
			pending.log.DurationMS = pending.log.CompletedAt.Sub(pending.log.StartedAt).Milliseconds()
			pending.log.Error = "session closed"
			tunnel.LastStatusCode = httpStatusTunnelClosed
			tunnel.LastError = "session closed"
			s.appendLog(tunnel, pending.log)
		}
		close(pending.responseCh)
	}

	if tunnel == nil {
		return
	}

	delete(s.byID, tunnel.ID)
	delete(s.bySlug, tunnel.Tenant+"/"+tunnel.Slug)
	for _, domain := range tunnel.Domains {
		delete(s.byHost, domain)
	}
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
	ref = normalizeName(ref)
	if ref == "" {
		return nil
	}

	if tunnel, ok := s.byID[ref]; ok {
		if tenant == "" || tunnel.Tenant == tenant {
			return tunnel
		}
	}

	if tenant != "" {
		key := normalizeTenant(tenant) + "/" + ref
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

func (s *Store) appendLog(tunnel *Tunnel, entry api.RequestLogEntry) {
	tunnel.RecentRequests = append(tunnel.RecentRequests, entry)
	if len(tunnel.RecentRequests) > 50 {
		tunnel.RecentRequests = tunnel.RecentRequests[len(tunnel.RecentRequests)-50:]
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

func extractTenantSlug(requestPath string) (string, string, string, bool) {
	if requestPath == "" || requestPath == "/" {
		return "", "", "", false
	}

	trimmed := strings.TrimPrefix(requestPath, "/")
	parts := strings.SplitN(trimmed, "/", 4)
	if len(parts) < 3 || parts[0] != "t" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}

	forwardedPath := "/"
	if len(parts) == 4 {
		forwardedPath = "/" + parts[3]
	}

	return normalizeTenant(parts[1]), parts[2], forwardedPath, true
}

func normalizeName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if lastDash || b.Len() == 0 {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		}
	}

	name := strings.Trim(b.String(), "-")
	if name == "" {
		return ""
	}
	return name
}

func normalizeTenant(value string) string {
	value = normalizeName(value)
	if value == "" {
		return "default"
	}
	return value
}

func normalizeHost(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err == nil {
			value = parsed.Host
		}
	}

	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}

	if strings.Count(value, ":") == 1 {
		host, _, _ := strings.Cut(value, ":")
		return host
	}

	return value
}

func normalizeDomains(domains []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(domains))
	for _, domain := range domains {
		normalized := normalizeHost(domain)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func normalizeSuffixes(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		value = strings.TrimPrefix(value, "*.")
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validateDomains(domains []string, tenant string, cfg Config) error {
	if len(domains) == 0 {
		return nil
	}

	allowed := append([]string(nil), cfg.AllowedDomainSuffixes...)
	allowed = append(allowed, cfg.TenantDomainSuffixes[tenant]...)
	if len(allowed) == 0 {
		return nil
	}

	for _, domain := range domains {
		allowedForDomain := false
		for _, suffix := range allowed {
			if domainHasSuffix(domain, suffix) {
				allowedForDomain = true
				break
			}
		}
		if !allowedForDomain {
			return fmt.Errorf("domain not allowed for tenant %s: %s", tenant, domain)
		}
	}
	return nil
}

func domainHasSuffix(domain, suffix string) bool {
	domain = strings.TrimSpace(strings.ToLower(domain))
	suffix = strings.TrimSpace(strings.ToLower(suffix))
	if domain == "" || suffix == "" {
		return false
	}
	return strings.HasSuffix(domain, suffix) && len(domain) > len(suffix)
}

func joinPathURL(origin, suffix string) string {
	base, err := url.Parse(origin)
	if err != nil {
		return origin + suffix
	}
	base.Path = path.Join(base.Path, suffix)
	if !strings.HasPrefix(base.Path, "/") {
		base.Path = "/" + base.Path
	}
	return base.String()
}

func joinHostURL(origin, host string) string {
	base, err := url.Parse(origin)
	if err != nil {
		return origin
	}
	base.Host = host
	base.Path = ""
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String()
}
