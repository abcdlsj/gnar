package client

import (
	"github.com/abcdlsj/gnar/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func Command() *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use: "client",
		Run: func(cmd *cobra.Command, args []string) {
			var cfg Config
			var err error

			if cfgFile != "" {
				cfg, err = LoadConfig(cfgFile)
				if err != nil {
					logger.Fatalf("Error loading config file: %v", err)
				}
			} else {
				cfg = Config{
					SvrAddr:   viper.GetString("server-addr"),
					Token:     viper.GetString("token"),
					Multiplex: viper.GetBool("multiplex"),
					Proxys:    []Proxy{},
				}

				var proxy Proxy
				proxy.ProxyName = viper.GetString("proxy-name")
				proxy.Subdomain = viper.GetString("subdomain")
				proxy.RemotePort = viper.GetInt("remote-port")
				proxy.LocalPort = viper.GetInt("local-port")
				proxy.SpeedLimit = viper.GetString("speed-limit")
				proxy.ProxyType = viper.GetString("proxy-type")

				cfg.Proxys = append(cfg.Proxys, proxy)
			}

			newClient(cfg).Run()
		},
	}

	cmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")
	cmd.PersistentFlags().StringP("server-addr", "s", "localhost:8910", "server addr")
	cmd.PersistentFlags().BoolP("multiplex", "m", false, "multiplex client/server control connection")
	cmd.PersistentFlags().StringP("token", "t", "", "token")
	cmd.PersistentFlags().IntP("remote-port", "u", 0, "proxy port")
	cmd.PersistentFlags().IntP("local-port", "l", 0, "local port")
	cmd.PersistentFlags().StringP("subdomain", "d", "", "subdomain")
	cmd.PersistentFlags().StringP("proxy-name", "n", "", "proxy name")
	cmd.PersistentFlags().StringP("proxy-type", "y", "tcp", "proxy transport protocol type")
	cmd.PersistentFlags().StringP("speed-limit", "", "", "speed limit")

	viper.BindPFlags(cmd.PersistentFlags())

	return cmd
}
