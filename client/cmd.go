package client

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func Command() *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "client [server-addr] [local-port:remote-port]",
		Short: "Run gnar client",
		Long:  "Run gnar client with optional server address and port mapping",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			viper.BindPFlags(cmd.PersistentFlags())

			cfg, err := LoadConfig(cfgFile, args)
			if err != nil {
				return fmt.Errorf("error loading config: %v", err)
			}

			return newClient(cfg).Run()
		},
	}

	cmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")
	cmd.PersistentFlags().StringP("server-addr", "s", "localhost:8910", "server addr")
	cmd.PersistentFlags().BoolP("multiplex", "m", false, "multiplex client/server control connection")
	cmd.PersistentFlags().StringP("token", "t", "", "token")
	cmd.PersistentFlags().StringP("subdomain", "d", "", "subdomain")
	cmd.PersistentFlags().StringP("proxy-name", "n", "", "proxy name")
	cmd.PersistentFlags().StringP("proxy-type", "y", "tcp", "proxy transport protocol type")
	cmd.PersistentFlags().StringP("speed-limit", "", "", "speed limit")

	return cmd
}
