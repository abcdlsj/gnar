package cmd

import (
	"github.com/abcdlsj/gpipe/client"
	"github.com/abcdlsj/gpipe/server"
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "gpipe",
	Short: "gpipe",
	Long:  "gpipe is a frp like tool",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	RootCmd.AddCommand(server.Command)
	RootCmd.AddCommand(client.Command)
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		panic(err)
	}
}
