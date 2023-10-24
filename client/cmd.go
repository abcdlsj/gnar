package client

import (
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	var (
		svrAddr   string
		lport     int
		fport     int
		token     string
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
				flagCfg.Forwards[0] = Forward{
					LocalPort:  lport,
					RemotePort: fport,
					Subdomain:  subdomain,
					ProxyName:  proxyName,
					ProxyType:  proxyType,
				}
				flagCfg.SvrAddr = svrAddr
				flagCfg.Token = token

				newClient(flagCfg).Run()
			}
		},
	}

	flagCfg.Forwards = make([]Forward, 1)

	cfg.PersistentFlags().StringVarP(&svrAddr, "server-addr", "s", "localhost:8910", "server addr")
	cfg.PersistentFlags().IntVarP(&fport, "forward-port", "u", 0, "forward port")
	cfg.PersistentFlags().IntVarP(&lport, "local-port", "l", 0, "local port")
	cfg.PersistentFlags().StringVarP(&token, "token", "t", "", "token")
	cfg.PersistentFlags().StringVarP(&subdomain, "subdomain", "d", "", "subdomain")
	cfg.PersistentFlags().StringVarP(&proxyName, "proxy-name", "n", "", "proxy name")
	cfg.PersistentFlags().StringVarP(&proxyType, "proxy-type", "y", "tcp", "forward protocol type")
	cfg.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")

	return cfg
}
