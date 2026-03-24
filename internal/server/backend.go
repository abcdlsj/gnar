package server

import (
	"context"
	"time"

	"github.com/abcdlsj/gnar/pkg/api"
)

type StoreBackend interface {
	Register(api.RegisterTunnelRequest) (*api.RegisterTunnelResponse, error)
	Resolve(string, string) (*Tunnel, string, error)
	Dispatch(string, string, api.AgentEvent) (<-chan api.PostResponseRequest, error)
	WaitEvent(string, time.Duration) (api.AgentEvent, error)
	Complete(string, api.PostResponseRequest) error
	DropPending(string, string, string, int)
	Remove(string)
	Cleaner(context.Context)
	List(string) []api.TunnelSummary
	Detail(string, string) (api.TunnelDetailResponse, error)
}

type Option func(*Server)

func WithStoreBackend(store StoreBackend) Option {
	return func(s *Server) {
		if store != nil {
			s.store = store
		}
	}
}
