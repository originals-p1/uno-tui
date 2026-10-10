package tui

import (
	"image/color"
	"os"

	lip "charm.land/lipgloss/v2"
)

// unoColor maps a card color to an ANSI palette color for lipgloss.
func unoColor(c string) color.Color {
	switch c {
	case "red":
		return lip.Color("196")
	case "yellow":
		return lip.Color("220")
	case "green":
		return lip.Color("34")
	case "blue":
		return lip.Color("33")
	default:
		return lip.Color("245")
	}
}

// colorEnabled reports whether ANSI color should be emitted (NO_COLOR honored).
func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return true
}

// titleStyle renders the header line styled.
func titleStyle(text string) string {
	return lip.NewStyle().Foreground(lip.Color("208")).Bold(true).Render(text)
}

// currentPlayerStyle renders a current-player line styled.
func currentPlayerStyle(text string) string {
	return lip.NewStyle().Foreground(lip.Color("39")).Bold(true).Render(text)
}

// playableStyle renders a playable hand line styled.
func playableStyle(text string) string {
	return lip.NewStyle().Bold(true).Render(text)
}

// successStyle renders a challenge-success banner (green).
func successStyle(text string) string {
	return lip.NewStyle().Foreground(lip.Color("34")).Bold(true).Render(text)
}

// failStyle renders a challenge-failure banner (red).
func failStyle(text string) string {
	return lip.NewStyle().Foreground(lip.Color("196")).Bold(true).Render(text)
}

// unoStyle renders a highly-visible "UNO!" marker: bold bright-yellow text on a
// red background so a player calling UNO stands out at a glance.
func unoStyle(text string) string {
	return lip.NewStyle().
		Bold(true).
		Foreground(lip.Color("220")).
		Background(lip.Color("196")).
		Render(text)
}
