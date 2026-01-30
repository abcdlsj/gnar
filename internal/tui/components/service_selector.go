package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ServiceItem represents a local service item in the list.
type ServiceItem struct {
	Port     int
	Protocol string
	Info     string
}

// FilterValue returns the filter value for the item.
func (i ServiceItem) FilterValue() string {
	return fmt.Sprintf("%d %s", i.Port, i.Info)
}

// Title returns the title for the item.
func (i ServiceItem) Title() string {
	return fmt.Sprintf("%d", i.Port)
}

// Description returns the description for the item.
func (i ServiceItem) Description() string {
	return i.Info
}

// ServiceSelector is a list for selecting local services.
type ServiceSelector struct {
	list   list.Model
	width  int
	height int
}

// NewServiceSelector creates a new service selector.
func NewServiceSelector(width, height int) *ServiceSelector {
	items := []list.Item{
		ServiceItem{Port: 3000, Protocol: "http", Info: "Next.js dev server"},
		ServiceItem{Port: 8080, Protocol: "http", Info: "Spring Boot"},
		ServiceItem{Port: 5000, Protocol: "http", Info: "Flask / Express"},
	}

	l := list.New(items, list.NewDefaultDelegate(), width, height)
	l.Title = "Select a local service to expose"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED"))

	return &ServiceSelector{
		list:   l,
		width:  width,
		height: height,
	}
}

// Init initializes the selector.
func (s *ServiceSelector) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (s *ServiceSelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.list.SetSize(msg.Width, msg.Height)
	}

	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)

	if s.list.SelectedItem() != nil {
		if item, ok := s.list.SelectedItem().(ServiceItem); ok {
			return s, func() tea.Msg {
				return ServiceSelectedMsg{Port: item.Port}
			}
		}
	}

	return s, cmd
}

// View renders the selector.
func (s *ServiceSelector) View() string {
	return s.list.View()
}
