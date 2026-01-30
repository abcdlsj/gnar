// Package tui provides the terminal user interface for gnar.
package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color scheme - no icons, just colors.
var (
	// Primary colors
	primary   = lipgloss.Color("#7C3AED") // Purple
	secondary = lipgloss.Color("#2563EB") // Blue
	success   = lipgloss.Color("#059669") // Green
	warning   = lipgloss.Color("#D97706") // Amber
	error     = lipgloss.Color("#DC2626") // Red
	info      = lipgloss.Color("#0891B2") // Cyan

	// Neutral colors
	muted = lipgloss.Color("#6B7280") // Gray
	light = lipgloss.Color("#E5E7EB") // Light gray
	dark  = lipgloss.Color("#1F2937") // Dark gray
	white = lipgloss.Color("#FFFFFF")
	black = lipgloss.Color("#000000")
)

// Text styles.
var (
	// Title style - bold, primary color
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primary).
			PaddingBottom(1)

	// Header style
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(white).
			Background(primary).
			Padding(0, 1)

	// Success text
	SuccessStyle = lipgloss.NewStyle().
			Foreground(success).
			Bold(true)

	// Error text
	ErrorStyle = lipgloss.NewStyle().
			Foreground(error).
			Bold(true)

	// Warning text
	WarningStyle = lipgloss.NewStyle().
			Foreground(warning).
			Bold(true)

	// Info text
	InfoStyle = lipgloss.NewStyle().
			Foreground(info)

	// Muted text - for secondary info
	MutedStyle = lipgloss.NewStyle().
			Foreground(muted)

	// Label text - for field labels
	LabelStyle = lipgloss.NewStyle().
			Foreground(secondary).
			Bold(true)

	// Value text - for field values
	ValueStyle = lipgloss.NewStyle().
			Foreground(white)

	// URL style - underlined, info color
	URLStyle = lipgloss.NewStyle().
			Foreground(info).
			Underline(true)

	// Active/Selected style
	ActiveStyle = lipgloss.NewStyle().
			Foreground(success).
			Bold(true)

	// Inactive style
	InactiveStyle = lipgloss.NewStyle().
			Foreground(muted)

	// Border styles
	NormalBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(muted).
			Padding(1, 2)

	FocusedBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primary).
			Padding(1, 2)

	SuccessBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(success).
			Padding(1, 2)

	ErrorBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(error).
			Padding(1, 2)
)

// Helper functions for colored text without icons.

// Success returns success-colored text.
func Success(text string) string {
	return SuccessStyle.Render(text)
}

// Error returns error-colored text.
func Error(text string) string {
	return ErrorStyle.Render(text)
}

// Warning returns warning-colored text.
func Warning(text string) string {
	return WarningStyle.Render(text)
}

// Info returns info-colored text.
func Info(text string) string {
	return InfoStyle.Render(text)
}

// Muted returns muted-colored text.
func Muted(text string) string {
	return MutedStyle.Render(text)
}

// Label returns label-styled text.
func Label(text string) string {
	return LabelStyle.Render(text)
}

// URL returns URL-styled text.
func URL(text string) string {
	return URLStyle.Render(text)
}

// Active returns active-styled text.
func Active(text string) string {
	return ActiveStyle.Render(text)
}
