package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/abcdlsj/gnar/internal/norm"
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	cfg := DefaultConfig()
	var agentCredentials []string
	var allowedDomainSuffixes []string
	var tenantDomainSuffixes []string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the public edge and control plane",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cfg.AllowedDomainSuffixes = norm.Suffixes(allowedDomainSuffixes)
			for _, item := range agentCredentials {
				tenant, token, ok := strings.Cut(item, "=")
				if !ok {
					return fmt.Errorf("invalid agent credential: %s", item)
				}
				tenant = norm.Tenant(tenant)
				token = strings.TrimSpace(token)
				if token == "" {
					return fmt.Errorf("invalid agent credential: %s", item)
				}
				cfg.AgentCredentials[tenant] = token
			}
			for _, item := range tenantDomainSuffixes {
				tenant, suffix, ok := strings.Cut(item, "=")
				if !ok {
					return fmt.Errorf("invalid tenant domain suffix: %s", item)
				}
				tenant = norm.Tenant(tenant)
				normalized := norm.Suffixes([]string{suffix})
				if len(normalized) == 0 {
					return fmt.Errorf("invalid tenant domain suffix: %s", item)
				}
				cfg.TenantDomainSuffixes[tenant] = append(cfg.TenantDomainSuffixes[tenant], normalized...)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return Run(context.Background(), cfg)
		},
	}

	cmd.Flags().StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "listen address")
	cmd.Flags().StringVar(&cfg.PublicURL, "public-url", cfg.PublicURL, "public origin used in generated URLs")
	cmd.Flags().StringVar(&cfg.BaseDomain, "base-domain", cfg.BaseDomain, "base domain for generated hostnames")
	cmd.Flags().StringVar(&cfg.AgentToken, "agent-token", cfg.AgentToken, "shared token for agent registration")
	cmd.Flags().StringVar(&cfg.ManageToken, "manage-token", cfg.ManageToken, "token for management APIs")
	cmd.Flags().StringSliceVar(&agentCredentials, "agent-credential", nil, "tenant=token pair for agent registration")
	cmd.Flags().StringSliceVar(&allowedDomainSuffixes, "allow-domain-suffix", nil, "globally allowed custom domain suffix")
	cmd.Flags().StringSliceVar(&tenantDomainSuffixes, "tenant-domain-suffix", nil, "tenant=suffix pair for custom domain binding")
	cmd.Flags().DurationVar(&cfg.RequestTimeout, "request-timeout", cfg.RequestTimeout, "max time a public request can wait for an agent response")
	cmd.Flags().DurationVar(&cfg.IdleTimeout, "idle-timeout", cfg.IdleTimeout, "session idle timeout")
	cmd.Flags().DurationVar(&cfg.PollTimeout, "poll-timeout", cfg.PollTimeout, "long poll timeout")
	cmd.Flags().Int64Var(&cfg.MaxBodyBytes, "max-body-bytes", cfg.MaxBodyBytes, "max buffered request body size")
	return cmd
}
