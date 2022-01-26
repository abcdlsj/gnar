package server

import (
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	port = 8910
)

var logger = logrus.New()

var Command = &cobra.Command{
	Use: "server",
	Run: func(cmd *cobra.Command, args []string) {
		run()
	},
}

func init() {
	Command.PersistentFlags().IntVarP(&port, "port", "p", 8910, "server port")

	_ = Command.MarkFlagRequired("port")

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
