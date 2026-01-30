package tunnel

import (
	"sync"
	"time"
)

// EventType represents event types.
type EventType string

const (
	EventConnectionStateChanged EventType = "connection_state_changed"
	EventTunnelEstablished      EventType = "tunnel_established"
	EventTunnelClosed           EventType = "tunnel_closed"
	EventTunnelError            EventType = "tunnel_error"
	EventAuthTokenRefreshed     EventType = "auth_token_refreshed"
	EventTrafficStats           EventType = "traffic_stats"
)

// Event is the interface for all events.
type Event interface {
	Type() EventType
	Timestamp() time.Time
}

// EventHandler handles events.
type EventHandler func(Event)

// baseEvent provides common event functionality.
type baseEvent struct {
	TypeVal   EventType
	TimeStamp time.Time
}

// Type returns the event type.
func (e *baseEvent) Type() EventType {
	return e.TypeVal
}

// Timestamp returns the event timestamp.
func (e *baseEvent) Timestamp() time.Time {
	return e.TimeStamp
}

// ConnectionStateChangedEvent is emitted when connection state changes.
type ConnectionStateChangedEvent struct {
	baseEvent
	OldState ConnectionState
	NewState ConnectionState
	Error    error
}

// TunnelEstablishedEvent is emitted when a tunnel is established.
type TunnelEstablishedEvent struct {
	baseEvent
	Tunnel *Tunnel
}

// TunnelClosedEvent is emitted when a tunnel is closed.
type TunnelClosedEvent struct {
	baseEvent
	TunnelID string
}

// TunnelErrorEvent is emitted when a tunnel encounters an error.
type TunnelErrorEvent struct {
	baseEvent
	TunnelID string
	Error    error
}

// AuthTokenRefreshedEvent is emitted when the auth token is refreshed.
type AuthTokenRefreshedEvent struct {
	baseEvent
	Server string
}

// TrafficStatsEvent is emitted periodically with traffic stats.
type TrafficStatsEvent struct {
	baseEvent
	TunnelID    string
	BytesSent   int64
	BytesRecv   int64
	Connections int
}

// eventEmitter manages event handlers and event emission.
type eventEmitter struct {
	handlers map[EventType][]EventHandler
	mu       sync.RWMutex
}

// newEventEmitter creates a new event emitter.
func newEventEmitter() *eventEmitter {
	return &eventEmitter{
		handlers: make(map[EventType][]EventHandler),
	}
}

// on registers an event handler.
func (e *eventEmitter) on(eventType EventType, handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[eventType] = append(e.handlers[eventType], handler)
}

// off unregisters an event handler.
func (e *eventEmitter) off(eventType EventType, handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()

	handlers := e.handlers[eventType]
	for i, h := range handlers {
		if &h == &handler {
			e.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
			return
		}
	}
}

// emit emits an event to all registered handlers.
func (e *eventEmitter) emit(event Event) {
	e.mu.RLock()
	handlers := e.handlers[event.Type()]
	e.mu.RUnlock()

	for _, handler := range handlers {
		go handler(event)
	}
}
