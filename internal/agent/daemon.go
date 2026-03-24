package agent

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/abcdlsj/gnar/internal/norm"
	"github.com/abcdlsj/gnar/pkg/api"
)

type Daemon struct {
	mu            sync.RWMutex
	stateStore    StateStore
	runnerFactory RunnerFactory
	observers     []DaemonObserver
	tunnels       map[string]*managedTunnel
}

type managedTunnel struct {
	key    string
	ctx    context.Context
	cfg    Config
	state  ManagedTunnel
	cancel context.CancelFunc
	done   chan struct{}
}

func NewDaemon(statePath string, opts ...DaemonOption) *Daemon {
	d := &Daemon{
		stateStore: NewFileStateStore(statePath),
		runnerFactory: func(cfg Config) RunnerService {
			return New(cfg)
		},
		tunnels: make(map[string]*managedTunnel),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

func (d *Daemon) Start(ctx context.Context, cfg Config) (ManagedTunnel, error) {
	tunnel, err := d.add(cfg)
	if err != nil {
		return ManagedTunnel{}, err
	}
	d.notifyUpdated(cloneManaged(tunnel.state))

	ready := make(chan struct{}, 1)
	failed := make(chan error, 1)
	go d.runTunnel(tunnel, ready, failed, false)

	select {
	case <-ctx.Done():
		_ = d.Stop(context.Background(), tunnel.state.Tenant, tunnel.state.Name)
		return ManagedTunnel{}, ctx.Err()
	case err := <-failed:
		_ = d.Stop(context.Background(), tunnel.state.Tenant, tunnel.state.Name)
		return ManagedTunnel{}, err
	case <-ready:
		return d.Get(cfg.Tenant, cfg.Name)
	}
}

func (d *Daemon) Restore() error {
	configs, err := d.stateStore.Load()
	if err != nil {
		return err
	}

	for _, cfg := range configs {
		tunnel, err := d.add(cfg)
		if err != nil {
			continue
		}
		d.notifyUpdated(cloneManaged(tunnel.state))
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
	if err := d.persistLocked(); err != nil {
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
		err := d.runnerFactory(tunnel.cfg).RunWithHooks(tunnel.ctx, RunnerHooks{
			OnRegistered: func(reg *api.RegisterTunnelResponse) {
				d.updateTunnelState(tunnel, func(state *ManagedTunnel) {
					state.Tenant = reg.Tenant
					state.PublicURL = reg.PublicURL
					state.URLs = append([]string(nil), reg.URLs...)
					state.Status = "connected"
					state.LastError = ""
					state.UpdatedAt = time.Now()
					state.ConnectedAt = state.UpdatedAt
				})
				notifyReady()
			},
			OnPollError: func(err error) {
				d.updateTunnelState(tunnel, func(state *ManagedTunnel) {
					state.Status = "degraded"
					state.LastError = err.Error()
					state.UpdatedAt = time.Now()
				})
			},
			OnStopped: func(err error) {
				d.updateTunnelState(tunnel, func(state *ManagedTunnel) {
					if err != nil {
						state.Status = "failed"
						state.LastError = err.Error()
					} else {
						state.Status = "stopped"
						state.LastError = ""
					}
					state.UpdatedAt = time.Now()
				})
			},
		})

		if err == nil {
			return
		}
		if tunnel.ctx.Err() != nil {
			return
		}

		d.updateTunnelState(tunnel, func(state *ManagedTunnel) {
			state.Status = "failed"
			state.LastError = err.Error()
			state.UpdatedAt = time.Now()
		})

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
	if err := d.persistLocked(); err != nil {
		d.mu.Unlock()
		return err
	}
	stopping := cloneManaged(tunnel.state)
	d.mu.Unlock()
	d.notifyUpdated(stopping)

	tunnel.cancel()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tunnel.done:
		d.notifyRemoved(cloneManaged(tunnel.state))
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

func (d *Daemon) persistLocked() error {
	configs := make([]Config, 0, len(d.tunnels))
	for _, tunnel := range d.tunnels {
		configs = append(configs, tunnel.cfg)
	}
	return d.stateStore.Save(configs)
}

func (d *Daemon) updateTunnelState(tunnel *managedTunnel, mutate func(*ManagedTunnel)) {
	d.mu.Lock()
	mutate(&tunnel.state)
	state := cloneManaged(tunnel.state)
	d.mu.Unlock()
	d.notifyUpdated(state)
}

func (d *Daemon) notifyUpdated(state ManagedTunnel) {
	state = cloneManaged(state)
	for _, observer := range d.observers {
		observer.TunnelUpdated(state)
	}
}

func (d *Daemon) notifyRemoved(state ManagedTunnel) {
	state = cloneManaged(state)
	for _, observer := range d.observers {
		observer.TunnelRemoved(state)
	}
}

func cloneManaged(state ManagedTunnel) ManagedTunnel {
	state.Domains = append([]string(nil), state.Domains...)
	state.URLs = append([]string(nil), state.URLs...)
	return state
}

func managedKey(tenant, name string) string {
	return norm.Key(tenant, name)
}
