package client

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/abcdlsj/gnar/logger"
)

type Config struct {
	SvrAddr   string  `toml:"server-addr"`
	Token     string  `toml:"token"`
	Multiplex bool    `toml:"multiplex"`
	Proxys    []Proxy `toml:"proxys"`
}

type Proxy struct {
	ProxyName  string `toml:"proxy-name"`
	Subdomain  string `toml:"subdomain"`
	RemotePort int    `toml:"remote-port"`
	LocalPort  int    `toml:"local-port"`
	SpeedLimit string `toml:"speed-limit"` // xx/s
	ProxyType  string `toml:"proxy-type"`
}

type Transport struct {
	Noise string `toml:"noise"`
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
