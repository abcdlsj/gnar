package cmd

import (
	"fmt"

	"github.com/abcdlsj/gpipe/client"
	"github.com/abcdlsj/gpipe/logger"
	"github.com/abcdlsj/gpipe/server"
	"github.com/spf13/cobra"
)

func Execute(gitHash, buildStamp string) {
	var RootCmd = &cobra.Command{
		Use:  "gpipe",
		Long: "gpipe is a frp like tool.",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	RootCmd.AddCommand(server.Command())
	RootCmd.AddCommand(client.Command())

	RootCmd.Version = fmt.Sprintf("v0.0.1-%s; buildstamp %s", gitHash, buildStamp)

	if err := RootCmd.Execute(); err != nil {
		logger.FatalF("Execute cobra cmd failed, err: %v", err)
	}
}
