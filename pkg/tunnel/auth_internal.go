package tunnel

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// authManager manages client-side authentication.
type authManager struct {
	store      AuthStore
	credential *Credential
	mu         sync.RWMutex
}

func newAuthManager(store AuthStore) *authManager {
	return &authManager{
		store: store,
	}
}

// isAuthenticated returns true if we have valid credentials.
func (a *authManager) isAuthenticated() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.credential != nil && a.credential.RefreshToken != ""
}

// authenticate performs initial authentication.
func (a *authManager) authenticate(ctx context.Context, server, token string) error {
	// For now, simplified: token is both refresh and access token
	// In production, exchange token with server for refresh token
	cred := &Credential{
		Server:       server,
		RefreshToken: token,
		AccessToken:  token,
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	// Save to store if available
	if a.store != nil {
		if err := a.store.Save(cred); err != nil {
			return fmt.Errorf("save credential: %w", err)
		}
	}

	a.mu.Lock()
	a.credential = cred
	a.mu.Unlock()

	return nil
}

// getAccessToken returns a valid access token, refreshing if needed.
func (a *authManager) getAccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.credential == nil {
		// Try to load from store
		if a.store != nil {
			// We need server address to load - this is a simplification
			// In practice, we'd need to track current server
		}
		return "", ErrNotAuthenticated
	}

	// Check if expired
	if time.Now().After(a.credential.ExpiresAt) {
		// Need to refresh
		// TODO: implement refresh flow with server
		return "", ErrTokenExpired
	}

	return a.credential.AccessToken, nil
}

// authHandler manages server-side authentication.
type authHandler struct {
	tokens     map[string]*tokenInfo // access token -> info
	refreshTTL time.Duration
	accessTTL  time.Duration
	mu         sync.RWMutex
}

type tokenInfo struct {
	ClientID  string
	Token     string
	CreatedAt time.Time
}

func newAuthHandler() *authHandler {
	return &authHandler{
		tokens:     make(map[string]*tokenInfo),
		refreshTTL: 365 * 24 * time.Hour,
		accessTTL:  time.Hour,
	}
}

// validateToken checks if token is valid.
func (h *authHandler) validateToken(token string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	info, exists := h.tokens[token]
	if !exists {
		return false
	}
	// Check expiration
	return time.Since(info.CreatedAt) < h.accessTTL
}

// getClientID gets client ID from token.
func (h *authHandler) getClientID(token string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if info, exists := h.tokens[token]; exists {
		return info.ClientID
	}
	return ""
}

// addToken adds a new token.
func (h *authHandler) addToken(clientID, token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tokens[token] = &tokenInfo{
		ClientID:  clientID,
		Token:     token,
		CreatedAt: time.Now(),
	}
}
