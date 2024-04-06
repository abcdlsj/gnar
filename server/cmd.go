package server

import (
	"github.com/abcdlsj/gnar/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func Command() *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use: "server",
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
					Port:         viper.GetInt("port"),
					AdminPort:    viper.GetInt("admin-port"),
					DomainTunnel: viper.GetBool("domain-tunnel"),
					Domain:       viper.GetString("domain"),
					Token:        viper.GetString("token"),
					Multiplex:    viper.GetBool("multiplex"),
				}
			}

			newServer(cfg).Run()
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
