package client

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var logger = logrus.New()

var (
	rPort, lPort, uPort = 8910, 3000, 9000
	rHost               = "localhost"
)

var Command = &cobra.Command{
	Use: "client",
	Run: func(cmd *cobra.Command, args []string) {
		run()
	},
}

func init() {
	Command.PersistentFlags().StringVarP(&rHost, "server-host", "s", "localhost", "server host")
	Command.PersistentFlags().IntVarP(&rPort, "server-port", "p", 8910, "server port")
	Command.PersistentFlags().IntVarP(&lPort, "local-port", "l", 0, "local port")
	Command.PersistentFlags().IntVarP(&uPort, "forward-port", "u", 0, "forward port")

	_ = Command.MarkFlagRequired("local-port")
	_ = Command.MarkFlagRequired("forward-port")

	logger.SetLevel(logLevel())
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
	})
}

func logLevel() logrus.Level {
	if envDebug() {
		return logrus.DebugLevel
	}
	return logrus.InfoLevel
}

func envDebug() bool {
	return os.Getenv("DEBUG") != ""
}
