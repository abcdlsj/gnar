package client

import (
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	var (
		shost     string
		sport     int
		lport     int
		fport     int
		token     string
		subdomain string
		proxyName string

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
					SubDomain:  subdomain,
					ProxyName:  proxyName,
				}
				flagCfg.ServerHost = shost
				flagCfg.ServerPort = sport
				flagCfg.Token = token

				newClient(flagCfg).Run()
			}
		},
	}

	flagCfg.Forwards = make([]Forward, 1)

	cfg.PersistentFlags().StringVarP(&shost, "server-host", "s", "localhost", "server host")
	cfg.PersistentFlags().IntVarP(&sport, "server-port", "p", 8910, "server port")
	cfg.PersistentFlags().IntVarP(&lport, "local-port", "l", 0, "local port")
	cfg.PersistentFlags().IntVarP(&fport, "forward-port", "u", 0, "forward port")
	cfg.PersistentFlags().StringVarP(&token, "token", "t", "", "token")
	cfg.PersistentFlags().StringVarP(&subdomain, "subdomain", "d", "", "subdomain")
	cfg.PersistentFlags().StringVarP(&proxyName, "proxy-name", "n", "", "proxy name")
	cfg.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")

	return cfg
}
