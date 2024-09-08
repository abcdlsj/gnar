package server

import (
	"fmt"
	"strconv"

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

func LoadConfig(cfgFile string, args []string) (config Config, err error) {
	viper.SetDefault("port", 8910)
	viper.SetDefault("admin-port", 8911)
	viper.SetDefault("domain-tunnel", false)
	viper.SetDefault("multiplex", false)

	viper.AutomaticEnv()
	viper.SetEnvPrefix("GNAR")
	viper.BindEnv("port")
	viper.BindEnv("admin-port")
	viper.BindEnv("domain-tunnel")
	viper.BindEnv("domain")
	viper.BindEnv("token")
	viper.BindEnv("multiplex")

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		viper.SetConfigType("toml")
		if err := viper.ReadInConfig(); err != nil {
			return config, fmt.Errorf("Error reading config file: %v", err)
		}
	}

	if len(args) > 0 {
		port, err := strconv.Atoi(args[0])
		if err != nil {
			return config, fmt.Errorf("Invalid port number: %v", err)
		}
		config.Port = port
	}

	if err := viper.Unmarshal(&config); err != nil {
		return config, fmt.Errorf("Error unmarshaling config: %v", err)
	}

	return config, nil
}
