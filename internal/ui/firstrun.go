package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/AlbertoVasquezR/panal/internal/mascots"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

// --------------------------------------------------------------- unseen --

// noteUnseen: a run that ends (well or badly) stays "unseen" until you open
// its detail; if the agent starts working again, the mark is no longer needed.
func (m *Model) noteUnseen(agent string, r mascots.Mode) {
	if m.unseen == nil {
		m.unseen = map[string]bool{}
	}
	switch r {
	case mascots.Celebrate, mascots.Scared:
		m.unseen[agent] = true
	case mascots.WakeUp:
		delete(m.unseen, agent)
	}
}

// syncUnseen copies the mark to the rows, which is what gets drawn.
func (m *Model) syncUnseen() {
	for i := range m.rows {
		m.rows[i].Unseen = m.unseen[m.rows[i].Agent]
	}
}

// markSelSeen clears the selected agent's mark.
func (m *Model) markSelSeen() {
	sel, ok := m.selected()
	if !ok {
		return
	}
	delete(m.unseen, m.rows[sel].Agent)
	m.rows[sel].Unseen = false
}

// ------------------------------------------------------------ first run --

type sourceSeen struct {
	readers.Source
	exists bool
}

// isFirstRun: no reader found anything and there are no runs. This is what
// someone who just installed the dashboard sees.
func (m Model) isFirstRun() bool {
	if len(m.rows) == 0 || len(m.runs) > 0 {
		return false
	}
	for _, f := range m.rows {
		if f.Status != state.NoData {
			return false
		}
	}
	return true
}

// checkSources checks on disk which sources exist. It runs on refresh (not
// while drawing) and only during the first run.
func (m *Model) checkSources() {
	if !m.isFirstRun() {
		m.sources = nil
		return
	}
	m.sources = m.sources[:0]
	for _, f := range readers.Sources(m.readers) {
		_, err := os.Stat(f.Path)
		m.sources = append(m.sources, sourceSeen{f, err == nil})
	}
}

// welcomeView: what the dashboard is, which sources it found and which are
// missing, and what to do to make the cards show up.
func (m Model) welcomeView(width int) string {
	dimS := lipgloss.NewStyle().Foreground(cDim)
	bold := lipgloss.NewStyle().Bold(true)
	var b strings.Builder
	// Plain text is truncated first and colored afterwards: truncating colored
	// text can split a color code and stain what follows.
	line := func(st lipgloss.Style, s string) { b.WriteString(st.Render(truncate(s, width)) + "\n") }
	normal := lipgloss.NewStyle()

	if c := corralFor(m.rows, m.animation(), -1, m.now, width); c != "" {
		b.WriteString(c + "\n\n")
	}
	line(bold, "  Hi. Nothing to show yet.")
	line(dimS, "  Panal shows what your agents are doing while Claude Code orchestrates them. This is what it looks for:")
	line(normal, "")
	whatWidth := 0
	for _, f := range m.sources {
		whatWidth = max(whatWidth, lipgloss.Width(f.What))
	}
	for _, f := range m.sources {
		mark := lipgloss.NewStyle().Foreground(cGreen).Render("✔")
		track := ""
		if !f.exists {
			mark = lipgloss.NewStyle().Foreground(cAmber).Render("○")
			track = "  missing · " + f.Env
		}
		// The path (and the hint) are cut so the line fits; if even that
		// doesn't fit, only what it is.
		what := truncate(f.What, max(width-4, 1))
		rest := width - 4 - (whatWidth + 2)
		if lipgloss.Width(track) > rest-8 {
			track = ""
		}
		path := ""
		if w := rest - lipgloss.Width(track); w >= 8 {
			path = dimS.Render(truncateMiddle(f.Path, w) + track)
			what = cell(f.What, whatWidth+2, normal)
		}
		b.WriteString("  " + mark + " " + what + path + "\n")
	}
	line(normal, "")
	line(bold, "  Getting started")
	line(normal, "  1. Start a run with panal delegate: each one leaves a JSON file in \"delegated runs\".")
	line(normal, "  2. To see Claude Code, save its status line there (see docs/getting-started.md).")
	line(normal, "  3. If your files live somewhere else, point the variables above to them.")
	line(normal, "")
	line(dimS, "  The dashboard updates by itself: as soon as something shows up, the cards appear.")
	return b.String()
}
