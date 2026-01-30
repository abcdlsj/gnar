package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ServerItem represents a server item in the list.
type ServerItem struct {
	Address string
	Default bool
}

// FilterValue returns the filter value for the item.
func (i ServerItem) FilterValue() string {
	return i.Address
}

// Title returns the title for the item.
func (i ServerItem) Title() string {
	if i.Default {
		return fmt.Sprintf("%s (default)", i.Address)
	}
	return i.Address
}

// Description returns the description for the item.
func (i ServerItem) Description() string {
	return ""
}

// ServerSelector is a list for selecting servers.
type ServerSelector struct {
	list   list.Model
	width  int
	height int
}

// NewServerSelector creates a new server selector.
func NewServerSelector(width, height int) *ServerSelector {
	items := []list.Item{
		ServerItem{Address: "gnar.example.com", Default: true},
		ServerItem{Address: "localhost:8443", Default: false},
	}

	l := list.New(items, list.NewDefaultDelegate(), width, height)
	l.Title = "Select a server"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED"))

	return &ServerSelector{
		list:   l,
		width:  width,
		height: height,
	}
}

// Init initializes the selector.
func (s *ServerSelector) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (s *ServerSelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.list.SetSize(msg.Width, msg.Height)
	case tea.KeyMsg:
		if msg.String() == "enter" {
			if item, ok := s.list.SelectedItem().(ServerItem); ok {
				return s, func() tea.Msg {
					return ServerSelectedMsg{Server: item.Address}
				}
			}
		}
	}

	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

// View renders the selector.
func (s *ServerSelector) View() string {
	return s.list.View()
}
