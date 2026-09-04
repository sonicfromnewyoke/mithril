package setupcmd

import (
	"github.com/sonicfromnewyoke/mithril/pkg/tui"
	"github.com/charmbracelet/lipgloss"
)

// Re-export shared theme for use within setupcmd package.
var (
	mithrilTeal = tui.MithrilTeal

	colorTextPrimary   = tui.ColorTextPrimary
	colorTextSecondary = tui.ColorTextSecondary
	colorTextMuted     = tui.ColorTextMuted
	colorTextDisabled  = tui.ColorTextDisabled

	colorSuccess = tui.ColorSuccess
	colorError   = tui.ColorError
	colorWarn    = tui.ColorWarn

	colorBorder       = tui.ColorBorder
	colorBorderActive = tui.ColorBorderActive
)

// Shared styles (re-exported from tui package)
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(mithrilTeal)

	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorError)

	warnStyle = lipgloss.NewStyle().
			Foreground(colorWarn)

	dimStyle = lipgloss.NewStyle().
			Foreground(colorTextMuted)
)
