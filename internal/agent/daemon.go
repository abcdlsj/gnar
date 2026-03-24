package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/abcdlsj/gnar/internal/norm"
	"github.com/abcdlsj/gnar/pkg/api"
)

type Daemon struct {
	mu        sync.RWMutex
	statePath string
	tunnels   map[string]*managedTunnel
}

type managedTunnel struct {
	key    string
	ctx    context.Context
	cfg    Config
	state  ManagedTunnel
	cancel context.CancelFunc
	done   chan struct{}
}

type persistedState struct {
	Tunnels []persistedTunnel `json:"tunnels"`
}

type persistedTunnel struct {
	ServerURL        string        `json:"server_url"`
	TargetURL        string        `json:"target_url"`
	Tenant           string        `json:"tenant"`
	Name             string        `json:"name"`
	Domains          []string      `json:"domains"`
	Token            string        `json:"token"`
	RequestTimeout   time.Duration `json:"request_timeout"`
	PollRetryBackoff time.Duration `json:"poll_retry_backoff"`
	MaxResponseBytes int64         `json:"max_response_bytes"`
}

func NewDaemon(statePath string) *Daemon {
	return &Daemon{
		statePath: statePath,
		tunnels:   make(map[string]*managedTunnel),
	}
}

func (d *Daemon) Start(ctx context.Context, cfg Config) (ManagedTunnel, error) {
	tunnel, err := d.add(cfg)
	if err != nil {
		return ManagedTunnel{}, err
	}

	ready := make(chan struct{}, 1)
	failed := make(chan error, 1)
	go d.runTunnel(tunnel, ready, failed, false)

	select {
	case <-ctx.Done():
		_ = d.Stop(context.Background(), cfg.Tenant, cfg.Name)
		return ManagedTunnel{}, ctx.Err()
	case err := <-failed:
		_ = d.Stop(context.Background(), cfg.Tenant, cfg.Name)
		return ManagedTunnel{}, err
	case <-ready:
		return d.Get(cfg.Tenant, cfg.Name)
	}
}

func (d *Daemon) Restore() error {
	state, err := d.loadState()
	if err != nil {
		return err
	}

	for _, entry := range state.Tunnels {
		cfg := Config{
			ServerURL:        entry.ServerURL,
			TargetURL:        entry.TargetURL,
			Tenant:           entry.Tenant,
			Name:             entry.Name,
			Domains:          append([]string(nil), entry.Domains...),
			Token:            entry.Token,
			RequestTimeout:   entry.RequestTimeout,
			PollRetryBackoff: entry.PollRetryBackoff,
			MaxResponseBytes: entry.MaxResponseBytes,
		}
		tunnel, err := d.add(cfg)
		if err != nil {
			continue
		}
		go d.runTunnel(tunnel, nil, nil, true)
	}

	return nil
}

func (d *Daemon) add(cfg Config) (*managedTunnel, error) {
	cfg.Tenant = norm.Tenant(cfg.Tenant)
	cfg.Name = norm.Name(cfg.Name)
	if cfg.Name == "" {
		return nil, errors.New("name is required")
	}

	cfg.Tenant = norm.Tenant(cfg.Tenant)
	key := managedKey(cfg.Tenant, cfg.Name)

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.tunnels[key]; exists {
		return nil, errors.New("tunnel already managed")
	}

	now := time.Now()
	runCtx, cancel := context.WithCancel(context.Background())
	tunnel := &managedTunnel{
		key: key,
		ctx: runCtx,
		cfg: cfg,
		state: ManagedTunnel{
			Tenant:    cfg.Tenant,
			Name:      cfg.Name,
			TargetURL: cfg.TargetURL,
			ServerURL: cfg.ServerURL,
			Domains:   append([]string(nil), cfg.Domains...),
			Status:    "starting",
			CreatedAt: now,
			UpdatedAt: now,
		},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	d.tunnels[key] = tunnel
	if err := d.saveLocked(); err != nil {
		delete(d.tunnels, key)
		cancel()
		return nil, err
	}
	return tunnel, nil
}

func (d *Daemon) runTunnel(tunnel *managedTunnel, ready chan<- struct{}, failed chan<- error, restart bool) {
	defer close(tunnel.done)

	var readyOnce sync.Once
	var failureSent bool
	notifyReady := func() {
		if ready == nil {
			return
		}
		readyOnce.Do(func() {
			ready <- struct{}{}
		})
	}

	for {
		err := New(tunnel.cfg).RunWithHooks(tunnel.ctx, RunnerHooks{
			OnRegistered: func(reg *api.RegisterTunnelResponse) {
				d.mu.Lock()
				tunnel.state.Tenant = reg.Tenant
				tunnel.state.PublicURL = reg.PublicURL
				tunnel.state.URLs = append([]string(nil), reg.URLs...)
				tunnel.state.Status = "connected"
				tunnel.state.LastError = ""
				tunnel.state.UpdatedAt = time.Now()
				tunnel.state.ConnectedAt = tunnel.state.UpdatedAt
				d.mu.Unlock()
				notifyReady()
			},
			OnPollError: func(err error) {
				d.mu.Lock()
				tunnel.state.Status = "degraded"
				tunnel.state.LastError = err.Error()
				tunnel.state.UpdatedAt = time.Now()
				d.mu.Unlock()
			},
			OnStopped: func(err error) {
				d.mu.Lock()
				defer d.mu.Unlock()
				if err != nil {
					tunnel.state.Status = "failed"
					tunnel.state.LastError = err.Error()
				} else {
					tunnel.state.Status = "stopped"
					tunnel.state.LastError = ""
				}
				tunnel.state.UpdatedAt = time.Now()
			},
		})

		if err == nil {
			return
		}
		if tunnel.ctx.Err() != nil {
			return
		}

		d.mu.Lock()
		tunnel.state.Status = "failed"
		tunnel.state.LastError = err.Error()
		tunnel.state.UpdatedAt = time.Now()
		d.mu.Unlock()

		if !restart {
			if failed != nil && !failureSent {
				failed <- err
				failureSent = true
			}
			return
		}

		select {
		case <-tunnel.ctx.Done():
			return
		case <-time.After(tunnel.cfg.PollRetryBackoff):
		}
	}
}

func (d *Daemon) Stop(ctx context.Context, tenant, name string) error {
	key := managedKey(norm.Tenant(tenant), norm.Name(name))

	d.mu.Lock()
	tunnel, ok := d.tunnels[key]
	if !ok {
		d.mu.Unlock()
		return errors.New("tunnel not found")
	}
	delete(d.tunnels, key)
	tunnel.state.Status = "stopping"
	tunnel.state.UpdatedAt = time.Now()
	if err := d.saveLocked(); err != nil {
		d.mu.Unlock()
		return err
	}
	d.mu.Unlock()

	tunnel.cancel()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tunnel.done:
		return nil
	}
}

func (d *Daemon) List() []ManagedTunnel {
	d.mu.RLock()
	defer d.mu.RUnlock()

	tunnels := make([]ManagedTunnel, 0, len(d.tunnels))
	for _, tunnel := range d.tunnels {
		tunnels = append(tunnels, cloneManaged(tunnel.state))
	}
	sort.Slice(tunnels, func(i, j int) bool {
		if tunnels[i].Tenant != tunnels[j].Tenant {
			return tunnels[i].Tenant < tunnels[j].Tenant
		}
		return tunnels[i].CreatedAt.Before(tunnels[j].CreatedAt)
	})
	return tunnels
}

func (d *Daemon) Get(tenant, name string) (ManagedTunnel, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	tunnel, ok := d.tunnels[managedKey(norm.Tenant(tenant), norm.Name(name))]
	if !ok {
		return ManagedTunnel{}, errors.New("tunnel not found")
	}
	return cloneManaged(tunnel.state), nil
}

func (d *Daemon) saveLocked() error {
	if d.statePath == "" {
		return nil
	}

	state := persistedState{
		Tunnels: make([]persistedTunnel, 0, len(d.tunnels)),
	}
	for _, tunnel := range d.tunnels {
		state.Tunnels = append(state.Tunnels, persistedTunnel{
			ServerURL:        tunnel.cfg.ServerURL,
			TargetURL:        tunnel.cfg.TargetURL,
			Tenant:           tunnel.cfg.Tenant,
			Name:             tunnel.cfg.Name,
			Domains:          append([]string(nil), tunnel.cfg.Domains...),
			Token:            tunnel.cfg.Token,
			RequestTimeout:   tunnel.cfg.RequestTimeout,
			PollRetryBackoff: tunnel.cfg.PollRetryBackoff,
			MaxResponseBytes: tunnel.cfg.MaxResponseBytes,
		})
	}

	sort.Slice(state.Tunnels, func(i, j int) bool {
		if state.Tunnels[i].Tenant != state.Tunnels[j].Tenant {
			return state.Tunnels[i].Tenant < state.Tunnels[j].Tenant
		}
		return state.Tunnels[i].Name < state.Tunnels[j].Name
	})

	buf, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(d.statePath), 0o755); err != nil {
		return err
	}

	tmpPath := d.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, buf, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, d.statePath)
}

func (d *Daemon) loadState() (persistedState, error) {
	if d.statePath == "" {
		return persistedState{}, nil
	}

	buf, err := os.ReadFile(d.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return persistedState{}, nil
		}
		return persistedState{}, err
	}

	var state persistedState
	if len(buf) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(buf, &state); err != nil {
		return persistedState{}, err
	}
	return state, nil
}

func cloneManaged(state ManagedTunnel) ManagedTunnel {
	state.Domains = append([]string(nil), state.Domains...)
	state.URLs = append([]string(nil), state.URLs...)
	return state
}

func managedKey(tenant, name string) string {
	return norm.Key(tenant, name)
}
