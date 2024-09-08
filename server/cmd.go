package server

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func Command() *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "server [port]",
		Short: "Run gnar server",
		Long:  "Run gnar server with optional port argument",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(cfgFile, args)
			if err != nil {
				return fmt.Errorf("error loading config: %v", err)
			}

			return newServer(cfg).Run()
		},
	}

	cmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file")
	cmd.PersistentFlags().IntP("port", "p", 8910, "server port")
	cmd.PersistentFlags().IntP("admin-port", "a", 0, "admin server port")
	cmd.PersistentFlags().BoolP("domain-tunnel", "d", false, "enable domain tunnel")
	cmd.PersistentFlags().StringP("domain", "D", "", "domain name")
	cmd.PersistentFlags().StringP("token", "t", "", "token")
	cmd.PersistentFlags().BoolP("multiplex", "m", false, "multiplex client/server control connection")

	viper.BindPFlags(cmd.PersistentFlags())

	return cmd
}
