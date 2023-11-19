package cmd

import (
	"fmt"

	"github.com/abcdlsj/gnar/client"
	"github.com/abcdlsj/gnar/server"
	"github.com/abcdlsj/gnar/share"
	"github.com/spf13/cobra"
)

func Execute() {
	var RootCmd = &cobra.Command{
		Use:  "gnar",
		Long: "gnar is a proxy tool.",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	RootCmd.AddCommand(server.Command())
	RootCmd.AddCommand(client.Command())

	RootCmd.Version = fmt.Sprintf("%s; buildstamp %s", share.GetVersion(), share.BuildStamp)

	RootCmd.Execute()
}
