package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbletea"
)

type App struct {
	state    string
	errMsg   string
	quitting bool
	width    int
	height   int
}

func NewApp() *App {
	return &App{state: "init"}
}

func (a *App) Init() tea.Cmd {
	return tea.EnterAltScreen
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, nil
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			a.quitting = true
			return a, tea.Quit
		}
	}
	return a, nil
}

func (a *App) View() string {
	if a.quitting {
		return ""
	}
	return fmt.Sprintf("Gnar TUI - Press q to quit (%dx%d)", a.width, a.height)
}

func Run(ctx context.Context) error {
	app := NewApp()
	p := tea.NewProgram(app, tea.WithAltScreen())
	go func() {
		<-ctx.Done()
		p.Quit()
	}()
	_, err := p.Run()
	return err
}
