package cmd

import (
	"fmt"

	"github.com/abcdlsj/pipe/client"
	"github.com/abcdlsj/pipe/server"
	"github.com/abcdlsj/pipe/share"
	"github.com/spf13/cobra"
)

func Execute() {
	var RootCmd = &cobra.Command{
		Use:  "pipe",
		Long: "pipe is a proxy tool.",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	RootCmd.AddCommand(server.Command())
	RootCmd.AddCommand(client.Command())

	RootCmd.Version = fmt.Sprintf("%s; buildstamp %s", share.GetVersion(), share.BuildStamp)

	RootCmd.Execute()
}
