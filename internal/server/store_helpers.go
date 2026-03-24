package server

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/abcdlsj/gnar/internal/norm"
)

func extractTenantSlug(requestPath string) (string, string, string, bool) {
	if requestPath == "" || requestPath == "/" {
		return "", "", "", false
	}

	trimmed := strings.TrimPrefix(requestPath, "/")
	parts := strings.SplitN(trimmed, "/", 4)
	if len(parts) < 3 || parts[0] != "t" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}

	forwardedPath := "/"
	if len(parts) == 4 {
		forwardedPath = "/" + parts[3]
	}

	return norm.Tenant(parts[1]), parts[2], forwardedPath, true
}

func validateDomains(domains []string, tenant string, cfg Config) error {
	if len(domains) == 0 {
		return nil
	}

	allowed := append([]string(nil), cfg.AllowedDomainSuffixes...)
	allowed = append(allowed, cfg.TenantDomainSuffixes[tenant]...)
	if len(allowed) == 0 {
		return nil
	}

	for _, domain := range domains {
		allowedForDomain := false
		for _, suffix := range allowed {
			if domainHasSuffix(domain, suffix) {
				allowedForDomain = true
				break
			}
		}
		if !allowedForDomain {
			return fmt.Errorf("domain not allowed for tenant %s: %s", tenant, domain)
		}
	}
	return nil
}

func domainHasSuffix(domain, suffix string) bool {
	domain = norm.Host(domain)
	suffix = strings.TrimSpace(strings.ToLower(suffix))
	if domain == "" || suffix == "" {
		return false
	}
	return strings.HasSuffix(domain, suffix) && len(domain) > len(suffix)
}

func joinPathURL(origin, suffix string) string {
	base, err := url.Parse(origin)
	if err != nil {
		return origin + suffix
	}
	base.Path = path.Join(base.Path, suffix)
	if !strings.HasPrefix(base.Path, "/") {
		base.Path = "/" + base.Path
	}
	return base.String()
}

func joinHostURL(origin, host string) string {
	base, err := url.Parse(origin)
	if err != nil {
		return origin
	}
	base.Host = host
	base.Path = ""
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String()
}
