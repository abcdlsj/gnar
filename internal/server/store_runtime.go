package server

import (
	"context"
	"errors"
	"time"

	"github.com/abcdlsj/gnar/pkg/api"
)

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

func (s *Store) appendLog(tunnel *Tunnel, entry api.RequestLogEntry) {
	tunnel.RecentRequests = append(tunnel.RecentRequests, entry)
	if len(tunnel.RecentRequests) > 50 {
		tunnel.RecentRequests = tunnel.RecentRequests[len(tunnel.RecentRequests)-50:]
	}
}
