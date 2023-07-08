package server

import (
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	var (
		cfgFile string
		flagCfg Config

		port         int
		adminPort    int
		domain       string
		domainTunnel bool
		token        string
	)

	cmd := &cobra.Command{
		Use: "server",
		Run: func(cmd *cobra.Command, args []string) {
			if cfgFile != "" {
				newServer(parseConfig(cfgFile)).Run()
			} else {
				flagCfg.Port = port
				flagCfg.AdminPort = adminPort
				flagCfg.Domain = domain
				flagCfg.DomainTunnel = domainTunnel
				flagCfg.Token = token

				newServer(flagCfg).Run()
			}
		},
	}
	cmd.PersistentFlags().IntVarP(&port, "port", "p", 8910, "server port")
	cmd.PersistentFlags().IntVarP(&adminPort, "admin-port", "a", 0, "admin server port")
	cmd.PersistentFlags().BoolVarP(&domainTunnel, "domain-tunnel", "d", false, "enable domain tunnel")
	cmd.PersistentFlags().StringVarP(&domain, "domain", "D", "", "domain name")
	cmd.PersistentFlags().StringVarP(&token, "token", "t", "", "token")
	cmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")

	return cmd
}
