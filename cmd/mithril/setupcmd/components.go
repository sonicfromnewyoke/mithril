package setupcmd

import (
	"fmt"
	"strings"

	"github.com/sonicfromnewyoke/mithril/pkg/tui"
	"github.com/charmbracelet/lipgloss"
)

// ── Menu Component ──────────────────────────────────────────────────────

type menuItem struct {
	label string
	value string
	desc  string
	isSep bool
}

func menuOption(label, value string) menuItem {
	return menuItem{label: label, value: value}
}

func menuOptionDesc(label, value, desc string) menuItem {
	return menuItem{label: label, value: value, desc: desc}
}

func menuSeparator() menuItem {
	return menuItem{isSep: true}
}

func menuBack() menuItem {
	return menuItem{label: "← Back", value: "_back"}
}

func renderMenu(title, description string, items []menuItem, cursor int, _ int) string {
	var b strings.Builder

	// ── Header ──
	b.WriteString("\n")
	b.WriteString("  " + lipgloss.NewStyle().
		Foreground(mithrilTeal).
		Bold(true).
		Render(title))
	b.WriteString("\n")

	if description != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(colorTextMuted).
			Render(description))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// ── Find max label width ──
	maxLabel := 0
	for _, item := range items {
		if !item.isSep && len(item.label) > maxLabel {
			maxLabel = len(item.label)
		}
	}

	// ── Items ──
	for i, item := range items {
		if item.isSep {
			b.WriteString("  " + lipgloss.NewStyle().
				Foreground(colorTextDisabled).
				Render(strings.Repeat("·", maxLabel+6)))
			b.WriteString("\n")
			continue
		}

		padded := fmt.Sprintf("%-*s", maxLabel+1, item.label)

		if i == cursor {
			// Selected: teal arrow + teal label + dim description on right
			arrow := lipgloss.NewStyle().Foreground(mithrilTeal).Bold(true).Render(" ▸ ")
			label := lipgloss.NewStyle().Foreground(mithrilTeal).Bold(true).Render(padded)
			line := arrow + label
			if item.desc != "" {
				line += " " + lipgloss.NewStyle().Foreground(colorTextMuted).Render(item.desc)
			}
			b.WriteString(line)
		} else {
			// Normal: indented, secondary color
			label := lipgloss.NewStyle().Foreground(colorTextSecondary).Render(padded)
			b.WriteString("   " + label)
		}
		b.WriteString("\n")
	}

	// ── Help ──
	b.WriteString("\n")
	k := lipgloss.NewStyle().Foreground(mithrilTeal)
	h := lipgloss.NewStyle().Foreground(colorTextDisabled)

	hasBack := false
	for _, item := range items {
		if item.value == "_back" {
			hasBack = true
			break
		}
	}

	help := "  " + k.Render("↑↓") + h.Render(" navigate") +
		"  " + k.Render("⏎") + h.Render(" select")
	if hasBack {
		help += "  " + k.Render("esc") + h.Render(" back")
	} else {
		help += "  " + k.Render("q") + h.Render(" quit")
	}
	b.WriteString(help)

	return b.String()
}

// ── Input Component ─────────────────────────────────────────────────────

func renderInput(title, description, value, errMsg string, cursorPos int) string {
	var b strings.Builder

	// ── Header ──
	b.WriteString("\n")
	b.WriteString("  " + lipgloss.NewStyle().
		Foreground(mithrilTeal).
		Bold(true).
		Render(title))
	b.WriteString("\n")

	if description != "" {
		lines := strings.Split(description, "\n")
		for _, line := range lines {
			b.WriteString("  " + lipgloss.NewStyle().
				Foreground(colorTextMuted).
				Render(line))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// ── Input field ──
	// Build text with block cursor
	text := value
	if cursorPos >= 0 && cursorPos <= len(text) {
		before := text[:cursorPos]
		after := text[cursorPos:]
		cursor := lipgloss.NewStyle().
			Background(mithrilTeal).
			Foreground(lipgloss.Color("#000000")).
			Render(" ")
		if cursorPos < len(text) {
			cursor = lipgloss.NewStyle().
				Background(mithrilTeal).
				Foreground(lipgloss.Color("#000000")).
				Render(string(after[0]))
			after = after[1:]
		}
		text = before + cursor + after
	}

	prompt := lipgloss.NewStyle().Foreground(mithrilTeal).Render("❯ ")
	b.WriteString("  " + prompt + text)
	b.WriteString("\n")

	// ── Underline ──
	underLen := len(value) + 2
	if underLen < 30 {
		underLen = 30
	}
	b.WriteString("  " + lipgloss.NewStyle().Foreground(colorBorderActive).Render(strings.Repeat("─", underLen)))
	b.WriteString("\n")

	// ── Error ──
	if errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(colorError).
			Render("✗ "+errMsg))
		b.WriteString("\n")
	}

	// ── Help ──
	b.WriteString("\n")
	k := lipgloss.NewStyle().Foreground(mithrilTeal)
	h := lipgloss.NewStyle().Foreground(colorTextDisabled)
	b.WriteString("  " + k.Render("⏎") + h.Render(" confirm") +
		"  " + k.Render("esc") + h.Render(" back") +
		"  " + k.Render("←→") + h.Render(" cursor"))

	return b.String()
}

// ── Review Box ──────────────────────────────────────────────────────────

func renderReview(title string, rows [][]string) string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString("  " + lipgloss.NewStyle().
		Foreground(mithrilTeal).
		Bold(true).
		Render(title))
	b.WriteString("\n\n")

	// Find max label width
	maxLabel := 0
	for _, row := range rows {
		if len(row[0]) > maxLabel {
			maxLabel = len(row[0])
		}
	}

	// Build aligned content
	var content strings.Builder
	for _, row := range rows {
		label := lipgloss.NewStyle().
			Foreground(colorTextMuted).
			Render(fmt.Sprintf("%-*s", maxLabel+1, row[0]))
		value := lipgloss.NewStyle().
			Foreground(colorTextPrimary).
			Render(row[1])
		content.WriteString("  " + label + " " + value + "\n")
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Render(content.String())

	b.WriteString(box)
	return b.String()
}

// ── Logo ────────────────────────────────────────────────────────────

func renderLogo() string {
	return tui.RenderLogo()
}

// ── Done Screen ─────────────────────────────────────────────────────

func renderDone(configPath string, err error) string {
	var b strings.Builder
	k := lipgloss.NewStyle().Foreground(mithrilTeal)
	h := lipgloss.NewStyle().Foreground(colorTextDisabled)

	if err != nil {
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(mithrilTeal).Bold(true).Render("Setup Failed"))
		b.WriteString("\n\n")
		b.WriteString("  " + lipgloss.NewStyle().Foreground(colorError).Render("✗ "+err.Error()))
		b.WriteString("\n\n  Press " + k.Render("q") + h.Render(" to exit"))
		b.WriteString("\n")
		return b.String()
	}

	b.WriteString("\n  " + lipgloss.NewStyle().Foreground(mithrilTeal).Bold(true).Render("Setup Complete"))
	b.WriteString("\n\n")
	b.WriteString("  " + lipgloss.NewStyle().Foreground(colorSuccess).Render("✓") +
		lipgloss.NewStyle().Foreground(colorTextPrimary).Render(" Config written to "+configPath))
	b.WriteString("\n\n")

	cmdStyle := lipgloss.NewStyle().Foreground(colorTextSecondary)
	b.WriteString("  " + lipgloss.NewStyle().Foreground(colorTextMuted).Render("Next steps:"))
	b.WriteString("\n")
	b.WriteString("  " + cmdStyle.Render("$ mithril run --config "+configPath))
	b.WriteString("\n")
	b.WriteString("  " + cmdStyle.Render("$ mithril doctor --config "+configPath))
	b.WriteString("\n\n")
	b.WriteString("  Press " + k.Render("q") + h.Render(" to exit"))
	b.WriteString("\n")

	return b.String()
}
