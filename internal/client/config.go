package client

import (
	"fmt"
	"strconv"
	"strings"

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

func LoadConfig(cfgFile string, args []string) (config Config, err error) {
	viper.SetDefault("server-addr", "localhost:8910")
	viper.SetDefault("multiplex", false)

	viper.AutomaticEnv()
	viper.SetEnvPrefix("GNAR")
	viper.BindEnv("token")
	viper.BindEnv("multiplex")

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

	proxy := Proxy{
		ProxyName:  viper.GetString("proxy-name"),
		Subdomain:  viper.GetString("subdomain"),
		SpeedLimit: viper.GetString("speed-limit"),
		ProxyType:  viper.GetString("proxy-type"),
	}

	if len(args) > 0 {
		config.SvrAddr = args[0]
	}

	if len(args) > 1 {
		localRemote, err := parseProxyArg(args[1])
		if err != nil {
			return config, err
		}
		proxy.LocalPort = localRemote.LocalPort
		proxy.RemotePort = localRemote.RemotePort
	}

	config.Proxys = []Proxy{proxy}

	return config, nil
}

func parseProxyArg(arg string) (Proxy, error) {
	parts := strings.Split(arg, ":")
	if len(parts) != 2 {
		return Proxy{}, fmt.Errorf("invalid proxy format. Expected localPort:remotePort")
	}

	localPort, err := strconv.Atoi(parts[0])
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid local port: %v", err)
	}

	remotePort, err := strconv.Atoi(parts[1])
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid remote port: %v", err)
	}

	return Proxy{
		LocalPort:  localPort,
		RemotePort: remotePort,
		ProxyType:  "tcp",
	}, nil
}
