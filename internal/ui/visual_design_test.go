package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/AlbertoVasquezR/panal/internal/history"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

// TestBadgesCheckTextAndIcon: each status has its icon and text inside the
// badge, dark text on a background (or dim text with no background at rest).
func TestBadgesCheckTextAndIcon(t *testing.T) {
	cases := []struct {
		stat     state.Status
		wantIcon string
		noBg     bool
	}{
		{state.NoData, "○", true},
		{state.Idle, "○", true},
		{state.Working, "●", false},
		{state.Stuck, "✖", false},
		{state.OutOfQuota, "◐", false},
		{state.NoPermission, "⊘", false},
		{state.Done, "✔", false},
		{state.Failed, "✖", false},
		{state.Orchestrating, "●", false},
	}

	for _, c := range cases {
		t.Run(c.stat.String(), func(t *testing.T) {
			badge := statusBadge(c.stat)
			plain := reANSI.ReplaceAllString(badge, "")

			// The icon must be inside the badge
			if !strings.Contains(plain, c.wantIcon) {
				t.Errorf("status %s: badge %q lacks the icon %q", c.stat, plain, c.wantIcon)
			}
			// The status text must be inside the badge
			if !strings.Contains(plain, c.stat.String()) {
				t.Errorf("status %s: badge %q lacks the text %q", c.stat, plain, c.stat.String())
			}

			// At rest (idle / no data) there is no background
			if c.noBg {
				// It must have no ANSI background code (\x1b[48;... or \x1b[4...m)
				if strings.Contains(badge, "\x1b[48;") || strings.Contains(badge, "\x1b[40m") || strings.Contains(badge, "\x1b[41m") {
					t.Errorf("status %s should have no background, got: %q", c.stat, badge)
				}
			} else {
				// It must have padding (" ● working ")
				if !strings.HasPrefix(plain, " ") || !strings.HasSuffix(plain, " ") {
					t.Errorf("status %s should have badge padding \" ... \", got: %q", c.stat, plain)
				}
			}
		})
	}
}

// TestTitleBandExactWidth: the title band is exactly the given width, both
// when the badge fits on the right and when it has to go below.
func TestTitleBandExactWidth(t *testing.T) {
	widths := []int{24, 28, 32, 36, 40, 50}
	agents := []string{"claude", "agy", "codex", "opencode", "cursor"}
	statuses := []state.Status{
		state.Working,
		state.Orchestrating,
		state.OutOfQuota,
		state.Idle,
		state.Failed,
	}

	for _, width := range widths {
		for _, ag := range agents {
			for _, stat := range statuses {
				lines := cardBand(ag, stat, width, false)
				if len(lines) == 0 {
					t.Fatalf("cardBand(%s, %s, %d) returned 0 lines", ag, stat, width)
				}
				w := lipgloss.Width(lines[0])
				if w != width {
					t.Errorf("cardBand(%s, %s, %d): line 1 width = %d, want = %d",
						ag, stat, width, w, width)
				}
				plain := reANSI.ReplaceAllString(lines[0], "")
				if !strings.Contains(plain, strings.ToUpper(ag)) {
					t.Errorf("cardBand(%s, %s, %d): the uppercase name is missing: %q",
						ag, stat, width, plain)
				}
			}
		}
	}

	// Case where it doesn't fit and has to go below
	t.Run("badge_below_if_it_does_not_fit", func(t *testing.T) {
		narrowWidth := 14
		lines := cardBand("opencode", state.Orchestrating, narrowWidth, false)
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines when the badge doesn't fit, got %d", len(lines))
		}
		if w := lipgloss.Width(lines[0]); w != narrowWidth {
			t.Errorf("first line width = %d, want %d", w, narrowWidth)
		}
		plain2 := reANSI.ReplaceAllString(lines[1], "")
		if !strings.Contains(plain2, state.Orchestrating.String()) {
			t.Errorf("the second line should contain the badge: %q", plain2)
		}
	})
}

// TestSelectionWithBackground: in the history and the compact table, the
// selected row uses an adaptive background, without the "▌" mark.
func TestSelectionWithBackground(t *testing.T) {
	// 1. History (runRow)
	c := history.Run{
		Stamp:    "20260926-120000-claude",
		Agent:    "claude",
		Model:    "claude-3-7-sonnet",
		Status:   runDone,
		Dir:      "C:\\Codigo\\wt\\tui-bonita",
		Task:     "Refactor the visual UI",
		Start:    time.Now().Add(-30 * time.Minute),
		End:      time.Now().Add(-10 * time.Minute),
		Tokens:   5000,
		Cost:     "$0.15",
		ReadOnly: false,
	}
	w := []int{6, 10, 16, 14, 8, 8, 8, 30}
	width := 110

	selRow := runRow(c, w, width, true, false)
	unselRow := runRow(c, w, width, false, false)

	plainSel := reANSI.ReplaceAllString(selRow, "")
	plainUnsel := reANSI.ReplaceAllString(unselRow, "")

	if strings.Contains(plainSel, "▌") {
		t.Errorf("a selected runRow should not contain \"▌\": %q", plainSel)
	}
	if strings.Contains(plainUnsel, "▌") {
		t.Errorf("an unselected runRow should not contain \"▌\": %q", plainUnsel)
	}
	if wRow := lipgloss.Width(selRow); wRow != width {
		t.Errorf("a selected runRow should be %d wide, got %d", width, wRow)
	}

	// 2. Compact table
	rows := []state.Row{
		{Agent: "claude", Status: state.Orchestrating, Model: "Sonnet"},
		{Agent: "agy", Status: state.Working, Model: "Flash"},
	}
	now := time.Now()
	tableOutput := compactTable(rows, 0, width, now)
	tableLines := strings.Split(strings.TrimRight(tableOutput, "\n"), "\n")

	// Line 1 is the titles, line 2 claude (selected), line 3 agy
	if len(tableLines) < 3 {
		t.Fatalf("compactTable returned fewer than 3 lines:\n%s", tableOutput)
	}
	row1 := tableLines[1]
	plainRow1 := reANSI.ReplaceAllString(row1, "")
	if strings.Contains(plainRow1, "▌") {
		t.Errorf("the selected compactTable row should not contain \"▌\": %q", plainRow1)
	}
	if wTable := lipgloss.Width(row1); wTable != width {
		t.Errorf("compactTable selected row width = %d, want = %d", wTable, width)
	}
}

// TestDetailSeparators: inside a DetailPanel each section has a thin dimmed
// separator with its title ("── actual changes ─────────") as wide as the panel.
func TestDetailSeparators(t *testing.T) {
	width := 60
	inner := width - 4
	pairs := [][2]string{
		{"dir", "C:\\project"},
		{"model", "gemini-2.5-pro"},
	}
	secs := []Section{
		{
			Title: "actual changes",
			Lines: []rowLine{
				{text: "✔ file.go  +10 −2", color: cGreen},
			},
		},
	}

	panel := DetailPanel("DETAIL TEST", agentColor("agy"), pairs, secs, width)
	plain := reANSI.ReplaceAllString(panel, "")

	// It must contain the thin separator with the title
	if !strings.Contains(plain, "── actual changes ─") {
		t.Errorf("the detail panel should contain the thin separator \"── actual changes ─\": %q", plain)
	}

	// Separator lines must span the inner width
	lines := detailPanelLines("DETAIL TEST", agentColor("agy"), pairs, secs, width)
	found := false
	for _, l := range lines {
		cleanL := reANSI.ReplaceAllString(l, "")
		if strings.HasPrefix(cleanL, "── actual changes") {
			found = true
			if w := lipgloss.Width(l); w != inner {
				t.Errorf("the section separator is %d wide, want %d (inner width)", w, inner)
			}
			break
		}
	}
	if !found {
		t.Errorf("no section separator line found in detailPanelLines")
	}
}

// TestHistoryCompactHeight24: below a height of 30 the two summary panels
// shrink to one line ("today: ...") so more runs fit.
func TestHistoryCompactHeight24(t *testing.T) {
	fixedTime := time.Date(2026, 9, 26, 15, 4, 5, 0, time.Local)
	m := Model{
		now:       fixedTime,
		width:     80,
		height:    24, // below 30
		inHistory: true,
		runs: []history.Run{
			{
				Stamp:  "20260926-140000-agy",
				Agent:  "agy",
				Model:  "gemini-pro",
				Status: runDone,
				Start:  fixedTime.Add(-1 * time.Hour),
				End:    fixedTime.Add(-30 * time.Minute),
				Task:   "Try a short view",
			},
		},
	}

	parts := m.historyPartsOf()

	// At height 24, p.summary must not contain the big panels (TODAY · ... in boxes)
	if strings.Contains(parts.summary, "BY AGENT · today") {
		t.Errorf("at height 24 the big summary panels should not show")
	}
	if strings.Contains(parts.summary, "╭") || strings.Contains(parts.summary, "┏") {
		t.Errorf("at height 24 the summary should have no box borders")
	}

	// At height 40 the big panels show
	m.height = 40
	parts40 := m.historyPartsOf()
	if !strings.Contains(parts40.summary, "BY AGENT · today") {
		t.Errorf("at height 40 the per-agent summary panels should show")
	}
}

// The selected row's background must stay on after each cell's color reset,
// all the way to the end of the line.
func TestSelectionBackgroundEdgeToEdge(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	cells := lipgloss.NewStyle().Foreground(cRed).Render("13:39") + " " + lipgloss.NewStyle().Foreground(cGreen).Render("agy")
	l := withSelBg(cells, 20)
	opener, _, _ := strings.Cut(lipgloss.NewStyle().Bold(true).Background(cSelBg).Render("x"), "x")
	if strings.Count(l, opener) < 3 {
		t.Fatalf("the background is not reopened after each cell: %q", l)
	}
	if w := lipgloss.Width(l); w != 20 {
		t.Fatalf("width %d, want 20", w)
	}
}
