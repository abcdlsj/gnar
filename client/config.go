package client

import (
	"github.com/abcdlsj/gnar/logger"
	"github.com/spf13/viper"
)

type Config struct {
	SvrAddr   string  `mapstructure:"server-addr"`
	Token     string  `mapstructure:"token"`
	Multiplex bool    `mapstructure:"multiplex"`
	Proxys    []Proxy `mapstructure:"proxys"`
}

type Proxy struct {
	ProxyName  string `mapstructure:"proxy-name"`
	Subdomain  string `mapstructure:"subdomain"`
	RemotePort int    `mapstructure:"remote-port"`
	LocalPort  int    `mapstructure:"local-port"`
	SpeedLimit string `mapstructure:"speed-limit"`
	ProxyType  string `mapstructure:"proxy-type"`
}

func LoadConfig(cfgFile string) (config Config, err error) {
	viper.SetConfigFile(cfgFile)
	viper.SetConfigType("toml")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		logger.Fatalf("Error reading config file: %v", err)
	}

	err = viper.Unmarshal(&config)
	return
}
