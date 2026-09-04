package dashboardcmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sonicfromnewyoke/mithril/pkg/tui"
	"github.com/charmbracelet/lipgloss"
)

func (m model) renderRightPane() string {
	if !m.hasConfig {
		return m.renderNoConfig()
	}

	switch m.screen {
	case screenOverview:
		return m.renderOverview()
	case screenConfig:
		return m.renderConfigView()
	case screenEdit:
		return m.renderEditView()
	case screenDoctor:
		return m.renderDoctorView()
	case screenLogs:
		return m.renderLogsView()
	case screenDisk:
		return m.renderDiskView()
	}
	return ""
}

// ── No Config ───────────────────────────────────────────────────────────

func (m model) renderNoConfig() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
	muted := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	text := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary)

	b.WriteString(title.Render("No configuration found") + "\n\n")
	b.WriteString(muted.Render("  Looked for: ") + text.Render(m.configFile) + "\n\n")
	b.WriteString(text.Render("  Select 'Create Config' from the menu to create one,") + "\n")
	b.WriteString(text.Render("  or from the command line:") + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(tui.ColorTextSecondary).Render("    $ mithril setup") + "\n")

	return b.String()
}

// ── Overview ────────────────────────────────────────────────────────────

func (m model) renderOverview() string {
	var b strings.Builder
	pass := lipgloss.NewStyle().Foreground(tui.ColorSuccess)
	fail := lipgloss.NewStyle().Foreground(tui.ColorError)
	warn := lipgloss.NewStyle().Foreground(tui.ColorWarn)
	label := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	value := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary)
	header := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)

	// Health summary
	passed := 0
	total := len(m.checks)
	for _, c := range m.checks {
		if c.status == "pass" {
			passed++
		}
	}

	b.WriteString(header.Render("Health") + "\n\n")
	for _, c := range m.checks {
		icon := pass.Render("✓")
		if c.status == "warn" {
			icon = warn.Render("~")
		} else if c.status == "fail" {
			icon = fail.Render("✗")
		}
		b.WriteString("  " + icon + " " + label.Render(c.name) + "\n")
	}
	b.WriteString("\n")
	b.WriteString("  " + label.Render(fmt.Sprintf("%d/%d checks passed", passed, total)) + "\n")
	b.WriteString("\n")

	// Services — only show when node has state (has been started before)
	if m.state != nil {
		b.WriteString(header.Render("Services") + "\n\n")
		for _, svc := range m.services {
			dot := fail.Render("○")
			status := label.Render("down")
			if svc.up {
				dot = pass.Render("●")
				status = pass.Render("up")
			}
			b.WriteString("  " + dot + " " + value.Render(fmt.Sprintf("%-14s", svc.name)) + label.Render(fmt.Sprintf("%-20s", svc.addr)) + status + "\n")
		}
		b.WriteString("\n")
	}

	// Node state
	if m.state != nil && m.state.LastSlot > 0 {
		b.WriteString(header.Render("Node State") + "\n\n")
		b.WriteString(label.Render("  Slot        ") + value.Render(formatNumber(m.state.LastSlot)) + "\n")
		b.WriteString(label.Render("  Epoch       ") + value.Render(fmt.Sprintf("%d", m.state.LastEpoch)) + "\n")
		if m.state.LastBankhash != "" {
			short := m.state.LastBankhash
			if len(short) > 12 {
				short = short[:12] + "..."
			}
			b.WriteString(label.Render("  Bankhash    ") + value.Render(short) + "\n")
		}
		if m.state.SnapshotSlot > 0 {
			b.WriteString(label.Render("  Snapshot    ") + value.Render(formatNumber(m.state.SnapshotSlot)) + "\n")
		}
		if m.state.LastShutdownReason != "" {
			reason := value.Render(m.state.LastShutdownReason)
			if m.state.LastShutdownAt != "" {
				reason += label.Render("  at ") + value.Render(m.state.LastShutdownAt)
			}
			b.WriteString(label.Render("  Shutdown    ") + reason + "\n")
		}
		if m.state.Stage != "" {
			b.WriteString(label.Render("  Stage       ") + value.Render(m.state.Stage) + "\n")
		}
		if m.state.LastWriterVersion != "" {
			ver := value.Render(m.state.LastWriterVersion)
			if m.state.LastWriterCommit != "" {
				short := m.state.LastWriterCommit
				if len(short) > 8 {
					short = short[:8]
				}
				ver += label.Render(" (") + value.Render(short) + label.Render(")")
			}
			b.WriteString(label.Render("  Writer      ") + ver + "\n")
		}
	}

	// When node hasn't run yet, show next steps instead of empty space
	if m.state == nil && m.cfg != nil {
		cmd := lipgloss.NewStyle().Foreground(tui.ColorTextSecondary)
		b.WriteString("\n")
		b.WriteString(header.Render("Next Steps") + "\n\n")
		b.WriteString(label.Render("  Node has not been started yet.") + "\n\n")
		b.WriteString(label.Render("  1. Review your config       ") + cmd.Render("← Config") + "\n")
		b.WriteString(label.Render("  2. Run health checks        ") + cmd.Render("← Doctor") + "\n")
		b.WriteString(label.Render("  3. Start the node:") + "\n")
		b.WriteString(cmd.Render("     $ mithril run --config "+m.configFile) + "\n")
	}

	return b.String()
}

// ── Config View ─────────────────────────────────────────────────────────

// configSection holds a parsed TOML section for display.
type configSection struct {
	name string
	keys []string
	vals []string
}

func (m model) renderConfigView() string {
	data, err := os.ReadFile(m.configFile)
	if err != nil {
		return "Could not read config: " + m.configFile
	}

	// Parse TOML into sections
	var sections []configSection
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[") {
			sections = append(sections, configSection{name: strings.Trim(trimmed, "[] ")})
			continue
		}
		if len(sections) > 0 {
			if idx := strings.Index(trimmed, "="); idx > 0 {
				k := strings.TrimSpace(trimmed[:idx])
				raw := strings.TrimSpace(trimmed[idx+1:])
				v := stripTomlQuotes(stripInlineComment(raw))
				s := &sections[len(sections)-1]
				s.keys = append(s.keys, k)
				s.vals = append(s.vals, v)
			}
		}
	}

	sectionStyle := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	valStyle := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary)
	hintStyle := lipgloss.NewStyle().Foreground(tui.ColorTextDisabled)

	// Count total lines needed for single-column display
	totalLines := 0
	for _, s := range sections {
		totalLines += 1 + len(s.keys) // header + kvs
	}
	totalLines += len(sections) - 1 // blank lines between sections

	// Estimate available height in right pane
	availHeight := m.height - 18
	if availHeight < 10 {
		availHeight = 10
	}

	// If it fits in one column, render single column
	if totalLines <= availHeight {
		return m.renderConfigSingleColumn(sections, sectionStyle, keyStyle, valStyle, hintStyle)
	}

	// Otherwise split into two columns side by side
	return m.renderConfigTwoColumns(sections, sectionStyle, keyStyle, valStyle, hintStyle)
}

// renderConfigColumn renders sections as lines for a single column.
// Dynamically calculates key padding from the longest key in the column.
func renderConfigColumn(secs []configSection, colWidth int, sectionStyle, keyStyle, valStyle lipgloss.Style) []string {
	// Find longest key for proper alignment
	keyPad := 12
	for _, s := range secs {
		for _, k := range s.keys {
			if len(k)+2 > keyPad { // +2 for indent
				keyPad = len(k) + 2
			}
		}
	}
	// Cap key padding — leave room for values
	maxKeyPad := colWidth * 45 / 100
	if keyPad > maxKeyPad {
		keyPad = maxKeyPad
	}

	maxVal := colWidth - keyPad - 3
	if maxVal < 8 {
		maxVal = 8
	}

	var lines []string
	for i, s := range secs {
		if i > 0 {
			lines = append(lines, "") // breathing room between sections
		}
		lines = append(lines, sectionStyle.Render(s.name))
		for j := range s.keys {
			v := s.vals[j]
			// Mask sensitive values (tokens, secrets, passwords)
			k := strings.ToLower(s.keys[j])
			if strings.Contains(k, "token") || strings.Contains(k, "secret") || strings.Contains(k, "password") {
				if len(v) > 4 {
					v = v[:2] + strings.Repeat("*", len(v)-4) + v[len(v)-2:]
				} else if len(v) > 0 {
					v = "****"
				}
			}
			if len(v) > maxVal {
				v = v[:maxVal-3] + "..."
			}
			lines = append(lines, "  "+keyStyle.Render(fmt.Sprintf("%-*s ", keyPad, s.keys[j]))+valStyle.Render(v))
		}
	}
	return lines
}

func (m model) renderConfigSingleColumn(sections []configSection, sectionStyle, keyStyle, valStyle, hintStyle lipgloss.Style) string {
	rightPaneWidth := (m.width - 3) * 78 / 100
	lines := renderConfigColumn(sections, rightPaneWidth, sectionStyle, keyStyle, valStyle)

	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n")
	ks := lipgloss.NewStyle().Foreground(tui.MithrilTeal)
	b.WriteString("  " + ks.Render("e") + hintStyle.Render(" edit") + "  " + ks.Render("r") + hintStyle.Render(" refresh") + "\n")
	return b.String()
}

func (m model) renderConfigTwoColumns(sections []configSection, sectionStyle, keyStyle, valStyle, hintStyle lipgloss.Style) string {
	// Split sections into two groups by total line count (balanced)
	totalLines := 0
	for _, s := range sections {
		totalLines += 2 + len(s.keys) // header + kvs + spacing
	}
	midpoint := totalLines / 2

	lineCount := 0
	splitIdx := len(sections)
	for i, s := range sections {
		sLines := 2 + len(s.keys)
		if lineCount+sLines > midpoint && lineCount > 0 {
			splitIdx = i
			break
		}
		lineCount += sLines
	}

	// Column sizing: right pane width → two columns with clean gap
	rightPaneWidth := (m.width - 3) * 78 / 100
	colGap := 4
	colWidth := (rightPaneWidth - colGap) / 2

	leftLines := renderConfigColumn(sections[:splitIdx], colWidth, sectionStyle, keyStyle, valStyle)
	rightLines := renderConfigColumn(sections[splitIdx:], colWidth, sectionStyle, keyStyle, valStyle)

	maxRows := len(leftLines)
	if len(rightLines) > maxRows {
		maxRows = len(rightLines)
	}

	gap := strings.Repeat(" ", colGap)
	truncStyle := lipgloss.NewStyle().MaxWidth(colWidth)

	var b strings.Builder
	for i := 0; i < maxRows; i++ {
		left := ""
		right := ""
		if i < len(leftLines) {
			left = truncStyle.Render(leftLines[i])
		}
		if i < len(rightLines) {
			right = rightLines[i]
		}
		leftPad := colWidth - lipgloss.Width(left)
		if leftPad > 0 {
			left += strings.Repeat(" ", leftPad)
		}
		b.WriteString(left + gap + right + "\n")
	}

	b.WriteString("\n")
	ks := lipgloss.NewStyle().Foreground(tui.MithrilTeal)
	b.WriteString("  " + ks.Render("e") + hintStyle.Render(" edit") + "  " + ks.Render("r") + hintStyle.Render(" refresh") + "\n")
	return b.String()
}

// stripInlineComment removes the inline comment from a TOML value.
// e.g., `"auto"   # some comment` → `"auto"`
func stripInlineComment(v string) string {
	// Don't strip # inside quoted strings
	inQuote := false
	for i, c := range v {
		if c == '"' {
			inQuote = !inQuote
		}
		if c == '#' && !inQuote {
			return strings.TrimSpace(v[:i])
		}
	}
	return v
}

// stripTomlQuotes removes TOML string quotes and array brackets for display.
func stripTomlQuotes(v string) string {
	// Array of strings: ["value"] or ["v1", "v2"]
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		inner := v[1 : len(v)-1]
		// Split by comma and clean each element
		parts := strings.Split(inner, ",")
		var clean []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			p = strings.Trim(p, "\"")
			if p != "" {
				clean = append(clean, p)
			}
		}
		return strings.Join(clean, ", ")
	}
	// Simple quoted string
	if strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") && len(v) >= 2 {
		return v[1 : len(v)-1]
	}
	return v
}

// ── Edit View (inline config editing) ───────────────────────────────

func (m model) renderEditView() string {
	if m.cfg == nil {
		return "No config loaded."
	}

	// When actively editing a field, show a focused full-pane view
	if m.editMode != editNone && m.editIdx < len(m.editFields) {
		return m.renderEditFocused()
	}

	// Otherwise show the compact field list
	return m.renderEditList()
}

// renderEditList shows all fields in a compact scrollable list.
func (m model) renderEditList() string {
	label := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	value := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary)
	active := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
	hint := lipgloss.NewStyle().Foreground(tui.ColorTextDisabled)

	var lines []string
	selectedStart := 0

	for i, f := range m.editFields {
		if f.isSep {
			lines = append(lines, "")
			continue
		}

		isSelected := i == m.editIdx
		val := m.getFieldValue(f)

		// Auto-detect indicator for unset txpar
		displayVal := val
		isAuto := val == "" && f.section == "tuning" && f.key == "txpar"
		if isAuto {
			displayVal = "not set (sequential)"
		}
		if len(displayVal) > 35 {
			displayVal = displayVal[:32] + "..."
		}

		if isSelected {
			selectedStart = len(lines)
			if isAuto {
				lines = append(lines, active.Render(fmt.Sprintf("  ▸ %-22s", f.label))+hint.Render(displayVal))
			} else {
				lines = append(lines, active.Render(fmt.Sprintf("  ▸ %-22s", f.label))+value.Render(displayVal))
			}
		} else {
			if isAuto {
				lines = append(lines, label.Render(fmt.Sprintf("    %-22s", f.label))+hint.Render(displayVal))
			} else {
				lines = append(lines, label.Render(fmt.Sprintf("    %-22s", f.label))+value.Render(displayVal))
			}
		}
	}

	// Auto-scroll: show a window centered on the selected field
	maxVisible := m.height - 20
	if maxVisible < 10 {
		maxVisible = 10
	}
	if len(lines) > maxVisible {
		start := selectedStart - maxVisible/3
		if start < 0 {
			start = 0
		}
		end := start + maxVisible
		if end > len(lines) {
			end = len(lines)
			start = end - maxVisible
			if start < 0 {
				start = 0
			}
		}
		lines = lines[start:end]
	}

	return strings.Join(lines, "\n") + "\n"
}

// renderEditFocused shows a single field's edit UI in the full right pane.
func (m model) renderEditFocused() string {
	f := m.editFields[m.editIdx]
	titleStyle := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	valueStyle := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary)
	activeStyle := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(tui.ColorTextDisabled)
	errorStyle := lipgloss.NewStyle().Foreground(tui.ColorError)
	keyStyle := lipgloss.NewStyle().Foreground(tui.MithrilTeal)

	var b strings.Builder

	// ── Title block ──
	b.WriteString("\n")
	b.WriteString("  " + titleStyle.Render(f.label) + "\n")
	b.WriteString("  " + subtitleStyle.Render(f.section+"."+f.key) + "\n")
	b.WriteString("\n")

	if m.editMode == editMenu {
		// ── Menu options ──
		maxLabel := 0
		for _, opt := range m.editOptions {
			if len(opt.label) > maxLabel {
				maxLabel = len(opt.label)
			}
		}

		for j, opt := range m.editOptions {
			padded := fmt.Sprintf("%-*s", maxLabel+2, opt.label)
			if j == m.editOptCursor {
				line := "  " + activeStyle.Render("▸ "+padded)
				if opt.desc != "" {
					line += subtitleStyle.Render(opt.desc)
				}
				b.WriteString(line + "\n")
			} else {
				line := "    " + valueStyle.Render(padded)
				if opt.desc != "" {
					line += subtitleStyle.Render(opt.desc)
				}
				b.WriteString(line + "\n")
			}
		}

		b.WriteString("\n")
		if m.editErr != "" {
			b.WriteString("  " + errorStyle.Render("✗ "+m.editErr) + "\n\n")
		}

		// ── Hints ──
		b.WriteString("  " + keyStyle.Render("↑↓") + hintStyle.Render(" select") +
			"    " + keyStyle.Render("⏎") + hintStyle.Render(" confirm") +
			"    " + keyStyle.Render("esc") + hintStyle.Render(" cancel") + "\n")

	} else if m.editMode == editText {
		// ── Current value ──
		currentVal := m.getFieldValue(f)
		isAuto := currentVal == "" && f.section == "tuning" && f.key == "txpar"
		if isAuto {
			b.WriteString("  " + subtitleStyle.Render("Current: ") + hintStyle.Render("not set (sequential)") + "\n")
		} else if currentVal != "" {
			b.WriteString("  " + subtitleStyle.Render("Current: ") + valueStyle.Render(currentVal) + "\n")
		}
		b.WriteString("\n")

		// ── Input field ──
		text := m.editValue
		if m.editCursor >= 0 && m.editCursor <= len(text) {
			before := text[:m.editCursor]
			after := text[m.editCursor:]
			cur := lipgloss.NewStyle().Background(tui.MithrilTeal).Foreground(lipgloss.Color("#000000")).Render(" ")
			if m.editCursor < len(text) {
				cur = lipgloss.NewStyle().Background(tui.MithrilTeal).Foreground(lipgloss.Color("#000000")).Render(string(after[0]))
				after = after[1:]
			}
			text = before + cur + after
		}

		prompt := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Render("  ❯ ")
		b.WriteString(prompt + text + "\n")

		underLen := len(m.editValue) + 4
		if underLen < 30 {
			underLen = 30
		}
		b.WriteString("  " + lipgloss.NewStyle().Foreground(tui.ColorBorder).Render(strings.Repeat("─", underLen)) + "\n")

		// ── Contextual hint ──
		if isAuto && m.editValue == "" {
			b.WriteString("\n  " + hintStyle.Render("Leave empty for sequential mode (0), or set worker count") + "\n")
		}

		b.WriteString("\n")
		if m.editErr != "" {
			b.WriteString("  " + errorStyle.Render("✗ "+m.editErr) + "\n\n")
		}

		// ── Hints ──
		b.WriteString("  " + keyStyle.Render("⏎") + hintStyle.Render(" save") +
			"    " + keyStyle.Render("esc") + hintStyle.Render(" cancel") +
			"    " + keyStyle.Render("←→") + hintStyle.Render(" cursor") + "\n")
	}

	return b.String()
}

// ── Doctor View ─────────────────────────────────────────────────────────

func (m model) renderDoctorView() string {
	var b strings.Builder
	pass := lipgloss.NewStyle().Foreground(tui.ColorSuccess)
	fail := lipgloss.NewStyle().Foreground(tui.ColorError)
	warn := lipgloss.NewStyle().Foreground(tui.ColorWarn)
	label := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)

	passed := 0
	total := len(m.checks)

	for _, c := range m.checks {
		icon := pass.Render("✓")
		if c.status == "warn" {
			icon = warn.Render("~")
		} else if c.status == "fail" {
			icon = fail.Render("✗")
		}
		b.WriteString("  " + icon + "  " + lipgloss.NewStyle().Foreground(tui.ColorTextPrimary).Render(fmt.Sprintf("%-20s", c.name)) + label.Render(c.msg) + "\n")
		if c.status == "pass" {
			passed++
		}
	}

	b.WriteString("\n")
	summary := fmt.Sprintf("  %d/%d checks passed", passed, total)
	if passed == total {
		b.WriteString(pass.Render(summary+" — ready to run!") + "\n")
	} else {
		b.WriteString(warn.Render(summary) + "\n")
	}

	b.WriteString("\n")
	k := lipgloss.NewStyle().Foreground(tui.MithrilTeal)
	hint := lipgloss.NewStyle().Foreground(tui.ColorTextDisabled)
	b.WriteString("  " + k.Render("r") + hint.Render(" re-run checks") + "\n")

	return b.String()
}

// ── Logs View ───────────────────────────────────────────────────────────

func (m model) renderLogsView() string {
	if len(m.mithrilLines) == 0 && len(m.lbLines) == 0 {
		mutedStyle := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
		cmdStyle := lipgloss.NewStyle().Foreground(tui.ColorTextSecondary)
		return mutedStyle.Render("  Logs will appear here after starting the node.") + "\n\n" +
			cmdStyle.Render("    $ mithril run --config "+m.configFile) + "\n"
	}

	titleStyle := lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	hintStyle := lipgloss.NewStyle().Foreground(tui.ColorTextDisabled)

	// Calculate column widths
	rightPaneWidth := (m.width - 3) * 78 / 100
	colGap := 3
	colWidth := (rightPaneWidth - colGap) / 2

	// Wrap long lines within column width so full messages are readable
	mLines := wrapLogLines(m.mithrilLines, colWidth)
	lLines := wrapLogLines(m.lbLines, colWidth)
	if m.logFocused && m.logScroll > 0 {
		if m.logPane == logPaneMithril {
			if m.logScroll < len(mLines) {
				mLines = mLines[m.logScroll:]
			} else {
				mLines = nil
			}
		} else {
			if m.logScroll < len(lLines) {
				lLines = lLines[m.logScroll:]
			} else {
				lLines = nil
			}
		}
	}

	// Render log lines with color coding
	colorLine := func(line string) string {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return ""
		}
		switch {
		case strings.Contains(line, " WARN ") || strings.HasPrefix(trimmed, "WARN"):
			return lipgloss.NewStyle().Foreground(tui.ColorWarn).Render(line)
		case strings.Contains(line, " ERROR ") || strings.HasPrefix(trimmed, "ERROR") || strings.Contains(line, "FATAL"):
			return lipgloss.NewStyle().Foreground(tui.ColorError).Render(line)
		default:
			return mutedStyle.Render(line)
		}
	}

	// Build side-by-side output with divider
	maxRows := len(mLines)
	if len(lLines) > maxRows {
		maxRows = len(lLines)
	}
	availHeight := m.height - 22
	if availHeight < 5 {
		availHeight = 5
	}
	if maxRows > availHeight {
		maxRows = availHeight
	}

	divStyle := lipgloss.NewStyle().Foreground(tui.ColorBorder)
	div := divStyle.Render("│")
	truncStyle := lipgloss.NewStyle().MaxWidth(colWidth)

	// Headers — underline the focused pane title
	var b strings.Builder
	var mTitle, lTitle string
	mTitleStyle := titleStyle
	lTitleStyle := titleStyle
	if m.logFocused && m.logPane == logPaneMithril {
		mTitleStyle = lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true).Underline(true)
	} else if m.logFocused && m.logPane == logPaneLightbringer {
		lTitleStyle = lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true).Underline(true)
	}
	mTitle = mTitleStyle.Render("mithril")
	lTitle = lTitleStyle.Render("lightbringer")

	b.WriteString(mTitle)
	leftPad := colWidth - lipgloss.Width(mTitle)
	if leftPad > 0 {
		b.WriteString(strings.Repeat(" ", leftPad))
	}
	b.WriteString(" " + div + " " + lTitle + "\n")

	// Divider line under headers
	b.WriteString(divStyle.Render(strings.Repeat("─", colWidth)) + " " + div + " " + divStyle.Render(strings.Repeat("─", colWidth)) + "\n")

	// Active pane line highlight style
	activeLineStyle := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary)

	for i := 0; i < maxRows; i++ {
		left := ""
		right := ""

		if i < len(mLines) {
			if m.logFocused && m.logPane == logPaneMithril {
				// Active pane: brighter text
				left = truncStyle.Render(activeLineStyle.Render(mLines[i]))
			} else {
				left = truncStyle.Render(colorLine(mLines[i]))
			}
		}
		if i < len(lLines) {
			if m.logFocused && m.logPane == logPaneLightbringer {
				right = truncStyle.Render(activeLineStyle.Render(lLines[i]))
			} else {
				right = truncStyle.Render(colorLine(lLines[i]))
			}
		}

		lPad := colWidth - lipgloss.Width(left)
		if lPad > 0 {
			left += strings.Repeat(" ", lPad)
		}
		b.WriteString(left + " " + div + " " + right + "\n")
	}

	if m.logFocused {
		b.WriteString("\n" + hintStyle.Render("  ↑↓ scroll  ←→ switch  esc back") + "\n")
	}

	return b.String()
}

// wrapLogLines wraps each line to fit within the given width.
// Continuation lines are indented with 2 spaces for readability.
func wrapLogLines(lines []string, width int) []string {
	if width < 10 {
		width = 10
	}
	var result []string
	for _, line := range lines {
		if len(line) <= width {
			result = append(result, line)
			continue
		}
		// First chunk at full width, continuations indented
		result = append(result, line[:width])
		remaining := line[width:]
		contWidth := width - 2 // indent continuation
		for len(remaining) > 0 {
			if len(remaining) <= contWidth {
				result = append(result, "  "+remaining)
				break
			}
			result = append(result, "  "+remaining[:contWidth])
			remaining = remaining[contWidth:]
		}
	}
	return result
}

// ── Disk View ───────────────────────────────────────────────────────────

func (m model) renderDiskView() string {
	if len(m.disks) == 0 {
		muted := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
		if !m.disksLoaded {
			return muted.Render("  Loading disk usage...") + "\n"
		}
		if m.cfg != nil && m.cfg.accountsPath != "" {
			return muted.Render("  No disk data available.") + "\n\n" +
				muted.Render("  Storage paths may not exist on this machine yet.") + "\n" +
				muted.Render("  Disk usage will appear once the node has been started.") + "\n"
		}
		return muted.Render("  Disk usage will appear once storage paths are configured.") + "\n"
	}

	var b strings.Builder
	label := lipgloss.NewStyle().Foreground(tui.ColorTextMuted)
	value := lipgloss.NewStyle().Foreground(tui.ColorTextPrimary)

	for _, d := range m.disks {
		b.WriteString(lipgloss.NewStyle().Foreground(tui.MithrilTeal).Bold(true).Render(d.label) + "\n")
		b.WriteString(label.Render("  "+d.path) + "\n")
		b.WriteString("  " + value.Render(fmt.Sprintf("%dG / %dG  %d%%", d.used, d.total, d.pct)) + "\n")
		b.WriteString("  " + renderBarChart(d.used, d.total, 30) + "\n")
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(tui.ColorTextDisabled).Render("  Normal: <80%  Warning: 80-90%  Critical: >90%") + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(tui.ColorTextDisabled).Render("  Press 'r' to refresh") + "\n")

	return b.String()
}

// Setup is launched directly as an embedded child TUI via selectCurrent().
