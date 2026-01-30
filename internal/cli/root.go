// Package cli provides command-line interface for gnar.
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// Execute runs the CLI.
func Execute(ctx context.Context) error {
	rootCmd := &cobra.Command{
		Use:   "gnar",
		Short: "Expose local services to the internet",
		Long: `Gnar is a tool that exposes your local services to the internet
through secure tunnels with automatic HTTPS.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// If no args, run TUI mode
			if len(args) == 0 {
				return runTUI(ctx)
			}
			return cmd.Help()
		},
	}

	// Add subcommands
	rootCmd.AddCommand(authCmd(ctx))
	rootCmd.AddCommand(exposeCmd(ctx))
	rootCmd.AddCommand(statusCmd(ctx))
	rootCmd.AddCommand(stopCmd(ctx))
	rootCmd.AddCommand(versionCmd())

	return rootCmd.Execute()
}

func authCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "auth <server>",
		Short: "Authenticate with a gnar server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			server := args[0]
			fmt.Printf("Authenticating with %s...\\n", server)
			// TODO: implement auth flow
			return nil
		},
	}
}

func exposeCmd(ctx context.Context) *cobra.Command {
	var (
		server    string
		subdomain string
		protocol  string
	)

	cmd := &cobra.Command{
		Use:   "expose [:<port>]",
		Short: "Expose a local service",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// If port specified, use CLI mode
			if len(args) > 0 {
				port := args[0]
				fmt.Printf("Exposing port %s...\\n", port)
				// TODO: implement CLI expose
				return nil
			}

			// Otherwise run TUI
			return runTUI(ctx)
		},
	}

	cmd.Flags().StringVarP(&server, "server", "s", "", "Server address (default: use saved default)")
	cmd.Flags().StringVarP(&subdomain, "name", "n", "", "Subdomain prefix")
	cmd.Flags().StringVarP(&protocol, "protocol", "p", "http", "Protocol (http/https)")

	return cmd
}

func statusCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show active tunnels",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Active tunnels:")
			// TODO: show status
			return nil
		},
	}
}

func stopCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "stop [name]",
		Short: "Stop a tunnel",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Println("Stopping all tunnels...")
			} else {
				fmt.Printf("Stopping tunnel: %s\\n", args[0])
			}
			return nil
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("gnar v2.0.0")
		},
	}
}

func runTUI(ctx context.Context) error {
	// TODO: implement TUI
	fmt.Println("Running in TUI mode...")
	return nil
}
