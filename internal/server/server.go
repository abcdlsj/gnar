package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/abcdlsj/gnar/internal/httpx"
)

type Server struct {
	cfg   Config
	store StoreBackend
	http  *http.Server
}

func Run(ctx context.Context, cfg Config) error {
	return New(cfg).Run(ctx)
}

func New(cfg Config, opts ...Option) *Server {
	mux := http.NewServeMux()
	s := &Server{
		cfg:   cfg,
		store: NewStore(cfg),
		http: &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(s)
	}

	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/_gnar/debug/path-query/", s.handleDebugPathQuery)
	mux.HandleFunc("/_gnar/debug/host-path/", s.handleDebugHostPath)
	mux.HandleFunc("/_gnar/debug/method-path/", s.handleDebugMethodPath)
	mux.HandleFunc("/_gnar/debug/prefix-path/", s.handleDebugPrefixPath)
	mux.HandleFunc("/_gnar/debug/static/", s.handleDebugStatic)
	mux.HandleFunc("/api/v1/agent/register", s.handleRegister)
	mux.HandleFunc("/api/v1/agent/poll", s.handlePoll)
	mux.HandleFunc("/api/v1/agent/respond", s.handleRespond)
	mux.HandleFunc("/api/v1/agent/unregister", s.handleUnregister)
	mux.HandleFunc("/api/v1/tunnels", s.handleTunnels)
	mux.HandleFunc("/api/v1/tunnels/", s.handleTunnelByName)
	mux.HandleFunc("/", s.handlePublic)
	return s
}

func (s *Server) Run(ctx context.Context) error {
	log.Printf("gnar server listening on %s", s.cfg.ListenAddr)
	log.Printf("public origin %s", s.cfg.PublicURL)
	if s.cfg.BaseDomain != "" {
		log.Printf("base domain %s", s.cfg.BaseDomain)
	}

	errCh := make(chan error, 1)

	go s.store.Cleaner(ctx)
	go func() {
		err := s.http.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	fmt.Fprint(w, "ok")
}
