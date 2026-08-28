package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// truncate shortens a possibly styled line to width cells, adding an ellipsis
// when it had to cut. It is ANSI-aware, so styling is never sliced in half.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// wrap reflows plain text to the given width, preserving blank-line paragraph
// breaks. Used for prose fields (a command's purpose, an exercise prompt).
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out []string
	for _, para := range strings.Split(strings.TrimSpace(s), "\n\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if lipgloss.Width(line)+1+lipgloss.Width(w) > width {
				out = append(out, line)
				line = w
				continue
			}
			line += " " + w
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// pad right-pads a line to width cells.
func pad(s string, width int) string {
	if n := width - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// scrollTo returns an updated viewport offset that keeps cursor visible within
// a window of the given height.
func scrollTo(cursor, offset, height int) int {
	if height < 1 {
		return 0
	}
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+height {
		return cursor - height + 1
	}
	return offset
}

// verticalRule renders a full-height single-column divider.
func verticalRule(height int) string {
	if height < 1 {
		height = 1
	}
	return strings.TrimSuffix(strings.Repeat(" │\n", height), "\n")
}

// indent shifts every line of a block right by n spaces.
func indent(s string, n int) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

// plural formats a count with the right noun, e.g. "1 command" / "3 commands".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// clip drops any lines past height, so a pane cannot push the footer off the
// bottom of the screen.
func clip(s string, height int) string {
	if height < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}
