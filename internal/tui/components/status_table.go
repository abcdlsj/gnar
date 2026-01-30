package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/abcdlsj/gnar/pkg/tunnel"
)

// StatusTable displays active tunnels in a table.
type StatusTable struct {
	table   table.Model
	tunnels []*tunnel.Tunnel
	width   int
	height  int
}

// NewStatusTable creates a new status table.
func NewStatusTable(tunnels []*tunnel.Tunnel, width, height int) *StatusTable {
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#059669"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#0891B2")).Underline(true)

	columns := []table.Column{
		{Title: "Local", Width: 20},
		{Title: "Public URL", Width: 40},
		{Title: "Status", Width: 15},
		{Title: "Traffic", Width: 20},
	}

	rows := []table.Row{}
	for _, t := range tunnels {
		status := activeStyle.Render("active")
		if t.Status != tunnel.TunnelStatusActive {
			status = mutedStyle.Render("pending")
		}

		stats := t.GetStats()
		traffic := fmt.Sprintf("%s / %s",
			formatBytes(stats.BytesSent),
			formatBytes(stats.BytesRecv))

		rows = append(rows, table.Row{
			fmt.Sprintf("localhost:%d", t.LocalPort),
			urlStyle.Render(t.PublicURL),
			status,
			traffic,
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(height-4),
	)

	return &StatusTable{
		table:   t,
		tunnels: tunnels,
		width:   width,
		height:  height,
	}
}

// Init initializes the table.
func (s *StatusTable) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (s *StatusTable) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// View renders the table.
func (s *StatusTable) View() string {
	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#059669"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	var b strings.Builder

	b.WriteString(successStyle.Render("Service exposed successfully!") + "\n\n")
	b.WriteString(s.table.View())
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("Press Ctrl+C to stop  |  Press c to copy URL  |  Press q to quit"))

	return b.String()
}

// formatBytes formats bytes to human readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
