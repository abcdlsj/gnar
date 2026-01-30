package tui

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

type LogWriter struct {
	prog *tea.Program
	mu   sync.Mutex
}

func NewLogWriter() *LogWriter {
	return &LogWriter{}
}

func (w *LogWriter) SetProgram(p *tea.Program) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prog = p
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	line := strings.TrimRight(string(p), "\n")
	w.mu.Lock()
	prog := w.prog
	w.mu.Unlock()

	if prog != nil {
		prog.Send(LogMsg(line))
	}
	return len(p), nil
}
