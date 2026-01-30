package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type LogMsg string

type TickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}
