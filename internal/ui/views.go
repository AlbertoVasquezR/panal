package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AlbertoVasquezR/panal/internal/changes"
	"github.com/AlbertoVasquezR/panal/internal/feedback"
	"github.com/AlbertoVasquezR/panal/internal/history"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/runs"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

var agentOrder = []string{"claude", "agy", "codex", "opencode", "cursor"}

// Run results, as stored in the run files (see internal/runs).
const (
	runDone         = runs.Done
	runRunning      = runs.Running
	runOutOfQuota   = runs.OutOfQuota
	runNoPermission = runs.NoPermission
	runSkipped      = runs.Skipped
	runTimedOut     = runs.Timeout
	runInterrupted  = runs.Interrupted
	runFailed       = runs.Failed
)

// Badge for a run result: icon, text and color.
func resultBadge(result runs.Status) (string, lipgloss.TerminalColor) {
	switch result {
	case runDone:
		return "✔ done", cGreen
	case runRunning:
		return "● running", cBlue
	case runOutOfQuota:
		return "◐ out of quota", cAmber
	case runNoPermission:
		return "⊘ no permission", cAmber
	case runSkipped:
		return "↷ skipped", cDim
	case runTimedOut:
		return "⌛ timed out", cRed
	case runInterrupted:
		return "✖ interrupted", cRed
	default:
		return "✖ failed", cRed
	}
}

func agentColor(a string) lipgloss.TerminalColor {
	if c, ok := accent[a]; ok {
		return c
	}
	return cDim
}

// -------------------------------------------------------------- history --

// Result filters (e key), in cycle order. "failures" and "quota" group
// several results; the others are a run result as is.
var resultOrder = []string{"", "failures", "quota", string(runDone), string(runRunning)}

var resultName = map[string]string{
	"failures": "failures", "quota": "out of quota/permission", string(runDone): "done", string(runRunning): "running",
}

func matchesResult(filter string, status runs.Status) bool {
	switch filter {
	case "":
		return true
	case "failures":
		return status == runFailed || status == runTimedOut || status == runInterrupted
	case "quota":
		return status == runOutOfQuota || status == runNoPermission
	default:
		return string(status) == filter
	}
}

// filteredRuns applies the filters: agent (f), result (e) and text (/), which
// is searched case-insensitively in task, model and dir.
func (m Model) filteredRuns() []history.Run {
	if m.historyFilter == "" && m.historyResult == "" && m.query == "" {
		return m.runs
	}
	q := strings.ToLower(m.query)
	var out []history.Run
	for _, c := range m.runs {
		if m.historyFilter != "" && c.Agent != m.historyFilter {
			continue
		}
		if !matchesResult(m.historyResult, c.Status) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(c.Task+"\x00"+c.FullTask+"\x00"+c.Model+"\x00"+c.Dir), q) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// next value in a cycle.
func next(order []string, current string) string {
	for i, v := range order {
		if v == current {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

// historyKey moves the cursor or changes the filters; true if it used the key.
func (m *Model) historyKey(k string) bool {
	n := len(m.filteredRuns())
	page := m.historyVisibleRows()
	switch k {
	case "up", "k":
		m.historyCursor--
	case "down", "j":
		m.historyCursor++
	case "pgup":
		m.historyCursor -= page
	case "pgdown":
		m.historyCursor += page
	case "home", "g":
		m.historyCursor = 0
	case "end", "G":
		m.historyCursor = n - 1
	case "f":
		// Cycle: all → claude → agy → codex → opencode → all.
		m.historyFilter = next(append([]string{""}, agentOrder...), m.historyFilter)
		m.historyCursor, m.historyOffset = 0, 0
		return true
	case "e":
		m.historyResult = next(resultOrder, m.historyResult)
		m.historyCursor, m.historyOffset = 0, 0
		return true
	case "/":
		m.searching = true
		return true
	case "esc":
		// esc clears the filters; with none set it does nothing.
		m.historyFilter, m.historyResult, m.query = "", "", ""
		m.historyCursor, m.historyOffset = 0, 0
		return true
	default:
		return false
	}
	if m.historyCursor >= n {
		m.historyCursor = n - 1
	}
	if m.historyCursor < 0 {
		m.historyCursor = 0
	}
	if m.historyCursor < m.historyOffset {
		m.historyOffset = m.historyCursor
	}
	if m.historyCursor >= m.historyOffset+page {
		m.historyOffset = m.historyCursor - page + 1
	}
	return true
}

// searchKey handles typing while the search is open. enter keeps it, esc
// clears it; the list filters as you type.
func (m *Model) searchKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyEnter:
		m.searching = false
	case tea.KeyEsc:
		m.searching, m.query = false, ""
	case tea.KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.query += string(msg.Runes)
		if msg.Type == tea.KeySpace {
			m.query += " "
		}
	}
	m.historyCursor, m.historyOffset = 0, 0
}

func (m Model) screenHeight() int {
	if m.height > 0 {
		return m.height
	}
	return 40
}

// historyParts are the pieces of the history screen other than the list. They
// are measured for real (not with assumed heights) so the list fits in the rest.
type historyParts struct {
	head, summary, search, titles, footer string
}

func (m Model) historyWidth() int { return max(m.width, 40) }

func (m Model) historyPartsOf() historyParts {
	width := m.historyWidth()
	vw := "history"
	if m.historyFilter != "" {
		vw += " · " + m.historyFilter
	}
	if m.historyResult != "" {
		vw += " · " + resultName[m.historyResult]
	}
	cs := m.filteredRuns()
	p := historyParts{
		head:   Header(m.rows, m.now, width, vw) + "\n" + tabBar("history", width) + "\n" + m.statusPhrase(width) + "\n" + m.alertBand(width),
		titles: headerRow(historyColumns(cs, width)) + "\n",
		footer: Footer(width, "↑↓", "select", "enter", "detail", "/", "search", "f", "agent", "e", "result", "esc", "back"),
	}
	if m.searching || m.query != "" {
		cursor := ""
		if m.searching {
			cursor = "▏"
		}
		p.search = lipgloss.NewStyle().Foreground(cBlue).Render(truncate("  / "+m.query+cursor, width)) +
			lipgloss.NewStyle().Foreground(cDim).Render("   "+plural(len(cs), "run")) + "\n"
	}
	// The summary panels are 9 lines tall: they only show if the height is at
	// least 30, 6 runs still fit with them and there is room; otherwise one line.
	fixed := lipgloss.Height(p.head+p.search+p.titles+p.footer) + 1
	if width >= 80 && m.screenHeight() >= 30 && m.screenHeight()-fixed-9 >= 6 {
		p.summary = m.summaryPanels(width) + "\n"
	} else {
		p.summary = lipgloss.NewStyle().Foreground(cDim).Render(truncate("  "+m.todaySummary(), width)) + "\n\n"
	}
	return p
}

// historyVisibleRows: the list lines (runs and day separators) that fit after
// the other parts and the "1–14 of 23" line.
func (m Model) historyVisibleRows() int {
	p := m.historyPartsOf()
	fixed := lipgloss.Height(p.head+p.summary+p.search+p.titles+p.footer) + 1
	if v := m.screenHeight() - fixed; v > 3 {
		return v
	}
	return 3
}

// todaySummary: the panels on one line, for when they don't fit.
func (m Model) todaySummary() string {
	return history.Summary(m.runs, m.now)
}

// summaryPanels: "TODAY" with counts and "BY AGENT" with bars.
func (m Model) summaryPanels(width int) string {
	today := m.now
	sameDay := func(t time.Time) bool {
		y1, m1, d1 := t.Local().Date()
		y2, m2, d2 := today.Local().Date()
		return y1 == y2 && m1 == m2 && d1 == d2
	}
	var todays []history.Run
	for _, c := range m.runs {
		if sameDay(c.Start) {
			todays = append(todays, c)
		}
	}
	dimS := lipgloss.NewStyle().Foreground(cDim)
	big := lipgloss.NewStyle().Bold(true)

	// TODAY panel.
	count := map[runs.Status]int{}
	var total time.Duration
	for _, c := range todays {
		count[c.Status]++
		if !c.End.IsZero() {
			total += c.End.Sub(c.Start)
		}
	}
	var chips []string
	for _, r := range []runs.Status{runDone, runRunning, runOutOfQuota, runNoPermission, runFailed, runTimedOut, runInterrupted, runSkipped} {
		if count[r] == 0 {
			continue
		}
		txt, c := resultBadge(r)
		chips = append(chips, lipgloss.NewStyle().Foreground(c).Render(fmt.Sprintf("%s %d", txt, count[r])))
	}
	cr := creditsSpent(todays)
	runsWord := " runs"
	if len(todays) == 1 {
		runsWord = " run"
	}
	left := []string{
		dimS.Render("TODAY · ") + big.Render(shortDate(today)),
		"",
		big.Render(fmt.Sprintf("%d", len(todays))) + dimS.Render(runsWord+" · ") +
			big.Render(readers.Ago(total)) + dimS.Render(" of agent work"),
	}
	if cr > 0 {
		left = append(left, lipgloss.NewStyle().Foreground(cAmber).Render(fmt.Sprintf("%d credits spent", cr)))
	}
	left = append(left, "")
	for i := 0; i < len(chips); i += 3 {
		end := i + 3
		if end > len(chips) {
			end = len(chips)
		}
		left = append(left, strings.Join(chips[i:end], "   "))
	}

	// BY AGENT panel: today's runs per agent and how many finished.
	maxN := 1
	perAgent := map[string][2]int{} // [runs, done]
	for _, c := range todays {
		v := perAgent[c.Agent]
		v[0]++
		if c.Status == runDone {
			v[1]++
		}
		perAgent[c.Agent] = v
		if v[0] > maxN {
			maxN = v[0]
		}
	}
	// Each box is panelWidth + 2 for the border, with a space between the two.
	panelWidth := (width - 5) / 2
	barLength := panelWidth - 30
	if barLength < 6 {
		barLength = 6
	}
	right := []string{dimS.Render("BY AGENT · today"), ""}
	for _, a := range agentOrder {
		v := perAgent[a]
		if v[0] == 0 {
			bar := lipgloss.NewStyle().Foreground(cEmpty).Render(strings.Repeat("░", barLength))
			right = append(right, lipgloss.NewStyle().Foreground(agentColor(a)).Width(9).Render(a)+" "+
				bar+" "+dimS.Render("no runs"))
		} else {
			filled := v[0] * barLength / maxN
			bar := lipgloss.NewStyle().Foreground(agentColor(a)).Render(strings.Repeat("█", filled)) +
				lipgloss.NewStyle().Foreground(cEmpty).Render(strings.Repeat("░", barLength-filled))
			success := fmt.Sprintf("%3d%% ok", v[1]*100/v[0])
			right = append(right, lipgloss.NewStyle().Foreground(agentColor(a)).Width(9).Render(a)+" "+
				bar+" "+big.Render(fmt.Sprintf("%3d", v[0]))+dimS.Render("  "+success))
		}
	}

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder).
		Padding(0, 1).Width(panelWidth).Height(7)
	return lipgloss.JoinHorizontal(lipgloss.Top, box.Render(strings.Join(left, "\n")), " ", box.Render(strings.Join(right, "\n")))
}

// historyColumns splits the width between time, agent, model, result,
// duration, tokens, cost and task (the flexible one). Tokens and cost only show
// if some run has them; in narrow terminals the model shrinks and duration,
// tokens and cost are dropped, so the task keeps 24 columns.
func historyColumns(cs []history.Run, width int) []int {
	tokens, cost := 0, 0
	for _, c := range cs {
		if c.Tokens > 0 {
			tokens = 10
		}
		if c.Cost != "" {
			cost = 9
		}
	}
	return distribute([]int{6, 10, 20, 17, 9, tokens, cost, 0}, []int{6, 10, 12, 16, 0, 0, 0, 0}, 7, 24, width-2, []int{2, 5, 6, 4})
}

func headerRow(w []int) string {
	t := lipgloss.NewStyle().Foreground(cDim)
	return "  " + cell("TIME", w[0], t) + cell("AGENT", w[1], t) + cell("MODEL", w[2], t) + cell("RESULT", w[3], t) +
		cell("DURATION", w[4], t) + cell("TOKENS", w[5], t) + cell("COST", w[6], t) + cell("TASK", w[7], t)
}

// runRow draws a run on one line using the historyColumns widths; sel
// highlights it.
func runRow(c history.Run, w []int, width int, sel, viol bool) string {
	dimS := lipgloss.NewStyle().Foreground(cDim)
	normal := lipgloss.NewStyle()
	res, resColor := resultBadge(c.Status)
	if viol {
		// It broke the task (already checked): visible in the list without opening the detail.
		res, resColor = "⚠ "+res, cRed
	}
	resCell := cell(res, w[3], lipgloss.NewStyle().Foreground(resColor))
	if mark, mc := ratingMark(c.Rating); mark != "" && w[3] > 0 {
		// The user's rating, after the result: ▲ good, ▼ bad.
		resCell = cell(lipgloss.NewStyle().Foreground(resColor).Render(res)+" "+
			lipgloss.NewStyle().Foreground(mc).Render(mark), w[3], lipgloss.NewStyle())
	}
	dur := "—"
	if !c.End.IsZero() {
		dur = readers.Ago(c.End.Sub(c.Start))
	}
	line := "  " +
		cell(c.Start.Local().Format("15:04"), w[0], dimS) +
		cell(c.Agent, w[1], lipgloss.NewStyle().Foreground(agentColor(c.Agent)).Bold(true)) +
		cell(dash(shortModel(c.Model)), w[2], dimS) +
		resCell +
		cell(dur, w[4], normal) +
		cell(formatTokens(c.Tokens), w[5], normal) +
		cell(formatCost(c.Cost), w[6], lipgloss.NewStyle().Foreground(cAmber)) +
		taskCell(shortProject(c.Dir), taskTitle(c.FullTask, c.Task), w[7])
	if sel {
		return withSelBg(line, width)
	}
	return line
}

// dayHeader: "── TODAY · Thu 24 Sep ─────".
func dayHeader(t, today time.Time, width int) string {
	lbl := shortDate(t)
	y1, m1, d1 := t.Local().Date()
	y2, m2, d2 := today.Local().Date()
	yesterday := today.AddDate(0, 0, -1)
	y3, m3, d3 := yesterday.Local().Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		lbl = "TODAY · " + lbl
	case y1 == y3 && m1 == m3 && d1 == d3:
		lbl = "YESTERDAY · " + lbl
	}
	rest := width - lipgloss.Width(lbl) - 5
	if rest < 0 {
		rest = 0
	}
	return lipgloss.NewStyle().Foreground(cBrand).Render("── "+lbl+" ") +
		lipgloss.NewStyle().Foreground(cBorder).Render(strings.Repeat("─", rest))
}

// historyRows: how many lines the runs from..to (inclusive) take, counting
// the day separators.
func historyRows(cs []history.Run, from, to int) int {
	n, day := 0, ""
	for i := from; i <= to && i < len(cs); i++ {
		if d := cs[i].Start.Local().Format("2006-01-02"); d != day {
			n++
			day = d
		}
		n++
	}
	return n
}

func (m Model) historyView() string {
	if m.historyDetail {
		return m.runDetailView()
	}
	width := m.historyWidth()
	p := m.historyPartsOf()
	var b strings.Builder
	b.WriteString(p.head + p.summary + p.search + p.titles)

	cs := m.filteredRuns()
	dimS := lipgloss.NewStyle().Foreground(cDim)
	if len(cs) == 0 {
		if len(m.runs) == 0 {
			b.WriteString(dimS.Render(truncate("  No runs yet. Delegated runs show up here as soon as they start.", width)) + "\n")
		} else {
			b.WriteString(dimS.Render(truncate("  no runs match the filters · esc clears them", width)) + "\n")
		}
	}
	visible := m.historyVisibleRows()
	// Day separators take a line too: move the window start forward until the
	// selected run is in view.
	from := min(m.historyOffset, max(0, m.historyCursor))
	for from < m.historyCursor && historyRows(cs, from, m.historyCursor) > visible {
		from++
	}
	w := historyColumns(cs, width)
	var prevDay string
	usedN, end := 0, from
	for i := from; i < len(cs); i++ {
		c := cs[i]
		day := c.Start.Local().Format("2006-01-02")
		needs := 1
		if day != prevDay {
			needs = 2
		}
		if usedN+needs > visible {
			break
		}
		if day != prevDay {
			b.WriteString(dayHeader(c.Start, m.now, width) + "\n")
			prevDay = day
		}
		b.WriteString(runRow(c, w, width, i == m.historyCursor, m.violation(c)) + "\n")
		usedN += needs
		end = i + 1
	}
	if len(cs) > 0 {
		b.WriteString(dimS.Render(fmt.Sprintf("  %d–%d of %d", from+1, end, len(cs))) + "\n")
	}
	return clipHeight(b.String(), m.screenHeight()-lipgloss.Height(p.footer)) + p.footer
}

// RunChanges: what the changes package needs to know about a run. Exported
// so -report and -summary check runs the same way the dashboard does.
func RunChanges(c history.Run) changes.Run {
	end := c.End
	if c.Status == runRunning {
		end = time.Time{}
	}
	task := c.Task
	full := false
	if c.FullTask != "" {
		task = c.FullTask
		full = true
	}
	return changes.Run{
		Key:      c.Stamp + "+" + c.Agent,
		Dir:      c.Dir,
		Start:    c.Start,
		End:      end,
		Task:     task,
		Complete: full,
		ReadOnly: c.ReadOnly,
	}
}

// checkChanges runs git (cached) on the run's dir.
func (m Model) checkChanges(c history.Run) (changes.Result, bool) {
	if m.changes == nil || c.Dir == "" || c.Agent == "claude" {
		return changes.Result{}, false
	}
	return m.changes.Check(RunChanges(c), m.now), true
}

// violation: whether the run was already checked and broke its task. It does
// not run git: the list is redrawn often and only marks what is already known.
func (m Model) violation(c history.Run) bool {
	if m.changes == nil {
		return false
	}
	r, ok := m.changes.Peek(RunChanges(c).Key)
	return ok && r.HasViolations()
}

// rowLine is one line of a panel section: plain text and its color (it is
// truncated before coloring).
type rowLine struct {
	text  string
	color lipgloss.TerminalColor
}

// Section of a DetailPanel: a title and lines; Raw = text from a log, shown
// in a dimmed block.
type Section struct {
	Title string
	Lines []rowLine
	Raw   bool
}

func raw(title string, lines []string) Section {
	s := Section{Title: title, Raw: true}
	for _, l := range lines {
		s.Lines = append(s.Lines, rowLine{text: l})
	}
	return s
}

// changesSection: what the agent really touched and whether it broke the task.
func changesSection(r changes.Result) Section {
	s := Section{Title: "actual changes in the dir (by date, during the run)"}
	add := func(t string, c lipgloss.TerminalColor) { s.Lines = append(s.Lines, rowLine{t, c}) }
	for _, a := range r.Warnings {
		add("⚠ "+a, cRed)
	}
	if r.Err != "" {
		add("("+r.Err+")", cDim)
		return s
	}
	if len(r.Allowed) > 0 {
		t := "allowed: " + strings.Join(r.Allowed, ", ")
		if r.Truncated {
			t += "  (the stored task is truncated: there may be more)"
		}
		add(t, cDim)
	}
	const limitN = 8
	for i, a := range r.Files {
		if i == limitN {
			add(fmt.Sprintf("  … and %d more", len(r.Files)-limitN), cDim)
			break
		}
		t := a.Path
		switch {
		case a.New:
			t += "  (new)"
		case a.Deleted:
			t += "  (deleted)"
		case a.Added >= 0:
			t += fmt.Sprintf("  +%d −%d", a.Added, a.Removed)
		}
		if a.Forbidden {
			add("✖ "+t+"  not allowed", cRed)
		} else {
			add("✔ "+t, cGreen)
		}
	}
	if len(r.Files) == 0 {
		add("no uncommitted changes from this run", cDim)
	}
	for _, c := range r.Commits {
		add(fmt.Sprintf("commit %s %s  %s  (%s)", c.Hash, c.Time.Local().Format("15:04"), c.Subject, changes.Count(len(c.Files), "file")), cAmber)
	}
	if r.Next != nil && len(r.Files) == 0 {
		c := r.Next
		outbound := "all within what is allowed"
		if c.Outside > 0 {
			outbound = fmt.Sprintf("%d not allowed", c.Outside)
		}
		if len(r.Allowed) == 0 {
			outbound = ""
		}
		t := fmt.Sprintf("next: %s %s \"%s\" · %s", c.Hash, c.Time.Local().Format("15:04"), c.Subject, changes.Count(len(c.Files), "file"))
		if outbound != "" {
			t += ", " + outbound
		}
		add(t+" — the work may have landed there", cDim)
	}
	return s
}

// tabBar draws the tab bar: "1 Dashboard   2 History   3 Report   4 Timeline   ? Help".
func tabBar(active string, width int) string {
	tabs := []struct {
		id  string
		txt string
	}{
		{"dashboard", "1 Dashboard"},
		{"history", "2 History"},
		{"report", "3 Report"},
		{"timeline", "4 Timeline"},
		{"help", "? Help"},
	}
	// If the full names don't fit they are abbreviated and, if that is still
	// too wide, the inactive tabs keep only their number.
	shorts := map[string]string{"dashboard": "1 Dash.", "history": "2 Hist.", "report": "3 Rep.", "timeline": "4 Time.", "help": "?"}
	paint := func(level int) string {
		var parts []string
		for _, t := range tabs {
			txt := t.txt
			if level >= 1 {
				txt = shorts[t.id]
			}
			if t.id == active {
				parts = append(parts, lipgloss.NewStyle().Bold(true).
					Foreground(lipgloss.Color("#FFFFFF")).Background(cBrand).
					Padding(0, 1).Render(txt))
				continue
			}
			if level >= 2 {
				txt, _, _ = strings.Cut(txt, " ")
			}
			parts = append(parts, lipgloss.NewStyle().Foreground(cDim).
				Render(" "+txt+" "))
		}
		return "  " + strings.Join(parts, " ")
	}
	for level := 0; level < 2; level++ {
		if s := paint(level); lipgloss.Width(s) <= width {
			return s
		}
	}
	return paint(2)
}

// ratingMark is the mark of the user's rating (panal feedback, or + / − in
// History): ▲ good in green, ▼ bad in red, "" when not rated.
func ratingMark(rating string) (string, lipgloss.TerminalColor) {
	switch rating {
	case feedback.Good:
		return "▲", cGreen
	case feedback.Bad:
		return "▼", cRed
	}
	return "", nil
}

// wrapWords splits s into lines of at most width columns, at spaces.
func wrapWords(s string, width int) []string {
	width = max(width, 10)
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		switch {
		case line == "":
			line = w
		case lipgloss.Width(line)+1+lipgloss.Width(w) <= width:
			line += " " + w
		default:
			lines = append(lines, line)
			line = w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// runDetailData extracts the pairs and sections of a run's detail, for a
// panel width columns wide.
func (m Model) runDetailData(c history.Run, width int) (title string, pairs [][2]string, secs []Section) {
	res, resColor := resultBadge(c.Status)
	rc := "—"
	if c.RC != nil {
		rc = fmt.Sprintf("%d", *c.RC)
	}
	period := shortDate(c.Start) + " " + c.Start.Local().Format("15:04:05")
	if !c.End.IsZero() {
		period += " → " + c.End.Local().Format("15:04:05") + "  (" + readers.Ago(c.End.Sub(c.Start)) + ")"
	}
	shortTask := c.Task
	if i := strings.IndexAny(shortTask, "\r\n"); i >= 0 {
		shortTask = shortTask[:i]
	}
	pairs = [][2]string{
		{"result", lipgloss.NewStyle().Foreground(resColor).Render(res) + "   rc " + rc},
	}
	if mark, mc := ratingMark(c.Rating); mark != "" {
		txt := mark + " " + c.Rating
		if c.RatingNote != "" {
			txt += " · " + c.RatingNote
		}
		pairs = append(pairs, [2]string{"rating", lipgloss.NewStyle().Foreground(mc).Render(truncate(txt, max(10, width-16)))})
	}
	pairs = append(pairs,
		[2]string{"model", dash(c.Model)},
		[2]string{"when", period},
		[2]string{"dir", dash(c.Dir)},
		[2]string{"log", dash(c.Log)},
		[2]string{"task", dash(shortTask)},
	)
	if c.TaskType != "" {
		kind := c.TaskType
		if c.Tier != "" {
			kind += " · " + c.Tier
		}
		if c.Auto {
			kind += " · picked by the router"
		}
		pairs = append(pairs, [2]string{"type", kind})
	}
	if c.ReadOnly {
		pairs = append(pairs, [2]string{"mode", "read-only"})
	}
	if c.Tokens > 0 || c.Cost != "" {
		pairs = append(pairs, [2]string{"usage", formatTokens(c.Tokens) + " tokens · " + formatCost(c.Cost)})
	}
	if c.FullTask != "" {
		lines := strings.Split(strings.ReplaceAll(c.FullTask, "\r\n", "\n"), "\n")
		if len(lines) > 1 {
			const limitN = 12
			var shown []string
			if len(lines) > limitN {
				shown = append(shown, lines[:limitN]...)
				shown = append(shown, fmt.Sprintf("… %d more lines", len(lines)-limitN))
			} else {
				shown = lines
			}
			secs = append(secs, raw("full task", shown))
		}
	}
	if c.Route != "" {
		why := strings.TrimPrefix(c.Route, "auto: ")
		if c.Choice > 1 {
			why = fmt.Sprintf("fallback #%d of the chain; the first pick was: %s", c.Choice, why)
		}
		s := Section{Title: "why this agent (router)"}
		for _, l := range wrapWords(why, width-6) {
			s.Lines = append(s.Lines, rowLine{text: l})
		}
		secs = append(secs, s)
	}
	if r, ok := m.checkChanges(c); ok {
		secs = append(secs, changesSection(r))
	}
	if c.Log != "" {
		if tail := readers.LastLines(c.Log, 5); len(tail) > 0 {
			secs = append(secs, raw("last lines of the log  (l: full)", tail))
		}
	}
	title = strings.ToUpper(c.Agent)
	if c.Stamp != "" {
		title += "  " + c.Stamp
	}
	return title, pairs, secs
}

// runDetail: a card with the run's data, what really changed and the last
// lines of the log.
func (m Model) runDetail(c history.Run, width int) string {
	title, pairs, secs := m.runDetailData(c, width)
	return DetailPanel(title, agentColor(c.Agent), pairs, secs, width)
}

func (m Model) runDetailLines(c history.Run, width int) []string {
	title, pairs, secs := m.runDetailData(c, width)
	return detailPanelLines(title, agentColor(c.Agent), pairs, secs, width)
}

// runDetailView draws a run's detail full screen.
func (m Model) runDetailView() string {
	width := max(m.width, 40)
	cs := m.filteredRuns()
	if len(cs) == 0 {
		return ""
	}
	sel := min(max(0, m.historyCursor), len(cs)-1)
	c := cs[sel]

	head := Header(m.rows, m.now, width, "history · detail") + "\n"
	if al := m.alertBand(width); al != "" {
		head += al
	}

	footer := Footer(width, "←→", "prev/next", "↑↓", "scroll", "l", "log", "d", "diff", "+/-", "rate", "esc", "back")

	ttl := lipgloss.NewStyle().Bold(true).Foreground(agentColor(c.Agent)).Render(strings.ToUpper(c.Agent))
	if c.Stamp != "" {
		ttl += "  " + lipgloss.NewStyle().Foreground(cDim).Render(c.Stamp)
	}
	nav := lipgloss.NewStyle().Foreground(cDim).Render("‹ previous · next ›")
	left := "  " + ttl + "   " + nav

	lines := m.runDetailLines(c, width)
	availHeight := max(5, m.screenHeight()-lipgloss.Height(head)-lipgloss.Height(footer)-2)
	box, off, end, total := detailBox(lines, agentColor(c.Agent), width, availHeight, m.historyDetailOffset)

	rowSelector := left
	if total > availHeight-2 {
		pos := fmt.Sprintf("%d–%d of %d · ↑↓ for more", min(off+1, total), end, total)
		if gap := width - lipgloss.Width(left) - lipgloss.Width(pos); gap >= 1 {
			rowSelector = left + strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(cDim).Render(pos)
		}
	}
	if lipgloss.Width(rowSelector) > width {
		rowSelector = truncate(rowSelector, width)
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString(rowSelector + "\n\n")
	b.WriteString(box + "\n")

	return clipHeight(b.String(), m.screenHeight()-lipgloss.Height(footer)) + footer
}

// detailPanelLines builds the formatted inner lines of the detail panel.
func detailPanelLines(title string, ac lipgloss.TerminalColor, pairs [][2]string, secs []Section, width int) []string {
	inner := width - 4
	dimS := lipgloss.NewStyle().Foreground(cDim)
	normal := lipgloss.NewStyle()
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(ac).Render(title), ""}
	labelWidth := 8
	for _, p := range pairs {
		if w := lipgloss.Width(p[0]); w > labelWidth {
			labelWidth = w
		}
	}
	if labelWidth > 18 {
		labelWidth = 18
	}
	for _, p := range pairs {
		lbl := fmt.Sprintf("%-*s", labelWidth, truncate(p[0], labelWidth))
		value := p[1]
		if lipgloss.Width(value) > inner-labelWidth-1 && !strings.Contains(value, "\x1b") {
			if looksLikePath(value) {
				value = truncateMiddle(value, inner-labelWidth-1)
			} else {
				value = truncate(value, inner-labelWidth-1)
			}
		}
		renderedVal := value
		if !strings.Contains(value, "\x1b") {
			renderedVal = normal.Render(value)
		}
		lines = append(lines, dimS.Render(lbl)+" "+renderedVal)
	}
	for _, s := range secs {
		if len(s.Lines) == 0 {
			continue
		}
		prefix := "── "
		ttl := truncate(s.Title, inner-lipgloss.Width(prefix)-2)
		rest := inner - lipgloss.Width(prefix) - lipgloss.Width(ttl) - 1
		if rest < 0 {
			rest = 0
		}
		sep := dimS.Render(prefix + ttl + " " + strings.Repeat("─", rest))
		lines = append(lines, "", sep)
		var rs []string
		for _, r := range s.Lines {
			if s.Raw {
				rs = append(rs, truncate(r.text, inner-4))
				continue
			}
			st := lipgloss.NewStyle()
			if r.color != nil {
				st = st.Foreground(r.color)
			}
			rs = append(rs, st.Render(truncate(r.text, inner)))
		}
		if s.Raw {
			rawBlock := lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(cBorder).Foreground(lipgloss.Color("250")).PaddingLeft(1).
				Render(strings.Join(rs, "\n"))
			for _, cl := range strings.Split(rawBlock, "\n") {
				lines = append(lines, cl)
			}
		} else {
			lines = append(lines, rs...)
		}
	}
	return lines
}

// DetailPanel: a card with label/value pairs and sections below.
func DetailPanel(title string, ac lipgloss.TerminalColor, pairs [][2]string, secs []Section, width int) string {
	inner := width - 4
	lines := detailPanelLines(title, ac, pairs, secs, width)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ac).
		Padding(0, 1).Width(inner + 2).Render(strings.Join(lines, "\n"))
}

// detailBox takes the inner lines and returns the bordered box clipped to maxHeight, scrolled to offset.
func detailBox(lines []string, ac lipgloss.TerminalColor, width, maxHeight, offset int) (string, int, int, int) {
	total := len(lines)
	inner := width - 4
	availLines := max(1, maxHeight-2)
	off := min(max(0, offset), max(0, total-availLines))
	end := min(total, off+availLines)
	visible := lines[off:end]
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ac).
		Padding(0, 1).Width(inner + 2).Render(strings.Join(visible, "\n"))
	return box, off, end, total
}

// lastRun of the agent according to the history (newest first).
func (m Model) lastRun(agent string) (history.Run, bool) {
	for _, c := range m.runs {
		if c.Agent == agent {
			return c, true
		}
	}
	return history.Run{}, false
}

// agentDetailData extracts the pairs and sections of an agent's detail.
func (m Model) agentDetailData(f state.Row) (title string, pairs [][2]string, secs []Section) {
	var tail, logLines []string
	if f.Dir != "" {
		pairs = append(pairs, [2]string{"dir", f.Dir})
	}
	if t := activityText(f, m.now); t != "" {
		pairs = append(pairs, [2]string{"now", t})
	}
	if f.Tests != "" {
		c := cGreen
		if !f.TestsOK {
			c = cRed
		}
		pairs = append(pairs, [2]string{"tests", lipgloss.NewStyle().Foreground(c).Render(f.Tests)})
	}
	for _, l := range strings.Split(f.Detail, "\n") {
		if r, ok := strings.CutPrefix(l, readers.LogPrefix); ok {
			logLines = append(logLines, r)
			continue
		}
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		parts := strings.Split(l, " · ")
		allKey := true
		for _, p := range parts {
			if i := strings.Index(p, ": "); i <= 0 || i > 22 {
				allKey = false
			}
		}
		if allKey {
			for _, p := range parts {
				i := strings.Index(p, ": ")
				pairs = append(pairs, [2]string{p[:i], p[i+2:]})
			}
			continue
		}
		tail = append(tail, l)
	}
	if f.Quota.Spend != "" {
		pairs = append(pairs, [2]string{"spend", f.Quota.Spend})
	}
	if f.Error != "" {
		pairs = append(pairs, [2]string{"error", lipgloss.NewStyle().Foreground(cRed).Render(f.Error)})
	}
	if c, ok := m.lastRun(f.Agent); ok {
		if r, ok := m.checkChanges(c); ok {
			s := changesSection(r)
			s.Title = "last run (" + c.Start.Local().Format("15:04") + "): " + s.Title
			secs = append(secs, s)
		}
	}
	if len(logLines) > 0 {
		secs = append(secs, raw("last lines of the log  (l: full)", logLines))
	}
	if len(tail) > 0 {
		secs = append(secs, raw("notes", tail))
	}
	title = strings.ToUpper(f.Agent) + "  ·  " + f.Status.String()
	return title, pairs, secs
}

// agentDetail turns a row's Detail (text from the readers) into label/value
// pairs when a line is "key: value"; everything else goes in raw. For delegated
// agents it adds what their last run changed.
func (m Model) agentDetail(f state.Row, width int) string {
	title, pairs, secs := m.agentDetailData(f)
	return DetailPanel(title, agentColor(f.Agent), pairs, secs, width)
}

func (m Model) agentDetailLines(f state.Row, width int) []string {
	title, pairs, secs := m.agentDetailData(f)
	return detailPanelLines(title, agentColor(f.Agent), pairs, secs, width)
}

// agentDetailView draws the agent's detail full screen.
func (m Model) agentDetailView() string {
	width := max(m.width, 40)
	sel := min(max(0, m.table.Cursor()), len(m.rows)-1)
	if sel < 0 || len(m.rows) == 0 {
		return ""
	}
	f := m.rows[sel]

	head := Header(m.rows, m.now, width, "detail · "+f.Agent) + "\n"
	if al := m.alertBand(width); al != "" {
		head += al
	}

	footer := Footer(width, "←→", "other agent", "↑↓", "scroll", "l", "log", "d", "diff", "esc", "back")

	var selParts []string
	for i, row := range m.rows {
		if i == sel {
			nm := lipgloss.NewStyle().Bold(true).Foreground(cTextOnColor).
				Background(agentColor(row.Agent)).Padding(0, 1).Render(row.Agent)
			selParts = append(selParts, nm+" "+statusBadge(row.Status))
		} else {
			st := lipgloss.NewStyle().Foreground(agentColor(row.Agent))
			selParts = append(selParts, st.Render(row.Agent))
		}
	}
	sep := "   "
	if width < 60 {
		sep = " "
	}
	left := lipgloss.NewStyle().Foreground(cDim).Render("‹  ") +
		strings.Join(selParts, sep) +
		lipgloss.NewStyle().Foreground(cDim).Render("  ›")
	if lipgloss.Width(left) > width {
		left = truncate(left, width)
	}

	lines := m.agentDetailLines(f, width)
	availHeight := max(5, m.screenHeight()-lipgloss.Height(head)-lipgloss.Height(footer)-2)
	box, off, end, total := detailBox(lines, agentColor(f.Agent), width, availHeight, m.detailOffset)

	rowSelector := left
	if total > availHeight-2 {
		pos := fmt.Sprintf("%d–%d of %d · ↑↓ for more", min(off+1, total), end, total)
		if gap := width - lipgloss.Width(left) - lipgloss.Width(pos); gap >= 1 {
			rowSelector = left + strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(cDim).Render(pos)
		}
	}
	if lipgloss.Width(rowSelector) > width {
		rowSelector = truncate(rowSelector, width)
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString(rowSelector + "\n\n")
	b.WriteString(box + "\n")

	return clipHeight(b.String(), m.screenHeight()-lipgloss.Height(footer)) + footer
}

// -------------------------------------------------------- compact table --

// compactTable: one line per agent, colored by status.
func compactTable(rows []state.Row, sel, width int, now time.Time) string {
	dimS := lipgloss.NewStyle().Foreground(cDim)
	var b strings.Builder
	// Columns: agent, status, model, time, quota, task (the flexible one).
	// In narrow terminals the model shrinks, then time is dropped and finally quota.
	w := distribute([]int{10, 19, 26, 11, 34, 0}, []int{10, 19, 12, 0, 0, 0}, 5, 20, width-2, []int{2, 3, 4, 1})
	normal := lipgloss.NewStyle()
	b.WriteString("  " + cell("AGENT", w[0], dimS) + cell("STATUS", w[1], dimS) + cell("MODEL", w[2], dimS) +
		cell("TIME", w[3], dimS) + cell("QUOTA", w[4], dimS) + cell("TASK", w[5], dimS) + "\n")
	for i, f := range rows {
		quotaText := quota(f.Quota)
		if len(f.Quota.Bars) > 0 {
			var bs []string
			for _, br := range f.Quota.Bars {
				bs = append(bs, fmt.Sprintf("%s %.0f%%", strings.ReplaceAll(barLabel(br.Name), " ", ""), br.UsedPct))
			}
			quotaText = strings.Join(bs, " · ")
			if f.Quota.Credits != "" {
				quotaText += " · " + f.Quota.Credits + " cr"
			}
		}
		badge := statusBadge(f.Status)
		if f.Review != "" {
			badge += lipgloss.NewStyle().Foreground(cRed).Render(" ⚠")
		}
		line := "  " +
			cell(f.Agent, w[0], lipgloss.NewStyle().Foreground(agentColor(f.Agent)).Bold(true)) +
			cell(badge, w[1], lipgloss.NewStyle()) +
			cell(dash(shortModel(f.Model)), w[2], dimS) +
			cell(rowTime(f, now), w[3], normal) +
			cell(quotaText, w[4], normal) +
			taskCell(shortProject(f.Dir), taskTitle(f.FullTask, f.Task), w[5])
		if i == sel {
			line = withSelBg(line, width)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// plural: "1 run", "3 runs".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// shortDate: "Thu 24 Sep".
func shortDate(t time.Time) string {
	return t.Local().Format("Mon 2 Jan")
}

// creditsSpent adds up the credits ("N cr" in Cost) of the given runs.
func creditsSpent(cs []history.Run) int {
	total := 0
	for _, c := range cs {
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(c.Cost, " cr"))); err == nil && n > 0 && strings.HasSuffix(c.Cost, " cr") {
			total += n
		}
	}
	return total
}
