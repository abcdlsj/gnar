// Package store provides local credential storage.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/abcdlsj/gnar/pkg/tunnel"
)

// FileStore implements AuthStore using local file storage.
type FileStore struct {
	path string
}

// NewFileStore creates a new file-based credential store.
func NewFileStore() (*FileStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	configDir := filepath.Join(homeDir, ".gnar")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	return &FileStore{
		path: filepath.Join(configDir, "credentials.json"),
	}, nil
}

// Save saves a credential.
func (s *FileStore) Save(cred *tunnel.Credential) error {
	creds, err := s.loadAll()
	if err != nil {
		creds = make(map[string]*tunnel.Credential)
	}

	creds[cred.Server] = cred

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	return nil
}

// Load loads a credential for a server.
func (s *FileStore) Load(server string) (*tunnel.Credential, error) {
	creds, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	cred, ok := creds[server]
	if !ok {
		return nil, fmt.Errorf("no credential found for %s", server)
	}

	return cred, nil
}

// Delete removes a credential.
func (s *FileStore) Delete(server string) error {
	creds, err := s.loadAll()
	if err != nil {
		return nil
	}

	delete(creds, server)

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	return nil
}

// List lists all saved credentials.
func (s *FileStore) List() ([]*tunnel.Credential, error) {
	creds, err := s.loadAll()
	if err != nil {
		return nil, err
	}

	var result []*tunnel.Credential
	for _, cred := range creds {
		result = append(result, cred)
	}

	return result, nil
}

// loadAll loads all credentials from file.
func (s *FileStore) loadAll() (map[string]*tunnel.Credential, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*tunnel.Credential), nil
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	var creds map[string]*tunnel.Credential
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("unmarshal credentials: %w", err)
	}

	return creds, nil
}

// GetDefaultServer returns the default server if any.
func GetDefaultServer() (string, error) {
	store, err := NewFileStore()
	if err != nil {
		return "", err
	}

	creds, err := store.List()
	if err != nil {
		return "", err
	}

	for _, cred := range creds {
		if cred.Default {
			return cred.Server, nil
		}
	}

	if len(creds) > 0 {
		return creds[0].Server, nil
	}

	return "", fmt.Errorf("no server configured")
}
