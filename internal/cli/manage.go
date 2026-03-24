package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/abcdlsj/gnar/pkg/api"
	"github.com/spf13/cobra"
)

type manageConfig struct {
	ServerURL string
	Token     string
	Tenant    string
	JSON      bool
}

func newManageConfig() manageConfig {
	return manageConfig{
		ServerURL: "http://127.0.0.1:8910",
	}
}

func NewListCommand() *cobra.Command {
	cfg := newManageConfig()
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List active tunnels",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := api.NewClient(cfg.ServerURL, cfg.Token)
			response, err := client.ListTunnels(cmd.Context(), cfg.Tenant)
			if err != nil {
				return err
			}
			if cfg.JSON {
				return printJSON(response)
			}
			printTunnelList(response.Tunnels)
			return nil
		},
	}
	addManageFlags(cmd, &cfg)
	return cmd
}

func NewInspectCommand() *cobra.Command {
	cfg := newManageConfig()
	cmd := &cobra.Command{
		Use:   "inspect <name>",
		Short: "Inspect a tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := api.NewClient(cfg.ServerURL, cfg.Token)
			response, err := client.TunnelDetail(cmd.Context(), cfg.Tenant, args[0])
			if err != nil {
				return err
			}
			if cfg.JSON {
				return printJSON(response)
			}
			printTunnelDetail(response)
			return nil
		},
	}
	addManageFlags(cmd, &cfg)
	return cmd
}

func NewLogsCommand() *cobra.Command {
	cfg := newManageConfig()
	limit := 20
	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show recent requests for a tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := api.NewClient(cfg.ServerURL, cfg.Token)
			response, err := client.TunnelLogs(cmd.Context(), cfg.Tenant, args[0], limit)
			if err != nil {
				return err
			}
			if cfg.JSON {
				return printJSON(response)
			}
			printLogs(response.Tunnel, response.RecentRequests)
			return nil
		},
	}
	addManageFlags(cmd, &cfg)
	cmd.Flags().IntVar(&limit, "limit", limit, "max number of recent requests")
	return cmd
}

func NewDoctorCommand() *cobra.Command {
	cfg := newManageConfig()
	timeout := 3 * time.Second
	cmd := &cobra.Command{
		Use:   "doctor [port-or-url]",
		Short: "Check server reachability and optional local target readiness",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var checks []string
			client := &http.Client{Timeout: timeout}
			if err := checkServer(timeout, cfg.ServerURL, cfg.Token); err != nil {
				checks = append(checks, "server: fail ("+err.Error()+")")
			} else {
				checks = append(checks, "server: ok")
			}

			if len(args) == 1 {
				target, err := normalizeDoctorTarget(args[0])
				if err != nil {
					checks = append(checks, "local: fail ("+err.Error()+")")
				} else if err := checkLocal(client, target); err != nil {
					checks = append(checks, "local: fail ("+err.Error()+")")
				} else {
					checks = append(checks, "local: ok")
				}
			}

			for _, check := range checks {
				fmt.Println(check)
			}

			for _, check := range checks {
				if strings.Contains(check, "fail") {
					return fmt.Errorf("doctor found failures")
				}
			}
			return nil
		},
	}
	addManageFlags(cmd, &cfg)
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "request timeout")
	return cmd
}

func addManageFlags(cmd *cobra.Command, cfg *manageConfig) {
	cmd.Flags().StringVar(&cfg.ServerURL, "server", cfg.ServerURL, "gnar server URL")
	cmd.Flags().StringVar(&cfg.Token, "token", cfg.Token, "management token")
	cmd.Flags().StringVar(&cfg.Tenant, "tenant", cfg.Tenant, "tenant filter")
	cmd.Flags().BoolVar(&cfg.JSON, "json", cfg.JSON, "print JSON")
}

func printTunnelList(tunnels []api.TunnelSummary) {
	if len(tunnels) == 0 {
		fmt.Println("no active tunnels")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TENANT\tNAME\tSTATUS\tPUBLIC\tTARGET\tREQS\tLAST")
	for _, tunnel := range tunnels {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			tunnel.Tenant,
			tunnel.Name,
			tunnel.Status,
			tunnel.PublicURL,
			tunnel.Target,
			tunnel.TotalRequests,
			tunnel.LastSeen.Format(time.RFC3339),
		)
	}
	_ = w.Flush()
}

func printTunnelDetail(detail api.TunnelDetailResponse) {
	fmt.Printf("Tenant:  %s\n", detail.Tunnel.Tenant)
	fmt.Printf("Name:    %s\n", detail.Tunnel.Name)
	fmt.Printf("Status:  %s\n", detail.Tunnel.Status)
	fmt.Printf("Target:  %s\n", detail.Tunnel.Target)
	fmt.Printf("Public:  %s\n", detail.Tunnel.PublicURL)
	for _, value := range detail.Tunnel.URLs {
		if value == detail.Tunnel.PublicURL {
			continue
		}
		fmt.Printf("Alias:   %s\n", value)
	}
	fmt.Printf("Requests:%d total, %d active\n", detail.Tunnel.TotalRequests, detail.Tunnel.ActiveRequests)
	if detail.Tunnel.LastError != "" {
		fmt.Printf("Error:   %s\n", detail.Tunnel.LastError)
	}
	if len(detail.RecentRequests) > 0 {
		fmt.Println("Recent:")
		printLogs(detail.Tunnel, detail.RecentRequests)
	}
}

func printLogs(tunnel api.TunnelSummary, logs []api.RequestLogEntry) {
	if len(logs) == 0 {
		fmt.Printf("%s has no recent requests\n", tunnel.Name)
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tMETHOD\tPATH\tDURATION\tREMOTE\tERROR")
	for _, entry := range logs {
		duration := time.Duration(entry.DurationMS) * time.Millisecond
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			entry.StatusCode,
			entry.Method,
			entry.Path,
			duration,
			entry.RemoteAddr,
			entry.Error,
		)
	}
	_ = w.Flush()
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func normalizeDoctorTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("target is required")
	}
	if _, err := strconv.Atoi(value); err == nil {
		return "http://127.0.0.1:" + value, nil
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid target")
	}
	return parsed.String(), nil
}

func checkServer(timeout time.Duration, serverURL, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return api.NewClient(serverURL, token).Health(ctx)
}

func checkLocal(client *http.Client, target string) error {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("local target returned %s", resp.Status)
	}
	return nil
}
