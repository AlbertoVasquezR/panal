package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The UI color palette, kept in one place. Adaptive variants (AdaptiveColor)
// keep it readable on both dark and light terminal backgrounds.
var (
	// Agent colors: kept identical to their mascots' pixel art.
	accent = map[string]lipgloss.Color{
		"claude":   "#D97757",
		"agy":      "#9575CD",
		"codex":    "#69F0AE",
		"opencode": "#FFD54F",
		"cursor":   "#38BDF8",
	}

	// The dashboard's main brand ("◆ Panal").
	cBrand = lipgloss.AdaptiveColor{Light: "#5E35B1", Dark: "#7C4DFF"}

	// Borders, dim text and the empty part of quota bars.
	// The light variant uses darker tones to contrast on white.
	cBorder = lipgloss.AdaptiveColor{Light: "248", Dark: "238"}
	cDim    = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}
	cEmpty  = lipgloss.AdaptiveColor{Light: "250", Dark: "237"}

	// Status colors: green (active), amber (warning/quota), red (error), blue (orchestrating).
	// The light variant uses deeper, more saturated tones so they don't fade on white.
	cGreen = lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#66BB6A"}
	cAmber = lipgloss.AdaptiveColor{Light: "#B26A00", Dark: "#FFB300"}
	cRed   = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#EF5350"}
	cBlue  = lipgloss.AdaptiveColor{Light: "#1565C0", Dark: "#42A5F5"}

	// Row selection in lists (history and compact table).
	cSelBg = lipgloss.AdaptiveColor{Light: "253", Dark: "236"}

	// Keys in the footer (keycaps: " enter ").
	cKeyBg   = lipgloss.AdaptiveColor{Light: "252", Dark: "238"}
	cKeyText = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#FFFFFF"}
)

// withSelBg paints the selected row's background edge to edge. Wrapping it in
// Background is not enough: each cell has its own colors and its "\x1b[0m"
// turns the background off mid-row, so the background is reopened after each reset.
func withSelBg(line string, width int) string {
	if missing := width - lipgloss.Width(line); missing > 0 {
		line += strings.Repeat(" ", missing)
	}
	sample := lipgloss.NewStyle().Bold(true).Background(cSelBg).Render("x")
	opener, _, ok := strings.Cut(sample, "x")
	if !ok || opener == "" {
		return lipgloss.NewStyle().Bold(true).Render(line) // no colors: bold only
	}
	return opener + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+opener) + "\x1b[0m"
}

// cTextOnColor: text on bands and badges, which sit on colored
// backgrounds. A fixed black rather than the palette's "0": each terminal theme
// defines that "black" its own way (sometimes almost the background) and, in
// bold, Windows Terminal swaps it for bright gray, unreadable on color.
var cTextOnColor = lipgloss.Color("#111111")
