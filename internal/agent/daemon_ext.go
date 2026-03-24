package agent

import "context"

type RunnerService interface {
	RunWithHooks(context.Context, RunnerHooks) error
}

type RunnerFactory func(Config) RunnerService

type StateStore interface {
	Load() ([]Config, error)
	Save([]Config) error
}

type DaemonObserver interface {
	TunnelUpdated(ManagedTunnel)
	TunnelRemoved(ManagedTunnel)
}

type DaemonOption func(*Daemon)

func WithStateStore(store StateStore) DaemonOption {
	return func(d *Daemon) {
		if store != nil {
			d.stateStore = store
		}
	}
}

func WithRunnerFactory(factory RunnerFactory) DaemonOption {
	return func(d *Daemon) {
		if factory != nil {
			d.runnerFactory = factory
		}
	}
}

func WithObservers(observers ...DaemonObserver) DaemonOption {
	return func(d *Daemon) {
		d.observers = append(d.observers, observers...)
	}
}
