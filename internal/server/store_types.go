package server

import (
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
