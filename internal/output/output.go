// Package output provides styled console output utilities.
package output

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Color scheme.
var (
	primary = lipgloss.Color("#7C3AED")
	success = lipgloss.Color("#059669")
	error   = lipgloss.Color("#DC2626")
	warning = lipgloss.Color("#D97706")
	info    = lipgloss.Color("#0891B2")
	muted   = lipgloss.Color("#6B7280")
	white   = lipgloss.Color("#FFFFFF")
)

// Styles.
var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(primary)
	successStyle = lipgloss.NewStyle().Foreground(success)
	errorStyle   = lipgloss.NewStyle().Foreground(error)
	warningStyle = lipgloss.NewStyle().Foreground(warning)
	infoStyle    = lipgloss.NewStyle().Foreground(info)
	mutedStyle   = lipgloss.NewStyle().Foreground(muted)
	labelStyle   = lipgloss.NewStyle().Bold(true).Foreground(primary)
	valueStyle   = lipgloss.NewStyle().Foreground(white)
	urlStyle     = lipgloss.NewStyle().Foreground(info).Underline(true)
)

// Printf prints a formatted string with styles applied.
func Printf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

// Println prints a line.
func Println(a ...interface{}) {
	fmt.Println(a...)
}

// Title prints a title.
func Title(format string, args ...interface{}) {
	fmt.Println(titleStyle.Render(fmt.Sprintf(format, args...)))
}

// Success prints success message.
func Success(format string, args ...interface{}) {
	fmt.Println(successStyle.Render(fmt.Sprintf(format, args...)))
}

// Error prints error message.
func Error(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, errorStyle.Render(fmt.Sprintf(format, args...)))
}

// Warning prints warning message.
func Warning(format string, args ...interface{}) {
	fmt.Println(warningStyle.Render(fmt.Sprintf(format, args...)))
}

// Info prints info message.
func Info(format string, args ...interface{}) {
	fmt.Println(infoStyle.Render(fmt.Sprintf(format, args...)))
}

// Muted prints muted/secondary text.
func Muted(format string, args ...interface{}) {
	fmt.Println(mutedStyle.Render(fmt.Sprintf(format, args...)))
}

// Label prints a label.
func Label(format string, args ...interface{}) {
	fmt.Print(labelStyle.Render(fmt.Sprintf(format, args...)))
}

// Value prints a value.
func Value(format string, args ...interface{}) {
	fmt.Println(valueStyle.Render(fmt.Sprintf(format, args...)))
}

// URL prints a URL.
func URL(format string, args ...interface{}) {
	fmt.Println(urlStyle.Render(fmt.Sprintf(format, args...)))
}

// Pair prints a label-value pair.
func Pair(label, value string) {
	fmt.Printf("%s %s\n", labelStyle.Render(label), valueStyle.Render(value))
}

// Line prints a separator line.
func Line() {
	fmt.Println(mutedStyle.Render("─────────────────────────────────────"))
}

// Fatal prints error and exits.
func Fatal(format string, args ...interface{}) {
	Error(format, args...)
	os.Exit(1)
}

// Sprint returns styled string without printing.
func Sprint(style string, format string, args ...interface{}) string {
	switch style {
	case "title":
		return titleStyle.Render(fmt.Sprintf(format, args...))
	case "success":
		return successStyle.Render(fmt.Sprintf(format, args...))
	case "error":
		return errorStyle.Render(fmt.Sprintf(format, args...))
	case "warning":
		return warningStyle.Render(fmt.Sprintf(format, args...))
	case "info":
		return infoStyle.Render(fmt.Sprintf(format, args...))
	case "muted":
		return mutedStyle.Render(fmt.Sprintf(format, args...))
	case "url":
		return urlStyle.Render(fmt.Sprintf(format, args...))
	default:
		return fmt.Sprintf(format, args...)
	}
}
