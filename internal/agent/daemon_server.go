package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/abcdlsj/gnar/internal/norm"
)

type DaemonServer struct {
	listenAddr string
	daemon     *Daemon
	server     *http.Server
}

func NewDaemonServer(listenAddr, statePath string) *DaemonServer {
	daemon := NewDaemon(statePath)
	mux := http.NewServeMux()
	s := &DaemonServer{
		listenAddr: listenAddr,
		daemon:     daemon,
		server: &http.Server{
			Addr:              listenAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}

	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/v1/tunnels", s.handleTunnels)
	mux.HandleFunc("/api/v1/tunnels/", s.handleTunnelByName)
	return s
}

func (s *DaemonServer) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	if err := s.daemon.Restore(); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		err := s.server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *DaemonServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDaemonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, _ = w.Write([]byte("ok"))
}

func (s *DaemonServer) handleTunnels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeDaemonJSON(w, http.StatusOK, ListManagedResponse{Tunnels: s.daemon.List()})
	case http.MethodPost:
		var req StartTunnelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeDaemonError(w, http.StatusBadRequest, "invalid request")
			return
		}

		cfg := Config{
			ServerURL:        req.ServerURL,
			TargetURL:        req.TargetURL,
			Tenant:           req.Tenant,
			Name:             req.Name,
			Domains:          req.Domains,
			Token:            req.Token,
			RequestTimeout:   req.RequestTimeout,
			PollRetryBackoff: req.PollRetryBackoff,
			MaxResponseBytes: req.MaxResponseBytes,
		}

		runCtx, cancel := context.WithTimeout(r.Context(), cfg.RequestTimeout+5*time.Second)
		defer cancel()
		tunnel, err := s.daemon.Start(runCtx, cfg)
		if err != nil {
			writeDaemonError(w, http.StatusConflict, err.Error())
			return
		}
		writeDaemonJSON(w, http.StatusCreated, StartTunnelResponse{Tunnel: tunnel})
	default:
		writeDaemonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *DaemonServer) handleTunnelByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/tunnels/")
	name = strings.Trim(name, "/")
	if name == "" {
		writeDaemonError(w, http.StatusBadRequest, "missing tunnel name")
		return
	}

	tenant := norm.Tenant(r.URL.Query().Get("tenant"))

	switch r.Method {
	case http.MethodGet:
		tunnel, err := s.daemon.Get(tenant, name)
		if err != nil {
			writeDaemonError(w, http.StatusNotFound, err.Error())
			return
		}
		writeDaemonJSON(w, http.StatusOK, StartTunnelResponse{Tunnel: tunnel})
	case http.MethodDelete:
		stopCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := s.daemon.Stop(stopCtx, tenant, name); err != nil {
			writeDaemonError(w, http.StatusNotFound, err.Error())
			return
		}
		writeDaemonJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
	default:
		writeDaemonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func writeDaemonJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDaemonError(w http.ResponseWriter, status int, message string) {
	writeDaemonJSON(w, status, map[string]string{"error": message})
}
