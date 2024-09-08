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
	CaddySrvName string `mapstructure:"caddy-srv-name"`
}

func LoadConfig(cfgFile string, args []string) (config Config, err error) {
	viper.SetDefault("port", 8910)
	viper.SetDefault("admin-port", 0)
	viper.SetDefault("domain-tunnel", false)
	viper.SetDefault("multiplex", false)
	viper.SetDefault("caddy-srv-name", "srv0")

	viper.AutomaticEnv()
	viper.SetEnvPrefix("GNAR")
	viper.BindEnv("port")
	viper.BindEnv("admin-port")
	viper.BindEnv("domain-tunnel")
	viper.BindEnv("domain")
	viper.BindEnv("token")
	viper.BindEnv("multiplex")
	viper.BindEnv("caddy-srv-name")

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		viper.SetConfigType("toml")
		if err := viper.ReadInConfig(); err != nil {
			return config, fmt.Errorf("error reading config file: %v", err)
		}
	}

	if err := viper.Unmarshal(&config); err != nil {
		return config, fmt.Errorf("error unmarshaling config: %v", err)
	}

	if len(args) > 0 {
		port, err := strconv.Atoi(args[0])
		if err != nil {
			return config, fmt.Errorf("invalid port number: %v", err)
		}
		config.Port = port
	}

	return config, nil
}
