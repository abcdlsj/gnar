package cli

import (
	"github.com/abcdlsj/gnar/internal/agent"
	"github.com/abcdlsj/gnar/internal/build"
	"github.com/abcdlsj/gnar/internal/server"
	"github.com/spf13/cobra"
)

func NewRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "gnar",
		Short:        "HTTP-first local service publishing",
		SilenceUsage: true,
		Version:      build.Version,
	}

	cmd.AddCommand(server.Command())
	cmd.AddCommand(agent.HTTPCommand())
	cmd.AddCommand(agent.AgentCommand())
	cmd.AddCommand(agent.StopCommand())
	cmd.AddCommand(NewListCommand())
	cmd.AddCommand(NewInspectCommand())
	cmd.AddCommand(NewLogsCommand())
	cmd.AddCommand(NewDoctorCommand())
	return cmd
}

func Execute() error {
	return NewRoot().Execute()
}
