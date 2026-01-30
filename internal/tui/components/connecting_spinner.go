package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConnectingSpinner shows a spinner while connecting.
type ConnectingSpinner struct {
	spinner spinner.Model
	server  string
	steps   []string
	current int
	width   int
	height  int
}

// NewConnectingSpinner creates a new connecting spinner.
func NewConnectingSpinner(server string, width, height int) *ConnectingSpinner {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))

	return &ConnectingSpinner{
		spinner: s,
		server:  server,
		steps: []string{
			"Checking authentication...",
			"Connecting to server...",
			"Negotiating port...",
			"Registering domain...",
			"Establishing tunnel...",
		},
		current: 0,
		width:   width,
		height:  height,
	}
}

// Init initializes the spinner.
func (c *ConnectingSpinner) Init() tea.Cmd {
	return c.spinner.Tick
}

// Update handles messages.
func (c *ConnectingSpinner) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	case spinner.TickMsg:
		var cmd tea.Cmd
		c.spinner, cmd = c.spinner.Update(msg)
		return c, cmd
	}

	return c, nil
}

// View renders the spinner.
func (c *ConnectingSpinner) View() string {
	var b strings.Builder

	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#0891B2"))
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#059669"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	b.WriteString(fmt.Sprintf("\n%s Connecting to %s\n\n",
		c.spinner.View(),
		infoStyle.Render(c.server)))

	for i, step := range c.steps {
		if i < c.current {
			b.WriteString(successStyle.Render(fmt.Sprintf("  %s %s", "[x]", step)) + "\n")
		} else if i == c.current {
			b.WriteString(fmt.Sprintf("  %s %s\n", c.spinner.View(), step))
		} else {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  %s %s", "[ ]", step)) + "\n")
		}
	}

	return b.String()
}
