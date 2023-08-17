package client

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/abcdlsj/pipe/logger"
)

type Config struct {
	ServerHost string    `toml:"server-host"`
	ServerPort int       `toml:"server-port"`
	Token      string    `toml:"token"`
	Forwards   []Forward `toml:"forwards"`
}

type Forward struct {
	ProxyName  string `toml:"proxy-name"`
	SubDomain  string `toml:"subdomain"`
	RemotePort int    `toml:"remote-port"`
	LocalPort  int    `toml:"local-port"`
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
