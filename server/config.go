package server

import (
	"github.com/abcdlsj/gnar/logger"
	"github.com/spf13/viper"
)

type Config struct {
	Port         int    `mapstructure:"port"`
	AdminPort    int    `mapstructure:"admin-port"`
	DomainTunnel bool   `mapstructure:"domain-tunnel"`
	Domain       string `mapstructure:"domain"`
	Token        string `mapstructure:"token"`
	Multiplex    bool   `mapstructure:"multiplex"`
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
