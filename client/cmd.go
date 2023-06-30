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
			var c *Client

			if cfgFile != "" {
				c = newClient(parseConfig(cfgFile))
			} else {
				c = newClient(flagCfg)
			}

			c.Run()
		},
	}

	flagCfg.Forwards = make([]Forward, 1)

	cfg.PersistentFlags().StringVarP(&flagCfg.ServerHost, "server-host", "s", "localhost", "server host")
	cfg.PersistentFlags().IntVarP(&flagCfg.ServerPort, "server-port", "p", 8910, "server port")
	cfg.PersistentFlags().IntVarP(&flagCfg.Forwards[0].LocalPort, "local-port", "l", 0, "local port")
	cfg.PersistentFlags().IntVarP(&flagCfg.Forwards[0].RemotePort, "forward-port", "u", 0, "forward port")
	cfg.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")

	return cfg
}
