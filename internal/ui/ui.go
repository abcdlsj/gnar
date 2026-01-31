package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	primaryColor   = lipgloss.Color("86")
	secondaryColor = lipgloss.Color("99")
	successColor   = lipgloss.Color("78")
	warningColor   = lipgloss.Color("214")
	errorColor     = lipgloss.Color("196")
	mutedColor     = lipgloss.Color("241")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1)

	labelStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Width(12)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	successStyle = lipgloss.NewStyle().
			Foreground(successColor)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(secondaryColor)
)

type ServerInfo struct {
	Version   string
	Port      int
	AdminPort int
	Domain    string
	Multiplex bool
}

type ClientInfo struct {
	Version   string
	SvrAddr   string
	Multiplex bool
	Token     bool
}

type ProxyInfo struct {
	Name       string
	LocalPort  int
	RemotePort int
	ProxyType  string
	Subdomain  string
	SpeedLimit string
}

type TunnelInfo struct {
	LocalAddr  string
	RemotePort int
	Domain     string
}

func RenderServerBanner(info ServerInfo) string {
	var sb strings.Builder

	title := titleStyle.Render("GNAR " + info.Version)
	subtitle := lipgloss.NewStyle().Foreground(mutedColor).Render("Proxy Tool with Auto-HTTPS")

	header := boxStyle.Width(40).Render(
		fmt.Sprintf("\n   %s\n   %s\n", title, subtitle),
	)
	sb.WriteString(header)
	sb.WriteString("\n\n")

	muxStatus := "disabled"
	if info.Multiplex {
		muxStatus = successStyle.Render("enabled")
	}

	adminAddr := "-"
	if info.AdminPort != 0 {
		adminAddr = fmt.Sprintf("http://localhost:%d", info.AdminPort)
	}

	domain := info.Domain
	if domain == "" {
		domain = "-"
	}

	content := fmt.Sprintf(
		"  %s%s\n  %s%s\n  %s%s\n  %s%s",
		labelStyle.Render("Port"),
		valueStyle.Render(fmt.Sprintf("%d", info.Port)),
		labelStyle.Render("Admin"),
		valueStyle.Render(adminAddr),
		labelStyle.Render("Domain"),
		valueStyle.Render(domain),
		labelStyle.Render("Multiplex"),
		muxStatus,
	)

	serverBox := headerStyle.Render("Server") + "\n" + boxStyle.Width(40).Render(content)
	sb.WriteString(serverBox)

	return sb.String()
}

func RenderClientBanner(info ClientInfo) string {
	var sb strings.Builder

	title := titleStyle.Render("GNAR " + info.Version)
	subtitle := lipgloss.NewStyle().Foreground(mutedColor).Render("Proxy Client")

	header := boxStyle.Width(40).Render(
		fmt.Sprintf("\n   %s\n   %s\n", title, subtitle),
	)
	sb.WriteString(header)
	sb.WriteString("\n\n")

	muxStatus := "disabled"
	if info.Multiplex {
		muxStatus = successStyle.Render("enabled")
	}

	tokenStatus := "disabled"
	if info.Token {
		tokenStatus = successStyle.Render("enabled")
	}

	content := fmt.Sprintf(
		"  %s%s\n  %s%s\n  %s%s",
		labelStyle.Render("Server"),
		valueStyle.Render(info.SvrAddr),
		labelStyle.Render("Multiplex"),
		muxStatus,
		labelStyle.Render("Token"),
		tokenStatus,
	)

	clientBox := headerStyle.Render("Client") + "\n" + boxStyle.Width(40).Render(content)
	sb.WriteString(clientBox)

	return sb.String()
}

func RenderTunnelActive(info TunnelInfo) string {
	domain := info.Domain
	if domain == "" {
		domain = fmt.Sprintf(":%d", info.RemotePort)
	} else {
		domain = "https://" + domain
	}

	content := fmt.Sprintf(
		"  %s\n\n  %s%s\n  %s%s\n  %s%s\n\n  %s",
		successStyle.Bold(true).Render("Tunnel Active"),
		labelStyle.Render("Local"),
		valueStyle.Render(info.LocalAddr),
		labelStyle.Render("Remote"),
		valueStyle.Render(fmt.Sprintf(":%d", info.RemotePort)),
		labelStyle.Render("Domain"),
		valueStyle.Render(domain),
		lipgloss.NewStyle().Foreground(mutedColor).Render("Press Ctrl+C to disconnect"),
	)

	return boxStyle.Width(42).Render(content)
}

func RenderProxyList(proxies []ProxyInfo) string {
	if len(proxies) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Proxies") + "\n")

	for i, p := range proxies {
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("%s:%d->%d", p.ProxyType, p.LocalPort, p.RemotePort)
		}

		subdomain := p.Subdomain
		if subdomain == "" {
			subdomain = "-"
		}

		speedLimit := p.SpeedLimit
		if speedLimit == "" {
			speedLimit = "-"
		}

		content := fmt.Sprintf(
			"  %s%s\n  %s%s\n  %s%s\n  %s%s",
			labelStyle.Render("Name"),
			valueStyle.Render(name),
			labelStyle.Render("Type"),
			valueStyle.Render(strings.ToUpper(p.ProxyType)),
			labelStyle.Render("Ports"),
			valueStyle.Render(fmt.Sprintf("%d -> %d", p.LocalPort, p.RemotePort)),
			labelStyle.Render("Subdomain"),
			valueStyle.Render(subdomain),
		)

		sb.WriteString(boxStyle.Width(40).Render(content))
		if i < len(proxies)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
