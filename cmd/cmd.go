package cmd

import (
	"fmt"

	"github.com/abcdlsj/pipe/client"
	"github.com/abcdlsj/pipe/logger"
	"github.com/abcdlsj/pipe/server"
	"github.com/spf13/cobra"
)

func Execute(gitHash, buildStamp string) {
	var RootCmd = &cobra.Command{
		Use:  "pipe",
		Long: "pipe is a proxy tool.",
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
