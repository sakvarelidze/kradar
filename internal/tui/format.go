package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Truncate clips text to max display width and appends an ellipsis when clipped.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}

	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > max {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimSpace(string(runes)) + "…"
}
