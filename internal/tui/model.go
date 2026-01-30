package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxLogLines = 1000

type Model struct {
	header   string
	table    table.Model
	logView  viewport.Model
	logs     []string
	provider StatusProvider
	onQuit   func()

	focusLog bool
	ready    bool
	width    int
	height   int
}

func NewModel(header string, provider StatusProvider, onQuit func()) Model {
	cols := provider.Columns()
	rows := provider.Rows()

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(5),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	t.SetStyles(s)

	return Model{
		header:   header,
		table:    t,
		provider: provider,
		onQuit:   onQuit,
	}
}

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.onQuit != nil {
				m.onQuit()
			}
			return m, tea.Quit
		case "tab":
			m.focusLog = !m.focusLog
			m.table.Focus()
			if m.focusLog {
				m.table.Blur()
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.recalcLayout()
		return m, nil

	case LogMsg:
		m.logs = append(m.logs, string(msg))
		if len(m.logs) > maxLogLines {
			m.logs = m.logs[len(m.logs)-maxLogLines:]
		}
		m.logView.SetContent(strings.Join(m.logs, "\n"))
		m.logView.GotoBottom()
		return m, nil

	case TickMsg:
		m.table.SetRows(m.provider.Rows())
		return m, tickCmd()
	}

	if m.focusLog {
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) recalcLayout() {
	headerH := lipgloss.Height(m.header) + 2 // border
	footerH := 1
	tableH := min(len(m.provider.Rows())+3, 10) // header + rows + border
	logH := m.height - headerH - footerH - tableH - 2

	if logH < 3 {
		logH = 3
	}

	innerW := m.width - 2 // border padding

	m.table.SetWidth(innerW)
	m.table.SetHeight(tableH - 2)

	if m.logView.Width == 0 && m.logView.Height == 0 {
		m.logView = viewport.New(innerW, logH)
	} else {
		m.logView.Width = innerW
		m.logView.Height = logH
	}
	m.logView.SetContent(strings.Join(m.logs, "\n"))
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	header := headerStyle.Width(m.width - 2).Render(m.header)

	tableView := tableStyle.Width(m.width - 2).Render(m.table.View())

	logBorder := logStyle
	if m.focusLog {
		logBorder = logFocusedStyle
	}
	logView := logBorder.Width(m.width - 2).Render(m.logView.View())

	footer := footerStyle.Render("  [tab] switch focus  [↑/↓] scroll  [q] quit")

	return fmt.Sprintf("%s\n%s\n%s\n%s", header, tableView, logView, footer)
}

func Run(header string, provider StatusProvider, logWriter *LogWriter, onQuit func()) error {
	m := NewModel(header, provider, onQuit)
	p := tea.NewProgram(m, tea.WithAltScreen())
	logWriter.SetProgram(p)
	_, err := p.Run()
	return err
}
