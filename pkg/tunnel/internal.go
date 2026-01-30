package tunnel

import (
	"context"
	"sync"
	"time"
)

// eventEmitter handles event subscription and emission.
type eventEmitter struct {
	handlers map[EventType][]EventHandler
	mu       sync.RWMutex
}

func newEventEmitter() *eventEmitter {
	return &eventEmitter{
		handlers: make(map[EventType][]EventHandler),
	}
}

func (e *eventEmitter) on(eventType EventType, handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[eventType] = append(e.handlers[eventType], handler)
}

func (e *eventEmitter) off(eventType EventType, handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()

	handlers := e.handlers[eventType]
	for i, h := range handlers {
		// Compare function pointers
		if &h == &handler {
			e.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
}

func (e *eventEmitter) emit(event Event) {
	e.mu.RLock()
	handlers := e.handlers[event.Type()]
	e.mu.RUnlock()

	for _, handler := range handlers {
		go handler(event)
	}
}

// authManager manages authentication.
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

func (a *authManager) isAuthenticated() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.credential != nil && a.credential.isValid()
}

func (a *authManager) authenticate(ctx context.Context, server, token string) error {
	// TODO: implement actual authentication with server
	cred := &Credential{
		Server:       server,
		RefreshToken: token, // simplified for now
		AccessToken:  token,
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	if a.store != nil {
		if err := a.store.Save(cred); err != nil {
			return err
		}
	}

	a.mu.Lock()
	a.credential = cred
	a.mu.Unlock()

	return nil
}

func (a *authManager) getAccessToken(ctx context.Context) (string, error) {
	a.mu.RLock()
	cred := a.credential
	a.mu.RUnlock()

	if cred == nil {
		return "", ErrNotAuthenticated
	}

	if cred.isExpired() {
		// TODO: implement refresh flow
		return "", ErrTokenExpired
	}

	return cred.AccessToken, nil
}
