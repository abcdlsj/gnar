package main

import (
	"fmt"

	"github.com/abcdlsj/gnar/internal/client"
	"github.com/abcdlsj/gnar/internal/server"
	"github.com/abcdlsj/gnar/pkg/share"
	"github.com/spf13/cobra"
)

func main() {
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
