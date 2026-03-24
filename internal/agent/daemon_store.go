package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type FileStateStore struct {
	path string
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

func NewFileStateStore(path string) *FileStateStore {
	return &FileStateStore{path: path}
}

func (s *FileStateStore) Load() ([]Config, error) {
	if s.path == "" {
		return nil, nil
	}

	buf, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var state persistedState
	if len(buf) > 0 {
		if err := json.Unmarshal(buf, &state); err != nil {
			return nil, err
		}
	}

	configs := make([]Config, 0, len(state.Tunnels))
	for _, tunnel := range state.Tunnels {
		configs = append(configs, Config{
			ServerURL:        tunnel.ServerURL,
			TargetURL:        tunnel.TargetURL,
			Tenant:           tunnel.Tenant,
			Name:             tunnel.Name,
			Domains:          append([]string(nil), tunnel.Domains...),
			Token:            tunnel.Token,
			RequestTimeout:   tunnel.RequestTimeout,
			PollRetryBackoff: tunnel.PollRetryBackoff,
			MaxResponseBytes: tunnel.MaxResponseBytes,
		})
	}
	return configs, nil
}

func (s *FileStateStore) Save(configs []Config) error {
	if s.path == "" {
		return nil
	}

	state := persistedState{
		Tunnels: make([]persistedTunnel, 0, len(configs)),
	}
	for _, cfg := range configs {
		state.Tunnels = append(state.Tunnels, persistedTunnel{
			ServerURL:        cfg.ServerURL,
			TargetURL:        cfg.TargetURL,
			Tenant:           cfg.Tenant,
			Name:             cfg.Name,
			Domains:          append([]string(nil), cfg.Domains...),
			Token:            cfg.Token,
			RequestTimeout:   cfg.RequestTimeout,
			PollRetryBackoff: cfg.PollRetryBackoff,
			MaxResponseBytes: cfg.MaxResponseBytes,
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
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, buf, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}
