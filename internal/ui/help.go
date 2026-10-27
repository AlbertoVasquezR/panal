package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// helpParts: the help screen's header and footer. The view and the scroll keys
// take the height from here, so both count the same way.
func (m Model) helpParts() (head, footer string) {
	width := max(m.width, 40)
	now := m.now
	if now.IsZero() {
		now = nowFn()
	}
	head = Header(m.rows, now, width, "help") + "\n" + m.alertBand(width) + "\n"
	footer = Footer(width, "↑↓", "scroll", "esc", "close", "?", "close", "q", "close")
	return head, footer
}

// helpHeight: content lines that fit (minus the title and its blank line).
func (m Model) helpHeight() int {
	head, footer := m.helpParts()
	return max(3, m.screenHeight()-lipgloss.Height(head)-lipgloss.Height(footer)-2)
}

func (m Model) helpLines(width int) []string {
	dimS := lipgloss.NewStyle().Foreground(cDim)
	bold := lipgloss.NewStyle().Bold(true)
	// Sections styled like the other dashboard titles: uppercase, one space in.
	sectionTitle := func(t string) string {
		return " " + bold.Foreground(cBrand).Render(strings.ToUpper(t))
	}

	// "label : explanation" lines are aligned in two columns so the
	// explanation always starts at the same place.
	const leftCol = 26
	var ls []string
	add := func(s string) {
		if left, right, ok := strings.Cut(s, " : "); ok {
			s = left + strings.Repeat(" ", max(1, leftCol-lipgloss.Width(left))) + dimS.Render(right)
		}
		ls = append(ls, truncate(s, width))
	}

	// 1. Keys
	add(sectionTitle("Keys"))
	add("")
	add("  " + bold.Render("Dashboard") + dimS.Render(" (cards and compact table)"))
	add("    ← → / ↑ ↓  move the agent selection")
	add("    enter/tab  detail of the selected agent")
	add("    1 2 3 4    switch view (Dashboard, History, Report, Timeline)")
	add("    h / i      open History or Report")
	add("    l          full log of the last run")
	add("    d          actual diff of the last run")
	add("    c / w      open its dir in VS Code / in a new terminal")
	add("    t          toggle between cards and compact table")
	add("    m          show or hide mascots")
	add("    p          pet the selected agent's mascot")
	add("    r          ask codex, agy, claude and opencode for their quota now (cursor has none)")
	add("    ?          open this help")
	add("    q          quit")
	add("")
	add("  " + bold.Render("Agent detail"))
	add("    ← →        switch agent")
	add("    ↑ ↓ / j k  scroll the content")
	add("    PgUp/PgDn  page down or up")
	add("    g/G / Home go to the top or bottom")
	add("    l          full log of the last run")
	add("    d          actual diff of the last run")
	add("    esc/tab    back to the dashboard (or enter)")
	add("")
	add("  " + bold.Render("History"))
	add("    ↑ ↓ / j k  move the run selection")
	add("    PgUp/PgDn  page down or up")
	add("    g/G / Home go to the first or last run")
	add("    enter/tab  detail of the selected run")
	add("    /          search text in task, model or dir")
	add("    f          filter by agent (all → claude → agy → codex → opencode → cursor)")
	add("    e          filter by result (failures, quota, done, running)")
	add("    1 2 3 4    switch view")
	add("    l          full log of the run")
	add("    d          actual diff of the run")
	add("    c / w      open its dir in VS Code / in a new terminal")
	add("    + / -      rate the run good or bad (again: clear); the router learns from it")
	add("    esc / h    back to the dashboard")
	add("    ?          open this help")
	add("    q          quit")
	add("")
	add("  " + bold.Render("Run detail"))
	add("    ← →        go to the previous or next run")
	add("    ↑ ↓ / j k  scroll the content")
	add("    PgUp/PgDn  page down or up")
	add("    g/G / Home go to the top or bottom")
	add("    l          full log of the run")
	add("    d          actual diff of the run")
	add("    + / -      rate the run good or bad (again: clear)")
	add("    esc/tab    back to the list (or enter)")
	add("")
	add("  " + bold.Render("Log and diff"))
	add("    ↑ ↓ / j k  scroll line by line")
	add("    PgUp/PgDn  page down or up")
	add("    g/G / Home go to the top or bottom")
	add("    r          show or hide noise in the log (Go / traces)")
	add("    esc / q    close the viewer and go back")
	add("    ?          open this help")
	add("")
	add("  " + bold.Render("Timeline"))
	add("    ← →        previous or next day")
	add("    1 2 3 4    switch view")
	add("    esc        back to the dashboard")
	add("")
	add("  " + bold.Render("Report"))
	add("    ← → / tab  switch the period between 7 and 30 days")
	add("    7          set the period to 7 days")
	add("    1 2 3 4    switch view")
	add("    esc / i    back to the dashboard")
	add("    ?          open this help")
	add("    q          quit")
	add("")

	// 2. Statuses
	add(sectionTitle("Statuses"))
	add("")
	add("  " + lipgloss.NewStyle().Foreground(cGreen).Render("●") + " working : the agent is in the middle of a run")
	add("  " + lipgloss.NewStyle().Foreground(cBlue).Render("●") + " orchestrating : Claude Code is directing the agents")
	add("  " + lipgloss.NewStyle().Foreground(cGreen).Render("✔") + " done : it ended its turn; that doesn't mean it went well")
	add("  " + lipgloss.NewStyle().Foreground(cRed).Render("✖") + " failed : the run ended with an error")
	add("  " + lipgloss.NewStyle().Foreground(cAmber).Render("◐") + " out of quota : it ran out of quota; panal delegate moved on to the next one")
	add("  " + lipgloss.NewStyle().Foreground(cAmber).Render("⊘") + " no permission : it was missing a permission and aborted the turn")
	add("  " + lipgloss.NewStyle().Foreground(cRed).Render("✖") + " stuck : process alive but no progress in the log")
	add("  " + lipgloss.NewStyle().Foreground(cDim).Render("○") + " idle : at rest, no active process")
	add("  " + lipgloss.NewStyle().Foreground(cDim).Render("⏻") + " off : at rest and turned off in the config (panal.conf)")
	add("  " + lipgloss.NewStyle().Foreground(cRed).Render("⚠") + " broke the task : touched files it wasn't allowed to, or made a forbidden commit")
	add("  ✦ unseen : its run ended while you were away; opening it with enter, l or d clears it")
	add("  " + lipgloss.NewStyle().Foreground(cGreen).Render("▲") + " / " + lipgloss.NewStyle().Foreground(cRed).Render("▼") + " rated : you rated the run good / bad (+ / - in History, or panal feedback)")
	add("")

	// Mascots: what each animation means.
	add(sectionTitle("Mascots"))
	add("")
	add("  moves (baton, floats, types…)    : working")
	add("  blinks and looks around          : at rest")
	add("  eyes closed and z, in color      : nap, more than 2 h without work")
	add("  gray, asleep                     : out of quota, no permission or failed")
	add("  gray and shaking                 : stuck")
	add("  drop of sweat                    : a quota or the context is above 80 %")
	add("  confetti · red \"!\" · hop         : done · failed · started working")
	add("  › text below                     : the last thing it did while working")
	add("  ⟳ ×4 command (amber)             : repeating itself; it may be stuck")
	add("  ✓ / ✗ test command               : whether the last tests it ran passed (codex)")
	add("  ★ N in a row without failing     : its run streak")
	add("  p                                : pet it")
	add("")

	// 3. Quotas
	add(sectionTitle("Quotas"))
	add("")
	add("  " + lipgloss.NewStyle().Foreground(cGreen).Render("█ green") + " : less than 60 % of the quota used")
	add("  " + lipgloss.NewStyle().Foreground(cAmber).Render("█ amber") + " : 60 % of the quota used or more")
	add("  " + lipgloss.NewStyle().Foreground(cRed).Render("█ red") + " : 85 % of the quota used or more")
	add("  " + lipgloss.NewStyle().Foreground(cAmber).Render("⚠ ~15:40") + " : at this pace it runs out before the reset")
	add("")

	// 4. Glossary
	add(sectionTitle("What things mean"))
	add("")
	add("  " + bold.Render("task") + " : the user's goal assigned to the agent, without the rules")
	add("  " + bold.Render("run") + " : a single delegated attempt (panal delegate) with a start, an end and a result")
	add("  " + bold.Render("actual changes") + " : edits and commits in the dir, verified with git")
	add("  " + bold.Render("broke") + " : changed files outside the allowed ones or made forbidden commits")
	add("  " + bold.Render("credits") + " : real OpenAI / Codex balance and spend, from the sessions")
	add("  " + bold.Render("router") + " : panal delegate -c auto picks the agent from past runs; see panal route")
	add("  " + bold.Render("tier") + " : how big a task looks to the router: simple, medium or complex")
	add("")

	return ls
}

func (m Model) helpView() string {
	width := max(m.width, 40)
	head, footer := m.helpParts()
	lines := m.helpLines(width)
	availHeight := m.helpHeight()

	offset := min(max(0, m.helpOffset), max(0, len(lines)-availHeight))
	end := min(len(lines), offset+availHeight)

	var b strings.Builder
	b.WriteString(head)

	// Title like the log's; the position only if not everything fits, and on
	// the right so it doesn't look like part of the content.
	ttl := lipgloss.NewStyle().Bold(true).Foreground(cBrand).Render(" HELP")
	if len(lines) > availHeight {
		pos := fmt.Sprintf("%d–%d of %d · ↑↓ for more ", min(offset+1, len(lines)), end, len(lines))
		if gap := width - lipgloss.Width(ttl) - lipgloss.Width(pos); gap >= 1 {
			ttl += strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(cDim).Render(pos)
		}
	}
	b.WriteString(ttl + "\n\n")

	for i := offset; i < end; i++ {
		b.WriteString(lines[i] + "\n")
	}
	for i := end - offset; i < availHeight; i++ {
		b.WriteString("\n")
	}

	return clipHeight(b.String(), m.screenHeight()-lipgloss.Height(footer)) + footer
}
