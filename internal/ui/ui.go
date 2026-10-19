// Package ui draws the rows. It reads no files: it asks the readers for everything.
package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AlbertoVasquezR/panal/internal/alerts"
	"github.com/AlbertoVasquezR/panal/internal/changes"
	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/credits"
	"github.com/AlbertoVasquezR/panal/internal/forecast"
	"github.com/AlbertoVasquezR/panal/internal/history"
	"github.com/AlbertoVasquezR/panal/internal/mascots"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/runs"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

type tick time.Time

type Model struct {
	readers             []readers.Reader
	rows                []state.Row
	table               table.Model
	every               time.Duration
	detail              bool
	detailOffset        int
	historyDetailOffset int
	now                 time.Time
	width               int
	height              int

	hist          *history.History
	runs          []history.Run
	inHistory     bool
	historyDetail bool
	historyCursor int    // selected run (in the filtered list)
	historyOffset int    // first visible run
	historyFilter string // agent being filtered on; empty = all

	inReport      bool
	inTimeline    bool            // 4: timeline
	title         string          // last title set on the window
	repeatAlerted map[string]bool // agent+repeated action already alerted
	timelineAll   []history.Run   // all runs, for the timeline of past days (m.runs holds only the latest 200)
	timelineDay   int             // how many days back the timeline shows (0 = today)
	unseen        map[string]bool // agents whose run ended and whose detail you have not opened
	sources       []sourceSeen    // first run: which sources exist (checked on tick, not while drawing)
	reportDays    int             // 7 or 30

	inHelp     bool // ?: help screen
	helpOffset int

	showMascots bool                // toggled with m
	frame       int                 // animation frame
	reactions   map[string]Reaction // mascot reactions in progress, by agent
	compact     bool                // t: the classic table instead of cards

	offRows []state.Row // Disabled agents at rest: shown on one line

	historyResult string // filter by result (e): "", failures, quota, done, running
	query         string // text searched (/) in task, model and dir
	searching     bool   // the search is being typed
	changes       *changes.Cache
	creditReader  *credits.Reader
	sampler       *forecast.Sampler
	savedAt       time.Time               // last time the samples were saved to disk
	quotaAlerted  map[string]bool         // key+reset already alerted
	seenRuns      map[string]runs.Status  // status of each run on the previous refresh
	statuses      map[string]state.Status // status of each agent on the previous refresh
	alert         string                  // latest alert, in the top band
	alertTime     time.Time
	reg           *logViewer // l: full-screen log
}

// Disabled are the agents not in use. At rest they get no card, just a gray
// line; if one starts working it gets its card back. If nil, they are taken
// from Conf on every refresh.
var Disabled map[string]bool

// Conf provides the disabled agents when Disabled is nil (~/.panal/panal.conf).
var Conf *config.Resolver

// AlertMode: which alerts are sent outside the dashboard (the -alerts flag).
var AlertMode = alerts.All

// OutboundAlerts: also send them to ntfy or a webhook (panal.conf: ntfy, webhook).
var OutboundAlerts alerts.Outbound

// SamplesPath: file where the forecast samples are stored.
// Empty = nothing is saved or loaded (for tests and text-only views).
var SamplesPath string

// nowFn lets tests replace time.Now.
var nowFn = time.Now

// LiveQuota: whoever asks the CLIs for their quota (nil = nobody). The r key
// asks it for a reading right now; what it detects (resets) shows up as an alert.
var LiveQuota interface {
	Refresh()
	TakeEvents() []string
}

// How long the latest alert stays in the top band.
const alertTTL = 30 * time.Second

func currentDisabled() map[string]bool {
	if Disabled != nil || Conf == nil {
		return Disabled
	}
	out := map[string]bool{}
	for _, a := range Conf.Disabled() {
		out[a] = true
	}
	return out
}

// anim advances the mascot animation; it is separate from the data tick so
// they move without re-reading anything.
type anim time.Time

const pulse = 450 * time.Millisecond

func animate() tea.Cmd {
	return tea.Tick(pulse, func(t time.Time) tea.Msg { return anim(t) })
}

// MascotMode picks the animation for the agent's status.
func MascotMode(e state.Status) mascots.Mode {
	switch e {
	case state.Working, state.Orchestrating:
		return mascots.Working
	case state.OutOfQuota, state.NoPermission, state.Failed:
		return mascots.Sleeping
	case state.Stuck:
		return mascots.Stuck
	default:
		return mascots.Idle
	}
}

// Corral column widths: the roomy one, and the narrowest that still fits
// the longest status ("⊘ no permission"), so five mascots fit in 80 columns.
const corralCol, corralMinCol = 18, 16

// corralColumn is the column width for n mascots in width, or 0 if even
// the narrow columns do not fit.
func corralColumn(n, width int) int {
	switch {
	case n*corralCol <= width:
		return corralCol
	case n*corralMinCol <= width:
		return corralMinCol
	}
	return 0
}

// Corral draws the mascots side by side, each with its name, status and the
// matching animation (MascotMode). sel is the highlighted index (-1: none).
func Corral(rows []state.Row, a Animation, sel int, now time.Time) string {
	return corral(rows, a, sel, now, corralCol)
}

// corralFor is the corral narrowed to fit width, or "" if it cannot.
func corralFor(rows []state.Row, a Animation, sel int, now time.Time, width int) string {
	if w := corralColumn(len(rows), width); w > 0 {
		return corral(rows, a, sel, now, w)
	}
	return ""
}

func corral(rows []state.Row, a Animation, sel int, now time.Time, colW int) string {
	col := lipgloss.NewStyle().Width(colW).Align(lipgloss.Center)
	var cols []string
	for i, f := range rows {
		s, ok := mascots.All[f.Agent]
		if !ok {
			continue
		}
		lbl := f.Agent
		if f.Unseen {
			lbl = "✦ " + lbl
		}
		name := title.Render(lbl)
		if i == sel {
			name = lipgloss.NewStyle().Bold(true).Foreground(cTextOnColor).
				Background(lipgloss.Color("39")).Padding(0, 1).Render(lbl)
		}
		// Per-mascot offset so they don't all blink or jump at once.
		mode, n, fx := a.offsetBy(i*3).mascot(f, now)
		// The zzz has a fixed width (3) so the line doesn't wobble as it grows.
		// In narrow columns it is left out when it would wrap the status;
		// the sprite still sleeps.
		status := icon(f.Status) + " " + f.Status.String()
		zs := dim.Render(fmt.Sprintf(" %-3s", mascots.Zs(mode, n)))
		if mode != mascots.Sleeping && mode != mascots.Nap || lipgloss.Width(status)+4 > colW {
			zs = ""
		}
		cols = append(cols, col.Render(s.RenderWith(mode, n, fx)+"\n"+name+"\n"+lipgloss.NewStyle().Foreground(statusColor(f.Status)).Render(status)+zs))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

func New(ls []readers.Reader, every time.Duration) Model {
	t := table.New(
		table.WithColumns(columns(100)),
		table.WithFocused(true),
		table.WithHeight(len(ls)+1),
	)
	stat := table.DefaultStyles()
	stat.Header = stat.Header.Bold(true).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true)
	stat.Selected = stat.Selected.Foreground(cTextOnColor).Background(lipgloss.Color("39"))
	t.SetStyles(stat)

	m := Model{
		readers:      ls,
		table:        t,
		every:        every,
		hist:         history.New(),
		width:        100,
		showMascots:  !noColor(),
		changes:      &changes.Cache{},
		creditReader: credits.New(),
		sampler:      forecast.New(),
		savedAt:      nowFn(),
		quotaAlerted: make(map[string]bool),
		reportDays:   7,
	}
	if SamplesPath != "" {
		_ = m.sampler.Load(SamplesPath, nowFn())
	}
	m.refresh()
	return m
}

func columns(width int) []table.Column {
	fixed := 10 + 14 + 22 + 8 + 30
	task := width - fixed - 12
	if task < 16 {
		task = 16
	}
	return []table.Column{
		{Title: "AGENT", Width: 10},
		{Title: "STATUS", Width: 14},
		{Title: "MODEL", Width: 22},
		{Title: "TASK", Width: task},
		{Title: "TIME", Width: 8},
		{Title: "QUOTA", Width: 30},
	}
}

// refresh re-reads everything and returns alerts for what changed since the
// previous refresh (none on the first one: old news is not alerted on start).
func (m *Model) refresh() []alert {
	m.now = nowFn()
	m.rows = m.rows[:0]
	m.offRows = m.offRows[:0]
	if m.sampler == nil {
		m.sampler = forecast.New()
	}
	if m.quotaAlerted == nil {
		m.quotaAlerted = make(map[string]bool)
	}
	if m.hist != nil {
		m.runs = m.hist.Read("", 200)
		if n := len(m.filteredRuns()); m.historyCursor >= n && n > 0 {
			m.historyCursor = n - 1
		}
	}

	firstLine := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		if i := strings.Index(s, "\n"); i >= 0 {
			return strings.TrimSpace(s[:i])
		}
		return strings.TrimSpace(s)
	}

	updateRow := func(f *state.Row) {
		f.Streak = streak(m.runs, f.Agent)
		if c, ok := m.lastRun(f.Agent); ok {
			if f.Task != "" && firstLine(f.Task) == firstLine(c.Task) {
				f.FullTask = c.FullTask
				if f.Dir == "" {
					f.Dir = c.Dir
				}
			}
			if m.changes != nil {
				if r, ok := m.changes.Peek(RunChanges(c).Key); ok && r.HasViolations() && len(r.Warnings) > 0 {
					f.Review = r.Warnings[0]
				}
			}
		}
	}

	offs := currentDisabled()
	usage := m.codexUsage()
	rows := make([]table.Row, 0, len(m.readers))
	for _, l := range m.readers {
		f := l.Read()
		updateRow(&f)
		if f.Agent == "codex" && usage.HasData {
			f.Quota.Spend, f.Quota.SpendWarning = spendText(usage)
		}
		for i := range f.Quota.Bars {
			b := &f.Quota.Bars[i]
			keyID := forecast.Key(f.Agent, b.Name)
			m.sampler.Record(keyID, m.now, b.UsedPct)
			if runsOut, ok := m.sampler.Project(keyID, m.now, b.UsedPct); ok {
				if b.ResetsAt.IsZero() || runsOut.Before(b.ResetsAt) {
					b.ExhaustsAt = runsOut
				}
			}
		}
		if offs[f.Agent] && atRest(f.Status) {
			m.offRows = append(m.offRows, f)
			continue
		}
		m.rows = append(m.rows, f)
		rows = append(rows, table.Row{
			f.Agent, mark(f.Status) + " " + f.Status.String(), dash(f.Model),
			dash(f.Task), rowTime(f, m.now), quota(f.Quota),
		})
	}
	m.table.SetRows(rows)
	if m.reg != nil {
		m.reg.reload()
	}
	if m.savedAt.IsZero() {
		m.savedAt = m.now
	} else if m.now.Sub(m.savedAt) >= time.Minute {
		m.saveSamples()
	}
	m.closeOrphans(m.runs)
	alertList := m.detectAlerts()
	m.checkSources()
	return alertList
}

// saveSamples writes the samples to disk if a path is set and there are new
// samples since the last save.
func (m *Model) saveSamples() {
	if SamplesPath == "" || m.sampler == nil || !m.sampler.Dirty() {
		return
	}
	_ = m.sampler.Save(SamplesPath)
	m.savedAt = m.now
}

func (m Model) Init() tea.Cmd {
	if NoAnimation {
		return wait(m.every)
	}
	return tea.Batch(wait(m.every), animate())
}

func wait(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tick(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tick:
		cmds := []tea.Cmd{wait(m.every)}
		for _, a := range m.refresh() {
			m.alert, m.alertTime = a.text, m.now
			cmds = append(cmds, sendAlert(a))
		}
		if LiveQuota != nil {
			for _, e := range LiveQuota.TakeEvents() {
				a := alert{title: "↻ " + e}
				m.alert, m.alertTime = a.title, m.now
				cmds = append(cmds, sendAlert(a))
			}
		}
		if c := m.titleCmd(); c != nil {
			cmds = append(cmds, c)
		}
		return m, tea.Batch(cmds...)
	case anim:
		m.frame++
		return m, animate()
	case ratedMsg:
		if msg.err != nil {
			m.alert, m.alertTime = "could not save the rating: "+msg.err.Error(), m.now
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetColumns(columns(msg.Width))
	case tea.KeyMsg:
		k := msg.String()
		if k == "ctrl+c" {
			m.saveSamples()
			return m, quit()
		}
		if m.inHelp {
			switch k {
			case "?", "q":
				m.inHelp = false
				m.helpOffset = 0
				return m, nil
			case "esc":
				m.inHelp = false
				m.helpOffset = 0
				m.inHistory = false
				m.inReport = false
				m.inTimeline = false
				return m, nil
			case "up", "k":
				m.helpOffset--
			case "down", "j":
				m.helpOffset++
			case "pgup":
				m.helpOffset -= m.helpHeight()
			case "pgdown", " ":
				m.helpOffset += m.helpHeight()
			case "home", "g":
				m.helpOffset = 0
			case "end", "G":
				m.helpOffset = 9999
			}
			maxOff := max(0, len(m.helpLines(max(m.width, 40)))-m.helpHeight())
			if m.helpOffset > maxOff {
				m.helpOffset = maxOff
			}
			if m.helpOffset < 0 {
				m.helpOffset = 0
			}
			return m, nil
		}
		if !m.searching && k == "?" {
			m.inHelp = true
			m.helpOffset = 0
			return m, nil
		}
		if m.reg != nil {
			if !m.reg.key(k, m.logHeight()) {
				m.reg = nil
			}
			return m, nil
		}
		if m.searching {
			m.searchKey(msg)
			return m, nil
		}
		if k == "4" {
			m.inTimeline = true
			m.timelineDay, m.timelineAll = 0, nil // on entry: today, with fresh data
			m.inReport, m.inHistory, m.historyDetail, m.detail = false, false, false, false
			return m, nil
		}
		if m.inTimeline {
			switch k {
			case "q":
				m.saveSamples()
				return m, quit()
			case "1", "esc":
				m.inTimeline = false
			case "2", "h":
				m.inTimeline, m.inHistory = false, true
			case "3", "i":
				m.inTimeline, m.inReport = false, true
				if m.reportDays == 0 {
					m.reportDays = 7
				}
			case "left":
				m.timelineDay = min(m.timelineDay+1, 60)
				m.loadTimeline()
			case "right":
				m.timelineDay = max(m.timelineDay-1, 0)
				m.loadTimeline()
			}
			return m, nil
		}
		if m.inReport {
			switch k {
			case "q", "ctrl+c":
				m.saveSamples()
				return m, quit()
			case "1":
				m.inReport = false
				m.inHistory = false
				return m, nil
			case "2", "h":
				m.inReport = false
				m.inHistory = true
				return m, nil
			case "3":
				return m, nil
			case "left":
				m.reportDays = 7
				return m, nil
			case "right":
				m.reportDays = 30
				return m, nil
			case "7":
				m.reportDays = 7
				return m, nil
			case "tab":
				if m.reportDays == 30 {
					m.reportDays = 7
				} else {
					m.reportDays = 30
				}
				return m, nil
			case "esc", "i":
				m.inReport = false
				m.inHistory = false
				return m, nil
			}
			return m, nil
		}
		if m.inHistory && m.historyDetail {
			cs := m.filteredRuns()
			switch k {
			case "q", "ctrl+c":
				m.saveSamples()
				return m, quit()
			case "esc", "tab", "enter":
				m.historyDetail = false
				m.historyDetailOffset = 0
				return m, nil
			case "left":
				if m.historyCursor > 0 {
					m.historyCursor--
					m.historyDetailOffset = 0
				}
				return m, nil
			case "right":
				if m.historyCursor < len(cs)-1 {
					m.historyCursor++
					m.historyDetailOffset = 0
				}
				return m, nil
			case "up", "k":
				m.historyDetailOffset--
			case "down", "j":
				m.historyDetailOffset++
			case "pgup":
				m.historyDetailOffset -= max(3, m.screenHeight()-8)
			case "pgdown", " ":
				m.historyDetailOffset += max(3, m.screenHeight()-8)
			case "home", "g":
				m.historyDetailOffset = 0
			case "end", "G":
				m.historyDetailOffset = 9999
			case "l":
				m.openLog()
				return m, nil
			case "d":
				m.openDiff()
				return m, nil
			case "c", "w":
				m.openDir(k)
				return m, nil
			case "+", "=", "-":
				return m, m.rateSelected(k)
			case "1", "h":
				m.historyDetail = false
				m.inHistory = false
				return m, nil
			case "2":
				m.historyDetail = false
				m.historyDetailOffset = 0
				return m, nil
			case "3", "i":
				m.historyDetail = false
				m.inHistory = false
				m.inReport = true
				if m.reportDays == 0 {
					m.reportDays = 7
				}
				return m, nil
			}
			if m.historyDetailOffset < 0 {
				m.historyDetailOffset = 0
			}
			return m, nil
		}
		if m.inHistory {
			switch k {
			case "q", "ctrl+c":
				m.saveSamples()
				return m, quit()
			case "esc", "h":
				m.inHistory = false
				return m, nil
			case "enter", "tab":
				if len(m.filteredRuns()) > 0 {
					m.historyDetail = true
					m.historyDetailOffset = 0
				}
				return m, nil
			case "1":
				m.inHistory = false
				return m, nil
			case "2":
				return m, nil
			case "3", "i":
				m.inHistory = false
				m.inReport = true
				if m.reportDays == 0 {
					m.reportDays = 7
				}
				return m, nil
			case "l":
				m.openLog()
				return m, nil
			case "d":
				m.openDiff()
				return m, nil
			case "c", "w":
				m.openDir(k)
				return m, nil
			case "+", "=", "-":
				return m, m.rateSelected(k)
			}
			if m.historyKey(k) {
				return m, nil
			}
		}
		if m.detail {
			switch k {
			case "q", "ctrl+c":
				m.saveSamples()
				return m, quit()
			case "esc", "tab", "enter":
				m.detail = false
				m.detailOffset = 0
				return m, nil
			case "left":
				if m.table.Cursor() > 0 {
					m.table.MoveUp(1)
					m.detailOffset = 0
				}
				return m, nil
			case "right":
				if m.table.Cursor() < len(m.rows)-1 {
					m.table.MoveDown(1)
					m.detailOffset = 0
				}
				return m, nil
			case "up", "k":
				m.detailOffset--
			case "down", "j":
				m.detailOffset++
			case "pgup":
				m.detailOffset -= max(3, m.screenHeight()-8)
			case "pgdown", " ":
				m.detailOffset += max(3, m.screenHeight()-8)
			case "home", "g":
				m.detailOffset = 0
			case "end", "G":
				m.detailOffset = 9999
			case "l":
				m.openLog()
				return m, nil
			case "d":
				m.openDiff()
				return m, nil
			case "c", "w":
				m.openDir(k)
				return m, nil
			case "1":
				m.detail = false
				m.detailOffset = 0
				return m, nil
			case "2", "h":
				m.detail = false
				m.detailOffset = 0
				m.inHistory = true
				return m, nil
			case "3", "i":
				m.detail = false
				m.detailOffset = 0
				m.inReport = true
				if m.reportDays == 0 {
					m.reportDays = 7
				}
				return m, nil
			}
			if m.detailOffset < 0 {
				m.detailOffset = 0
			}
			return m, nil
		}
		if k == "l" {
			m.markSelSeen()
			m.openLog()
			return m, m.titleCmd()
		}
		if k == "d" {
			m.markSelSeen()
			m.openDiff()
			return m, m.titleCmd()
		}
		if k == "c" || k == "w" {
			m.openDir(k)
			return m, nil
		}
		switch k {
		case "q", "ctrl+c":
			m.saveSamples()
			return m, quit()
		case "1":
			return m, nil
		case "2", "h":
			m.inHistory = true
			return m, nil
		case "3", "i":
			m.inReport = true
			if m.reportDays == 0 {
				m.reportDays = 7
			}
			return m, nil
		case "m":
			m.showMascots = !m.showMascots
			return m, nil
		case "r":
			if LiveQuota != nil {
				LiveQuota.Refresh()
				m.alert, m.alertTime = "↻ asking the agents for their quota…", m.now
			}
			return m, nil
		case "p":
			if sel, ok := m.selected(); ok {
				m.react(m.rows[sel].Agent, mascots.Pet)
			}
			return m, nil
		case "t":
			m.compact = !m.compact
			return m, nil
		case "esc":
			return m, nil
		case "left", "up":
			if !m.compact {
				before := m.table.Cursor()
				m.table.MoveUp(1)
				if m.table.Cursor() != before {
					m.greet()
				}
				return m, nil
			}
		case "right", "down":
			if !m.compact {
				before := m.table.Cursor()
				m.table.MoveDown(1)
				if m.table.Cursor() != before {
					m.greet()
				}
				return m, nil
			}
		case "tab", "enter":
			if len(m.rows) > 0 {
				m.detail = true
				m.detailOffset = 0
				m.markSelSeen()
			}
			return m, m.titleCmd()
		}
	}
	var cmd tea.Cmd
	if !m.inHistory && !m.detail {
		before := m.table.Cursor()
		m.table, cmd = m.table.Update(msg)
		if m.table.Cursor() != before {
			m.greet()
		}
	}
	return m, cmd
}

func (m *Model) openDiff() {
	var c history.Run
	ok := false
	if m.inHistory {
		if cs := m.filteredRuns(); m.historyCursor < len(cs) {
			c, ok = cs[m.historyCursor], true
		}
	} else if sel := min(m.table.Cursor(), len(m.rows)-1); sel >= 0 {
		c, ok = m.lastRun(m.rows[sel].Agent)
	}
	if !ok {
		m.alert, m.alertTime = "no run to show", m.now
		return
	}
	r, reviewOk := m.checkChanges(c)
	if !reviewOk || r.Err != "" {
		m.alert, m.alertTime = "no changes to show", m.now
		return
	}
	txt, err := changes.Diff(c.Dir, r)
	if err != nil {
		m.alert, m.alertTime = "could not get the diff: "+err.Error(), m.now
		return
	}
	if txt == "" {
		m.alert, m.alertTime = "no changes to show", m.now
		return
	}
	m.reg = newDiffViewer(c, txt)
}

var (
	title = lipgloss.NewStyle().Bold(true)
	dim   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	box   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func (m Model) View() string {
	if m.inHelp {
		return m.helpView()
	}
	if m.reg != nil {
		return m.logViewView()
	}
	if m.inTimeline {
		return m.timelineView()
	}
	if m.inReport {
		return m.reportView()
	}
	if m.inHistory {
		if m.historyDetail {
			return m.runDetailView()
		}
		return m.historyView()
	}
	if m.detail {
		return m.agentDetailView()
	}

	head := Header(m.rows, m.now, m.width, "live · every "+m.every.String()) + "\n" +
		tabBar("dashboard", m.width) + "\n" +
		m.statusPhrase(m.width) + "\n" +
		m.alertBand(m.width)

	if m.isFirstRun() {
		footer := Footer(m.width, "?", "help", "q", "quit")
		return clipHeight(head+"\n"+m.welcomeView(m.width), m.screenHeight()-lipgloss.Height(footer)) + footer
	}

	sel := min(max(0, m.table.Cursor()), len(m.rows)-1)
	height := m.screenHeight()
	offs := offRowLine(m.offRows, m.width) + remoteLines(m.now, m.width)

	if m.compact {
		footer := Footer(m.width, "↑↓", "select", "enter", "detail", "?", "help", "q", "quit")
		var body string
		if c := corralFor(m.rows, m.animation(), sel, m.now, m.width); m.showMascots && c != "" {
			corral := c + "\n\n" + compactTable(m.rows, sel, m.width, m.now)
			if lipgloss.Height(head+corral+offs+footer) <= height {
				body = corral
			} else {
				body = compactTable(m.rows, sel, m.width, m.now)
			}
		} else {
			body = compactTable(m.rows, sel, m.width, m.now)
		}
		blankLine := ""
		if lipgloss.Height(head+"\n"+body+offs+footer) <= height {
			blankLine = "\n"
		}
		return clipHeight(head+blankLine+body+offs, height-lipgloss.Height(footer)) + footer
	}

	cardsFooter := Footer(m.width, "←→", "select", "enter", "detail", "?", "help", "q", "quit")
	bodies := []string{
		Cards(m.rows, m.animation(), sel, m.width, m.now) + "\n",
		cards(m.rows, m.animation(), sel, m.width, m.now, false) + "\n",
		MiniCards(m.rows, sel, m.width, m.now) + "\n",
	}
	if !m.showMascots {
		bodies = bodies[1:]
	}
	for _, c := range bodies {
		blankLine := ""
		if lipgloss.Height(head+"\n"+c+offs+cardsFooter) <= height {
			blankLine = "\n"
		}
		if lipgloss.Height(head+blankLine+c+offs+cardsFooter) <= height {
			return clipHeight(head+blankLine+c+offs, height-lipgloss.Height(cardsFooter)) + cardsFooter
		}
	}

	// If not even the mini cards fit (very small terminal), the table.
	body := compactTable(m.rows, sel, m.width, m.now)
	tableFooter := Footer(m.width, "↑↓", "select", "enter", "detail", "?", "help", "q", "quit")
	smallNote := lipgloss.NewStyle().Foreground(cDim).Render("small terminal: table")
	if lipgloss.Width(tableFooter)+2+lipgloss.Width(smallNote) <= m.width {
		tableFooter += "  " + smallNote
	} else {
		tableFooter += "\n" + smallNote
	}
	blankLine := ""
	if lipgloss.Height(head+"\n"+body+offs+tableFooter) <= height {
		blankLine = "\n"
	}
	return clipHeight(head+blankLine+body+offs, height-lipgloss.Height(tableFooter)) + tableFooter
}

// offRowLine: "⏻ off: opencode (out of quota)", in gray.
func offRowLine(fs []state.Row, width int) string {
	if len(fs) == 0 {
		return ""
	}
	var parts []string
	for _, f := range fs {
		parts = append(parts, f.Agent+" ("+f.Status.String()+")")
	}
	lbl := "  ⏻ off: "
	return lipgloss.NewStyle().Foreground(cDim).Render(truncate(lbl+strings.Join(parts, " · "), width)) + "\n"
}

// clipHeight keeps at most n lines of s (last resort when not even the
// shortest view fits): better to lose the end of the detail than break the screen.
func clipHeight(s string, n int) string {
	if n < 0 {
		n = 0
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + "\n"
}

type statusPart struct {
	text  string
	color lipgloss.TerminalColor
}

// statusPhrase builds the summary line under the header: which agents are
// working and for how long, which ones have a problem and, if codex reports
// spend, how many days its credits will last.
func (m Model) statusPhrase(width int) string {
	var parts []statusPart

	// 1. Agents working / orchestrating
	var working []string
	orchestrating := false
	for _, f := range m.rows {
		if f.Status == state.Orchestrating {
			orchestrating = true
		} else if f.Status == state.Working {
			t := ""
			if !f.Since.IsZero() {
				t = " for " + readers.Ago(m.now.Sub(f.Since))
			}
			working = append(working, fmt.Sprintf("%s working%s", f.Agent, t))
		}
	}

	if orchestrating {
		parts = append(parts, statusPart{text: "claude orchestrating", color: cDim})
	}
	if len(working) > 0 {
		for _, work := range working {
			parts = append(parts, statusPart{text: work, color: cDim})
		}
	} else {
		parts = append(parts, statusPart{text: "no agent working", color: cDim})
	}

	// 2. Agents with problems
	for _, f := range m.rows {
		switch f.Status {
		case state.OutOfQuota:
			parts = append(parts, statusPart{text: f.Agent + " out of quota", color: cAmber})
		case state.NoPermission:
			parts = append(parts, statusPart{text: f.Agent + " no permission", color: cAmber})
		case state.Stuck:
			parts = append(parts, statusPart{text: f.Agent + " stuck", color: cRed})
		case state.Failed:
			parts = append(parts, statusPart{text: f.Agent + " failed", color: cRed})
		}
	}

	// 3. Codex credits
	for _, f := range m.rows {
		if f.Agent == "codex" && f.Quota.Spend != "" {
			for _, p := range strings.Split(f.Quota.Spend, " · ") {
				p = strings.TrimSpace(p)
				if strings.Contains(p, "days") {
					col := cDim
					if f.Quota.SpendWarning {
						col = cAmber
					}
					parts = append(parts, statusPart{text: "codex: credits for " + p, color: col})
					break
				}
			}
			break
		}
	}

	return renderStatusPhrase(parts, width)
}

func renderStatusPhrase(parts []statusPart, width int) string {
	if len(parts) == 0 {
		return ""
	}
	dimS := lipgloss.NewStyle().Foreground(cDim)

	type item struct {
		text  string
		color lipgloss.TerminalColor
	}
	var items []item
	items = append(items, item{text: "  ", color: cDim})
	for i, p := range parts {
		if i > 0 {
			items = append(items, item{text: " · ", color: cDim})
		}
		items = append(items, item{text: p.text, color: p.color})
	}

	totalWidth := 0
	for _, it := range items {
		totalWidth += lipgloss.Width(it.text)
	}

	if totalWidth <= width {
		var b strings.Builder
		for _, it := range items {
			b.WriteString(lipgloss.NewStyle().Foreground(it.color).Render(it.text))
		}
		return b.String()
	}

	avail := width - 1
	var b strings.Builder
	for _, it := range items {
		w := lipgloss.Width(it.text)
		if avail <= 0 {
			break
		}
		if w <= avail {
			b.WriteString(lipgloss.NewStyle().Foreground(it.color).Render(it.text))
			avail -= w
		} else {
			r := []rune(it.text)
			var sub []rune
			curW := 0
			for _, ru := range r {
				rw := lipgloss.Width(string(ru))
				if curW+rw > avail {
					break
				}
				sub = append(sub, ru)
				curW += rw
			}
			if len(sub) > 0 {
				b.WriteString(lipgloss.NewStyle().Foreground(it.color).Render(string(sub)))
			}
			avail = 0
			break
		}
	}
	b.WriteString(dimS.Render("…"))
	return b.String()
}

// Capture draws the main screen once at the given width without opening the
// UI: for design reviews and tests.
func Capture(ls []readers.Reader, width int) string {
	return CaptureView(ls, width, "")
}

// CaptureView draws one specific view once: "" (cards), "table", "detail"
// (agent detail screen), "history", "history-detail" (run detail screen),
// "report", "timeline" (also "statusline", the name main passes) or "help".
func CaptureView(ls []readers.Reader, width int, vw string) string {
	m := New(ls, 2*time.Second)
	m.width, m.height = width, 40
	m.table.SetColumns(columns(width))
	switch vw {
	case "table":
		m.compact = true
	case "detail", "agent-detail":
		m.detail = true
	case "history":
		m.inHistory = true
	case "history-detail", "run-detail":
		m.inHistory, m.historyDetail = true, true
	case "report":
		m.inReport = true
	case "timeline", "statusline":
		m.inTimeline = true
	case "help":
		m.inHelp = true
	}
	return m.View()
}

// Text is the status as plain text, one line per agent, no colors.
func Text(ls []readers.Reader) string {
	now := nowFn()
	var b strings.Builder
	fmt.Fprintf(&b, "%-9s %-13s %-24s %-8s %s\n", "AGENT", "STATUS", "MODEL", "TIME", "QUOTA | TASK")
	for _, l := range ls {
		f := l.Read()
		fmt.Fprintf(&b, "%-9s %-13s %-24s %-8s %s | %s\n", f.Agent, f.Status, dash(f.Model),
			duration(f.Since, now), quota(f.Quota), dash(f.Task))
	}
	return b.String()
}

// HistoryText prints the history as plain text: a summary plus one line per run.
func HistoryText(limit int) string {
	runs := history.Read("", limit)
	now := nowFn()
	var b strings.Builder
	b.WriteString(history.Summary(runs, now))
	b.WriteString("\n")
	for _, c := range runs {
		date := c.Start.Local().Format("02-Jan 15:04")
		fmt.Fprintf(&b, "%-12s  %-9s  %-24s  %-14s  %-8s  %-10s  %-8s  %s\n",
			date, c.Agent, dash(c.Model), dash(string(c.Status)),
			runDuration(c), formatTokens(c.Tokens), formatCost(c.Cost), dash(c.Task))
	}
	return b.String()
}

func mark(e state.Status) string {
	switch e {
	case state.Working, state.Orchestrating:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("●")
	case state.Stuck, state.OutOfQuota, state.NoPermission, state.Failed:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✖")
	default:
		return "○"
	}
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func duration(from, now time.Time) string {
	if from.IsZero() {
		return "—"
	}
	return readers.Ago(now.Sub(from))
}

func runDuration(c history.Run) string {
	if c.Status == runRunning || c.End.IsZero() {
		return "—"
	}
	return readers.Ago(c.End.Sub(c.Start))
}

func formatTokens(n int64) string {
	if n <= 0 {
		return "—"
	}
	return formatThousands(n)
}

func formatThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	l := len(s)
	for i, r := range s {
		if i > 0 && (l-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func formatCost(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func quota(c state.Quota) string {
	if c.Exact {
		s := fmt.Sprintf("%.0f%%", c.UsedPct)
		if !c.ResetsAt.IsZero() {
			s += " · resets " + c.ResetsAt.Format("02-Jan 15:04")
		}
		if c.Credits != "" {
			s += " · " + c.Credits + " cr"
		}
		if c.Summary != "" {
			s += " · " + c.Summary
		}
		return s
	}
	return dash(c.Summary)
}
