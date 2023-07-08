package server

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/abcdlsj/gpipe/logger"
)

type Config struct {
	Port         int    `toml:"port"`
	AdminPort    int    `toml:"admin-port"`    // zero means disable admin server
	DomainTunnel bool   `toml:"domain-tunnel"` // enable domain tunnel
	Domain       string `toml:"domain"`        // domain name
	Token        string `toml:"token"`
}

func parseConfig(cfgFile string) Config {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		logger.FatalF("Error reading config file: %v", err)
	}

	var cfg Config
	toml.Unmarshal(data, &cfg)

	return cfg
}
