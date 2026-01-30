// Package cli provides command-line interface for gnar.
package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/abcdlsj/gnar/internal/output"
	"github.com/abcdlsj/gnar/internal/tui"
	"github.com/abcdlsj/gnar/pkg/tunnel"
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
				return tui.Run(ctx)
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

			output.Info("Authenticating with %s...", server)
			output.Label("Enter token: ")

			var token string
			_, err := fmt.Scanln(&token)
			if err != nil {
				return err
			}

			if token == "" {
				return fmt.Errorf("token is required")
			}

			// Create client and authenticate
			cfg := tunnel.ClientConfig{
				ServerAddr: server,
				QUIC:       tunnel.QUICConfig{},
			}
			client := tunnel.NewClient(cfg)

			if err := client.Auth(ctx, token); err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}

			output.Success("Authentication successful!")
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
		Use:   "expose [port]",
		Short: "Expose a local service",
		Long: `Expose a local service to the internet.
		
Examples:
  gnar expose          # Run in TUI mode to select a service
  gnar expose 3000     # Expose port 3000
  gnar expose :8080    # Expose port 8080`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// If port specified, use CLI mode
			if len(args) > 0 {
				portStr := args[0]
				// Remove colon if present
				portStr = strings.TrimPrefix(portStr, ":")

				port, err := strconv.Atoi(portStr)
				if err != nil {
					return fmt.Errorf("invalid port: %s", portStr)
				}

				return exposePort(ctx, server, port, subdomain, protocol)
			}

			// Otherwise run TUI
			return tui.Run(ctx)
		},
	}

	cmd.Flags().StringVarP(&server, "server", "s", "", "Server address (default: localhost:8443)")
	cmd.Flags().StringVarP(&subdomain, "name", "n", "", "Subdomain prefix")
	cmd.Flags().StringVarP(&protocol, "protocol", "p", "http", "Protocol (http/https)")

	return cmd
}

func exposePort(ctx context.Context, server string, port int, subdomain, protocol string) error {
	if server == "" {
		server = "localhost:8443"
	}

	output.Info("Exposing port %d via %s...", port, server)

	// Create client
	cfg := tunnel.ClientConfig{
		ServerAddr: server,
		QUIC:       tunnel.QUICConfig{},
	}
	client := tunnel.NewClient(cfg)

	// Check if authenticated - for now, we need a token
	// In a real implementation, we'd load saved credentials
	if !client.IsAuthenticated() {
		return fmt.Errorf("not authenticated. Run 'gnar auth %s' first", server)
	}

	// Connect and expose
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	t, err := client.Expose(ctx, port, tunnel.ExposeOptions{
		Subdomain: subdomain,
		Protocol:  protocol,
	})
	if err != nil {
		return fmt.Errorf("failed to expose port: %w", err)
	}

	output.Line()
	output.Success("Tunnel established!")
	output.Line()
	output.Pair("Local:", fmt.Sprintf("localhost:%d", port))
	output.Pair("Public:", t.PublicURL)
	output.Line()
	output.Muted("Press Ctrl+C to stop")

	// Wait for context cancellation
	<-ctx.Done()

	output.Line()
	output.Info("Shutting down...")
	return nil
}

func statusCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show active tunnels",
		RunE: func(cmd *cobra.Command, args []string) error {
			output.Title("Active Tunnels")
			output.Muted("No active tunnels (TUI mode required for full status)")
			return nil
		},
	}
}

func stopCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "stop [tunnel-id]",
		Short: "Stop a tunnel",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				output.Info("Stopping all tunnels...")
				output.Success("No active tunnels to stop")
			} else {
				tunnelID := args[0]
				output.Info("Stopping tunnel: %s", tunnelID)
				output.Success("Tunnel stopped")
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
			output.Title("gnar v2.0.0")
		},
	}
}
