package server

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/abcdlsj/pipe/logger"
)

type Config struct {
	Port         int    `toml:"port"`
	AdminPort    int    `toml:"admin-port"`    // zero means disable admin server
	DomainTunnel bool   `toml:"domain-tunnel"` // enable domain tunnel
	Domain       string `toml:"domain"`        // domain name
	Token        string `toml:"token"`
	Multiple     bool   `toml:"multiple"`
}

func parseConfig(cfgFile string) Config {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		logger.Fatalf("Error reading config file: %v", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		logger.Fatalf("Error parsing config file: %v", err)
	}

	return cfg
}
