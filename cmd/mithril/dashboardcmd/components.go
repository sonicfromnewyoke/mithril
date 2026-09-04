package dashboardcmd

import (
	"fmt"
	"strings"

	"github.com/sonicfromnewyoke/mithril/pkg/tui"
	"github.com/charmbracelet/lipgloss"
)

// ── Help Item ───────────────────────────────────────────────────────────

type helpItem struct {
	key  string
	desc string
}

func renderHelpBar(items []helpItem, width int) string {
	k := lipgloss.NewStyle().Foreground(tui.MithrilTeal)
	h := lipgloss.NewStyle().Foreground(tui.ColorTextDisabled)

	var parts []string
	for _, item := range items {
		parts = append(parts, k.Render(item.key)+h.Render(" "+item.desc))
	}
	line := "  " + strings.Join(parts, "  ")

	bg := lipgloss.NewStyle().Width(width)
	return bg.Render(line)
}

// ── Status Bar (top, below logo) ────────────────────────────────────────

type statusBarConfig struct {
	cluster   string
	slot      uint64
	epoch     uint64
	online    bool // at least one service responding
	hasConfig bool
}

func renderStatusBar(cfg statusBarConfig, width int) string {
	label := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	value := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary)
	sep := lipgloss.NewStyle().Foreground(tui.ColorBorder).Render(" │ ")

	var parts []string

	if cfg.hasConfig {
		parts = append(parts, label.Render("  ")+value.Render(cfg.cluster))
		if cfg.slot > 0 {
			parts = append(parts, label.Render("slot ")+value.Render(formatNumber(cfg.slot)))
			parts = append(parts, label.Render("epoch ")+value.Render(fmt.Sprintf("%d", cfg.epoch)))
			// Only show Online/Offline when node has actually produced state
			if cfg.online {
				parts = append(parts, lipgloss.NewStyle().Foreground(tui.ColorSuccess).Render("● Online"))
			} else {
				parts = append(parts, lipgloss.NewStyle().Foreground(tui.ColorError).Render("● Offline"))
			}
		}
	} else {
		parts = append(parts, label.Render("  no config"))
	}

	line := strings.Join(parts, sep)

	border := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(tui.ColorBorder).
		Width(width - 2).
		Padding(0, 1)

	return border.Render(line)
}

// ── Footer Bar ──────────────────────────────────────────────────────────

type footerConfig struct {
	version    string
	configFile string
}

func renderFooter(cfg footerConfig, width int) string {
	value := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	sep := lipgloss.NewStyle().Foreground(tui.ColorBorder).Render(" │ ")

	parts := []string{
		lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true).Render(" ◎ Mithril"),
	}
	if cfg.version != "" {
		parts = append(parts, value.Render(cfg.version))
	}
	if cfg.configFile != "" {
		parts = append(parts, value.Render(cfg.configFile))
	}

	return strings.Join(parts, sep)
}

// ── Split View ──────────────────────────────────────────────────────────

type splitViewConfig struct {
	leftTitle    string
	leftContent  string
	rightTitle   string
	rightContent string
	focusLeft    bool
}

func renderSplitView(cfg splitViewConfig, width, height int) string {
	if width < 10 || height < 3 {
		return "Terminal too small"
	}
	if width < 60 {
		return renderStackedView(cfg, width, height)
	}

	// Bordered split view with focus indicator
	innerWidth := width - 3 // left border + center divider + right border
	leftWidth := innerWidth * 22 / 100
	rightWidth := innerWidth - leftWidth

	// Focus-aware border colors
	leftBorderStyle := lipgloss.NewStyle().Foreground(tui.ColorBorder)
	rightBorderStyle := lipgloss.NewStyle().Foreground(tui.ColorBorder)
	dividerStyle := lipgloss.NewStyle().Foreground(tui.ColorBorder)
	leftTitleStyle := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	rightTitleStyle := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)

	leftIndicator := "── "
	rightIndicator := "── "
	if cfg.focusLeft {
		leftBorderStyle = lipgloss.NewStyle().Foreground(tui.MithrilTeal)
		leftTitleStyle = lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
		leftIndicator = "─► "
	} else {
		rightBorderStyle = lipgloss.NewStyle().Foreground(tui.MithrilTeal)
		rightTitleStyle = lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
		rightIndicator = "─► "
	}

	// Top border with titles
	leftTitle := leftBorderStyle.Render(leftIndicator) + leftTitleStyle.Render(cfg.leftTitle) + leftBorderStyle.Render(" ")
	leftTitlePad := leftWidth + 2 - lipgloss.Width(leftTitle)
	if leftTitlePad < 0 {
		leftTitlePad = 0
	}

	rightTitle := dividerStyle.Render(rightIndicator) + rightTitleStyle.Render(cfg.rightTitle) + rightBorderStyle.Render(" ")
	rightTitlePad := rightWidth + 2 - lipgloss.Width(rightTitle)
	if rightTitlePad < 0 {
		rightTitlePad = 0
	}

	top := leftBorderStyle.Render("┌") +
		leftTitle + leftBorderStyle.Render(strings.Repeat("─", leftTitlePad)) +
		dividerStyle.Render("┬") +
		rightTitle + rightBorderStyle.Render(strings.Repeat("─", rightTitlePad)) +
		rightBorderStyle.Render("┐")

	// Content rows
	leftLines := padLines(cfg.leftContent, leftWidth, height)
	rightLines := padLines(cfg.rightContent, rightWidth, height)

	var rows []string
	rows = append(rows, top)
	for i := 0; i < height; i++ {
		row := leftBorderStyle.Render("│") + " " + leftLines[i] + " " +
			dividerStyle.Render("│") + " " + rightLines[i] + " " +
			rightBorderStyle.Render("│")
		rows = append(rows, row)
	}

	// Bottom border
	bottom := leftBorderStyle.Render("└") +
		leftBorderStyle.Render(strings.Repeat("─", leftWidth+2)) +
		dividerStyle.Render("┴") +
		rightBorderStyle.Render(strings.Repeat("─", rightWidth+2)) +
		rightBorderStyle.Render("┘")
	rows = append(rows, bottom)

	return strings.Join(rows, "\n")
}

func renderStackedView(cfg splitViewConfig, width, height int) string {
	borderStyle := lipgloss.NewStyle().Foreground(tui.ColorBorder)
	titleStyle := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
	innerWidth := width - 2

	topHeight := height * 35 / 100
	if topHeight < 3 {
		topHeight = 3
	}
	bottomHeight := height - topHeight - 1
	if bottomHeight < 3 {
		bottomHeight = 3
	}

	// Top border
	topTitle := borderStyle.Render("─► ") + titleStyle.Render(cfg.leftTitle) + borderStyle.Render(" ")
	topTitlePad := innerWidth + 2 - lipgloss.Width(topTitle)
	if topTitlePad < 0 {
		topTitlePad = 0
	}
	top := borderStyle.Render("┌") + topTitle + borderStyle.Render(strings.Repeat("─", topTitlePad)) + borderStyle.Render("┐")

	topLines := padLines(cfg.leftContent, innerWidth, topHeight)
	var topRows []string
	for _, l := range topLines {
		topRows = append(topRows, borderStyle.Render("│")+" "+l+" "+borderStyle.Render("│"))
	}

	// Mid divider
	midTitle := borderStyle.Render("─► ") + titleStyle.Render(cfg.rightTitle) + borderStyle.Render(" ")
	midTitlePad := innerWidth + 2 - lipgloss.Width(midTitle)
	if midTitlePad < 0 {
		midTitlePad = 0
	}
	mid := borderStyle.Render("├") + midTitle + borderStyle.Render(strings.Repeat("─", midTitlePad)) + borderStyle.Render("┤")

	bottomLines := padLines(cfg.rightContent, innerWidth, bottomHeight)
	var bottomRows []string
	for _, l := range bottomLines {
		bottomRows = append(bottomRows, borderStyle.Render("│")+" "+l+" "+borderStyle.Render("│"))
	}

	bot := borderStyle.Render("└") + borderStyle.Render(strings.Repeat("─", innerWidth+2)) + borderStyle.Render("┘")

	return top + "\n" + strings.Join(topRows, "\n") + "\n" + mid + "\n" + strings.Join(bottomRows, "\n") + "\n" + bot
}

// padLines splits content into lines and pads/truncates to exact width and height.
// Uses lipgloss for ANSI-aware truncation so styled text doesn't escape the pane.
func padLines(content string, width, height int) []string {
	lines := strings.Split(content, "\n")
	result := make([]string, height)
	truncStyle := lipgloss.NewStyle().MaxWidth(width)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			line := truncStyle.Render(lines[i])
			pad := width - lipgloss.Width(line)
			if pad > 0 {
				line += strings.Repeat(" ", pad)
			}
			result[i] = line
		} else {
			result[i] = strings.Repeat(" ", width)
		}
	}
	return result
}

// ── Menu rendering for left pane ────────────────────────────────────────

type menuItem struct {
	label string
	value string
	desc  string
	isSep bool
}

func renderLeftMenu(items []menuItem, cursor int, width int) string {
	var b strings.Builder

	// Full-row highlight for selected menu item
	selectedStyle := lipgloss.NewStyle().
		Background(tui.MithrilTeal).
		Foreground(lipgloss.Color("#000000")).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(tui.ColorTextSecondary)

	sepStyle := lipgloss.NewStyle().
		Foreground(tui.ColorBorder)

	for i, item := range items {
		if item.isSep {
			b.WriteString(sepStyle.Render(strings.Repeat("─", width)) + "\n")
			continue
		}

		label := fmt.Sprintf(" %-*s", width-1, item.label)
		if i == cursor {
			b.WriteString(selectedStyle.Render(label) + "\n")
		} else {
			b.WriteString(normalStyle.Render(label) + "\n")
		}
	}

	return b.String()
}

// ── Utility ─────────────────────────────────────────────────────────────

func formatNumber(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []string
	for i, c := range reverseString(s) {
		if i > 0 && i%3 == 0 {
			result = append(result, ",")
		}
		result = append(result, string(c))
	}
	// Reverse back
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return strings.Join(result, "")
}

func reverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func renderBarChart(used, total uint64, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}
	pct := float64(used) / float64(total)
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	var color lipgloss.Color
	switch {
	case pct >= 0.9:
		color = tui.ColorError
	case pct >= 0.8:
		color = tui.ColorWarn
	default:
		color = tui.MithrilTeal
	}

	return lipgloss.NewStyle().Foreground(color).Render(bar)
}
