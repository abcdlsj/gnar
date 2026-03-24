package server

import "time"

type Config struct {
	ListenAddr            string
	PublicURL             string
	BaseDomain            string
	AgentToken            string
	ManageToken           string
	AgentCredentials      map[string]string
	AllowedDomainSuffixes []string
	TenantDomainSuffixes  map[string][]string
	RequestTimeout        time.Duration
	IdleTimeout           time.Duration
	PollTimeout           time.Duration
	MaxBodyBytes          int64
}

func DefaultConfig() Config {
	return Config{
		ListenAddr:           ":8910",
		PublicURL:            "http://127.0.0.1:8910",
		AgentCredentials:     make(map[string]string),
		TenantDomainSuffixes: make(map[string][]string),
		RequestTimeout:       30 * time.Second,
		IdleTimeout:          45 * time.Second,
		PollTimeout:          25 * time.Second,
		MaxBodyBytes:         8 << 20,
	}
}
