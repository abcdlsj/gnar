package components

import (
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// AuthForm is a form for entering authentication tokens.
type AuthForm struct {
	form   *huh.Form
	server string
	width  int
	height int
	token  string
}

// NewAuthForm creates a new authentication form.
func NewAuthForm(server string, width, height int) *AuthForm {
	return &AuthForm{
		server: server,
		width:  width,
		height: height,
	}
}

// Init initializes the form.
func (a *AuthForm) Init() tea.Cmd {
	a.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Authentication").
				Description("Login to "+a.server),
			huh.NewInput().
				Key("token").
				Title("Token").
				EchoMode(huh.EchoModePassword),
		),
	).WithTheme(huh.ThemeCharm())

	return a.form.Init()
}

// Update handles messages.
func (a *AuthForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := a.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		a.form = f
	}

	if a.form.State == huh.StateCompleted {
		a.token = a.form.GetString("token")
		return a, func() tea.Msg {
			return AuthSuccessMsg{Token: a.token}
		}
	}

	return a, cmd
}

// View renders the form.
func (a *AuthForm) View() string {
	return a.form.View()
}
