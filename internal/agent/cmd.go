package agent

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func HTTPCommand() *cobra.Command {
	cfg := DefaultConfig()
	daemonURL := "http://127.0.0.1:7777"
	detach := false

	cmd := &cobra.Command{
		Use:   "http <port-or-url>",
		Short: "Expose a local HTTP service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveConfig(args[0], cfg)
			if err != nil {
				return err
			}

			if detach {
				ctx, cancel := context.WithTimeout(cmd.Context(), resolved.RequestTimeout+5*time.Second)
				defer cancel()
				client := NewDaemonClient(daemonURL)
				if err := client.Health(ctx); err != nil {
					return daemonUnavailableHelp(daemonURL)
				}
				tunnel, err := client.Start(ctx, StartTunnelRequest{
					ServerURL:        resolved.ServerURL,
					TargetURL:        resolved.TargetURL,
					Tenant:           resolved.Tenant,
					Name:             resolved.Name,
					Domains:          resolved.Domains,
					Token:            resolved.Token,
					RequestTimeout:   resolved.RequestTimeout,
					PollRetryBackoff: resolved.PollRetryBackoff,
					MaxResponseBytes: resolved.MaxResponseBytes,
				})
				if err != nil {
					return err
				}
				printManagedTunnel(tunnel)
				return nil
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return New(resolved).Run(ctx)
		},
	}

	cmd.Flags().StringVar(&cfg.ServerURL, "server", cfg.ServerURL, "gnar server URL")
	cmd.Flags().StringVar(&cfg.Tenant, "tenant", cfg.Tenant, "tenant namespace")
	cmd.Flags().StringVar(&cfg.Name, "name", cfg.Name, "tunnel name")
	cmd.Flags().StringSliceVar(&cfg.Domains, "domain", nil, "custom domain bound to this tunnel")
	cmd.Flags().StringVar(&cfg.Token, "token", cfg.Token, "shared registration token")
	cmd.Flags().DurationVar(&cfg.RequestTimeout, "request-timeout", cfg.RequestTimeout, "local upstream timeout")
	cmd.Flags().DurationVar(&cfg.PollRetryBackoff, "retry-backoff", cfg.PollRetryBackoff, "backoff after poll failures")
	cmd.Flags().Int64Var(&cfg.MaxResponseBytes, "max-response-bytes", cfg.MaxResponseBytes, "max buffered upstream response size")
	cmd.Flags().BoolVar(&detach, "detach", detach, "start the tunnel via the local daemon")
	cmd.Flags().StringVar(&daemonURL, "agent-url", daemonURL, "local daemon URL")
	return cmd
}

func AgentCommand() *cobra.Command {
	listenAddr := ":7777"
	statePath := defaultStatePath()
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the local tunnel daemon",
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local tunnel daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return NewDaemonServer(listenAddr, statePath).Run(ctx)
		},
	}

	daemonURL := "http://127.0.0.1:7777"
	lsCmd := &cobra.Command{
		Use:   "ls",
		Short: "List tunnels managed by the local daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			client := NewDaemonClient(daemonURL)
			tunnels, err := client.List(ctx)
			if err != nil {
				return daemonUnavailableHelp(daemonURL)
			}
			if len(tunnels) == 0 {
				fmt.Println("no managed tunnels")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tPUBLIC\tTARGET")
			for _, tunnel := range tunnels {
				fmt.Fprintf(w, "%s/%s\t%s\t%s\t%s\n", tunnel.Tenant, tunnel.Name, tunnel.Status, tunnel.PublicURL, tunnel.TargetURL)
			}
			return w.Flush()
		},
	}

	lsCmd.Flags().StringVar(&daemonURL, "url", daemonURL, "local daemon URL")
	cmd.AddCommand(serveCmd)
	cmd.AddCommand(lsCmd)

	serveCmd.Flags().StringVar(&listenAddr, "listen", listenAddr, "local daemon listen address")
	serveCmd.Flags().StringVar(&statePath, "state-file", statePath, "path to daemon state file")
	return cmd
}

func StopCommand() *cobra.Command {
	daemonURL := "http://127.0.0.1:7777"
	tenant := "default"
	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a detached local tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			if err := NewDaemonClient(daemonURL).Stop(ctx, tenant, args[0]); err != nil {
				return err
			}
			fmt.Printf("stopped %s/%s\n", normalizeTenant(tenant), args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&tenant, "tenant", tenant, "tenant namespace")
	cmd.Flags().StringVar(&daemonURL, "agent-url", daemonURL, "local daemon URL")
	return cmd
}

func printManagedTunnel(tunnel ManagedTunnel) {
	fmt.Printf("Tunnel: %s/%s\n", tunnel.Tenant, tunnel.Name)
	fmt.Printf("Local:  %s\n", tunnel.TargetURL)
	if tunnel.PublicURL != "" {
		fmt.Printf("Public: %s\n", tunnel.PublicURL)
	}
	for _, value := range tunnel.URLs {
		if value == tunnel.PublicURL {
			continue
		}
		fmt.Printf("Alias:  %s\n", value)
	}
	fmt.Printf("State:  %s\n", tunnel.Status)
}
