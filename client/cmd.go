package client

import (
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	var (
		lport     int
		fport     int
		subdomain string
		proxyName string
		proxyType string

		cfgFile string
		flagCfg Config
	)

	cfg := &cobra.Command{
		Use: "client",
		Run: func(cmd *cobra.Command, args []string) {
			if cfgFile != "" {
				newClient(parseConfig(cfgFile)).Run()
			} else {
				flagCfg.Proxys[0] = Proxy{
					LocalPort:  lport,
					RemotePort: fport,
					Subdomain:  subdomain,
					ProxyName:  proxyName,
					ProxyType:  proxyType,
				}

				newClient(flagCfg).Run()
			}
		},
	}

	flagCfg.Proxys = make([]Proxy, 1)

	cfg.PersistentFlags().StringVarP(&flagCfg.SvrAddr, "server-addr", "s", "localhost:8910", "server addr")
	cfg.PersistentFlags().IntVarP(&fport, "proxy-port", "u", 0, "proxy port")
	cfg.PersistentFlags().IntVarP(&lport, "local-port", "l", 0, "local port")
	cfg.PersistentFlags().StringVarP(&flagCfg.Token, "token", "t", "", "token")
	cfg.PersistentFlags().StringVarP(&subdomain, "subdomain", "d", "", "subdomain")
	cfg.PersistentFlags().StringVarP(&proxyName, "proxy-name", "n", "", "proxy name")
	cfg.PersistentFlags().StringVarP(&proxyType, "proxy-type", "y", "tcp", "proxy transport protocol type")
	cfg.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")
	cfg.PersistentFlags().BoolVarP(&flagCfg.Multiplex, "multiplex", "m", false, "multiplex client/server control connection")

	return cfg
}
