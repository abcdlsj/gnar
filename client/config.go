package client

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/abcdlsj/pipe/logger"
)

type Config struct {
	SvrHost  string    `toml:"server-host"`
	SvrPort  int       `toml:"server-port"`
	Token    string    `toml:"token"`
	Forwards []Forward `toml:"forwards"`
}

type Forward struct {
	ProxyName  string `toml:"proxy-name"`
	Subdomain  string `toml:"subdomain"`
	RemotePort int    `toml:"remote-port"`
	LocalPort  int    `toml:"local-port"`
	SpeedLimit string `toml:"speed-limit"` // xx/s
	Noise      string `toml:"noise"`       // transport noise
	Type       string `toml:"type"`
}

func parseConfig(cfgFile string) Config {
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		logger.FatalF("Error reading config file: %v", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		logger.FatalF("Error parsing config file: %v", err)
	}

	return cfg
}
