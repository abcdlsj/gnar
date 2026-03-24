package agent

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abcdlsj/gnar/internal/norm"
)

type Config struct {
	ServerURL        string
	TargetURL        string
	Tenant           string
	Name             string
	Domains          []string
	Token            string
	RequestTimeout   time.Duration
	PollRetryBackoff time.Duration
	MaxResponseBytes int64
}

func DefaultConfig() Config {
	return Config{
		ServerURL:        "http://127.0.0.1:8910",
		Tenant:           "default",
		RequestTimeout:   30 * time.Second,
		PollRetryBackoff: time.Second,
		MaxResponseBytes: 8 << 20,
	}
}

func resolveConfig(input string, cfg Config) (Config, error) {
	targetURL, err := normalizeTarget(input)
	if err != nil {
		return cfg, err
	}

	cfg.TargetURL = targetURL
	if cfg.Name == "" {
		cfg.Name = defaultName()
	}
	return cfg, nil
}

func normalizeTarget(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("target is required")
	}

	if _, err := strconv.Atoi(input); err == nil {
		return "http://127.0.0.1:" + input, nil
	}

	if !strings.Contains(input, "://") {
		input = "http://" + input
	}

	parsed, err := url.Parse(input)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("target must be a valid URL or local port")
	}
	return parsed.String(), nil
}

func defaultName() string {
	wd, err := os.Getwd()
	if err != nil {
		return "tunnel"
	}

	name := filepath.Base(wd)
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || name == "." || name == "/" {
		return "tunnel"
	}
	return norm.Name(name)
}
