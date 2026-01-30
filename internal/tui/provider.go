package tui

import "github.com/charmbracelet/bubbles/table"

type StatusProvider interface {
	Columns() []table.Column
	Rows() []table.Row
}
