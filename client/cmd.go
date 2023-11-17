package client

import (
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	var (
		cfgFile string
		flagCfg Config
	)

	cfg := &cobra.Command{
		Use: "client",
		Run: func(cmd *cobra.Command, args []string) {
			if cfgFile != "" {
				newClient(parseConfig(cfgFile)).Run()
			} else {
				newClient(flagCfg).Run()
			}
		},
	}

	flagCfg.Proxys = make([]Proxy, 1)

	cfg.PersistentFlags().StringVarP(&flagCfg.SvrAddr, "server-addr", "s", "localhost:8910", "server addr")
	cfg.PersistentFlags().BoolVarP(&flagCfg.Multiplex, "multiplex", "m", false, "multiplex client/server control connection")
	cfg.PersistentFlags().StringVarP(&flagCfg.Token, "token", "t", "", "token")

	cfg.PersistentFlags().IntVarP(&flagCfg.Proxys[0].RemotePort, "remote-port", "u", 0, "proxy port")
	cfg.PersistentFlags().IntVarP(&flagCfg.Proxys[0].LocalPort, "local-port", "l", 0, "local port")
	cfg.PersistentFlags().StringVarP(&flagCfg.Proxys[0].Subdomain, "subdomain", "d", "", "subdomain")
	cfg.PersistentFlags().StringVarP(&flagCfg.Proxys[0].ProxyName, "proxy-name", "n", "", "proxy name")
	cfg.PersistentFlags().StringVarP(&flagCfg.Proxys[0].ProxyType, "proxy-type", "y", "tcp", "proxy transport protocol type")
	cfg.PersistentFlags().StringVarP(&flagCfg.Proxys[0].SpeedLimit, "speed-limit", "", "", "speed limit")

	cfg.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")

	return cfg
}
