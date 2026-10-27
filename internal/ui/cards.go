package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/AlbertoVasquezR/panal/internal/mascots"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

const (
	minWidth     = 28 // minimum card width
	mascotHeight = mascots.Height / 2
)

// statusColor: green if working or done, blue if orchestrating, amber if out
// of quota or permission, red if failed or stuck, gray at rest.
func statusColor(e state.Status) lipgloss.TerminalColor {
	switch e {
	case state.Working, state.Done:
		return cGreen
	case state.Orchestrating:
		return cBlue
	case state.OutOfQuota, state.NoPermission:
		return cAmber
	case state.Failed, state.Stuck:
		return cRed
	default:
		return cDim
	}
}

func icon(e state.Status) string {
	switch e {
	case state.Working, state.Orchestrating:
		return "●"
	case state.OutOfQuota:
		return "◐"
	case state.NoPermission:
		return "⊘"
	case state.Failed, state.Stuck:
		return "✖"
	case state.Done:
		return "✔"
	default:
		return "○"
	}
}

// statusBadge draws the agent's status as a badge (" ● working ") on the
// status color with dark text. "idle" and "no data" are shown as dim text
// with no background. The icon is always included.
func statusBadge(e state.Status) string {
	txt := icon(e) + " " + e.String()
	if e == state.Idle || e == state.NoData {
		return lipgloss.NewStyle().Foreground(cDim).Render(txt)
	}
	return lipgloss.NewStyle().Bold(true).Foreground(cTextOnColor).
		Background(statusColor(e)).Padding(0, 1).Render(txt)
}

// cardBand draws the card's top band: the agent name in bold uppercase, dark
// text on the agent's color, as wide as the card. The status badge goes on the
// right if it fits; otherwise it is returned on a line below.
// unseen puts "✦" before the name: the run ended and you haven't opened its detail.
func cardBand(agent string, e state.Status, width int, unseen bool) []string {
	ac := agentColor(agent)
	nm := " " + strings.ToUpper(agent)
	if unseen {
		nm = " ✦" + nm
	}
	nameRender := lipgloss.NewStyle().Bold(true).Foreground(cTextOnColor).
		Background(ac).Render(nm)

	badge := statusBadge(e)
	right := badge
	if e == state.Idle || e == state.NoData {
		right = lipgloss.NewStyle().Foreground(cTextOnColor).Background(ac).Render(icon(e) + " " + e.String() + " ")
	}

	wName := lipgloss.Width(nm)
	wRight := lipgloss.Width(right)

	if wName+wRight <= width {
		gap := width - wName - wRight
		band := nameRender + lipgloss.NewStyle().Background(ac).Render(strings.Repeat(" ", gap)) + right
		return []string{band}
	}

	band := lipgloss.NewStyle().Bold(true).Foreground(cTextOnColor).
		Background(ac).Width(width).Render(nm)
	return []string{band, badge}
}

// Header: the brand, the time and a count of statuses as chips.
func Header(rows []state.Row, now time.Time, width int, vw string) string {
	mark := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).
		Background(cBrand).Padding(0, 1).Render("◆ Panal")
	sub := lipgloss.NewStyle().Foreground(cDim).Render(" " + vw + " · " + now.Format("15:04:05"))

	count := map[state.Status]int{}
	for _, f := range rows {
		count[f.Status]++
	}
	chip := func(n int, txt string, c lipgloss.TerminalColor) string {
		if n == 0 {
			return ""
		}
		return lipgloss.NewStyle().Foreground(c).Render(fmt.Sprintf(" %s %d %s", "■", n, txt))
	}
	actives := count[state.Working] + count[state.Orchestrating]
	chips := chip(actives, "active", cGreen) +
		chip(count[state.OutOfQuota]+count[state.NoPermission], "no quota/permission", cAmber) +
		chip(count[state.Failed]+count[state.Stuck], "failing", cRed) +
		chip(count[state.Idle]+count[state.Done]+count[state.NoData], "at rest", cDim)

	// If they don't fit with words, they go compact with each status glyph
	// ("● 1  ◐ 1  ○ 2") on the same line; only if that doesn't fit either, below.
	mini := func(n int, g string, c lipgloss.TerminalColor) string {
		if n == 0 {
			return ""
		}
		return lipgloss.NewStyle().Foreground(c).Render(fmt.Sprintf("  %s %d", g, n))
	}
	compacts := mini(actives, "●", cGreen) +
		mini(count[state.OutOfQuota]+count[state.NoPermission], "◐", cAmber) +
		mini(count[state.Failed]+count[state.Stuck], "✖", cRed) +
		mini(count[state.Idle]+count[state.Done]+count[state.NoData], "○", cDim)

	left := mark + sub
	for _, c := range []string{chips, compacts} {
		if gap := width - lipgloss.Width(left) - lipgloss.Width(c); gap >= 1 {
			return left + strings.Repeat(" ", gap) + c
		}
	}
	return left + "\n" + compacts
}

// barLabel turns a quota bar name from the readers ("5h", "sem", "mes") into
// its label. The names are matched as the readers write them.
func barLabel(name string) string {
	switch name {
	case "sem", "week":
		return "week"
	case "5h":
		return "5 h"
	case "mes", "month":
		return "month"
	default:
		if strings.HasSuffix(name, "h") {
			return strings.TrimSuffix(name, "h") + " h"
		}
		return name
	}
}

// Bar draws "5 h    ███████░░░  39%  resets 12:40" at the given width (the
// reset time if it is less than a day away, otherwise the date), in green,
// amber or red by usage. With ExhaustsAt set it adds "⚠ ~hh:mm" (or "⚠") in amber.
func Bar(b state.Bar, width int, now time.Time) string {
	pct := math.Max(0, math.Min(100, b.UsedPct))
	c := cGreen
	switch {
	case pct >= 85:
		c = cRed
	case pct >= 60:
		c = cAmber
	}
	if !b.ExhaustsAt.IsZero() && c == cGreen {
		c = cAmber
	}
	label := fmt.Sprintf("%-6s", barLabel(b.Name))
	figure := fmt.Sprintf("%3.0f%%", pct)

	var longStr, shortStr string
	if b.AlreadyReset {
		longStr, shortStr = " already reset", " ↻ now"
	} else if !b.ResetsAt.IsZero() {
		t := b.ResetsAt.Local()
		if b.ResetsAt.Sub(now) < 24*time.Hour {
			longStr = " resets " + t.Format("15:04")
			// Space after "↻": many fonts (Cascadia, Windows Terminal's) lack it
			// and the fallback font draws it wider than a cell, so it overlaps
			// the number that follows.
			shortStr = " ↻ " + t.Format("15:04")
		} else {
			date := t.Format("02 Jan")
			longStr = " resets " + date
			shortStr = " ↻ " + date
		}
	}

	var runsOutFull, runsOutShort string
	if !b.ExhaustsAt.IsZero() {
		runsOutFull = " ⚠ ~" + b.ExhaustsAt.Local().Format("15:04")
		runsOutShort = " ⚠"
	}

	fixedBase := len(label) + 1 + 1 + len(figure)
	avail := width - fixedBase - 4
	if avail < 0 {
		avail = 0
	}

	var runsOutText, reset string
	if runsOutFull != "" && longStr != "" {
		wFull := lipgloss.Width(runsOutFull)
		wShort := lipgloss.Width(runsOutShort)
		wRLong := lipgloss.Width(longStr)
		wRShort := lipgloss.Width(shortStr)

		switch {
		case avail >= wFull+wRLong:
			runsOutText = runsOutFull
			reset = longStr
		case avail >= wFull+wRShort:
			runsOutText = runsOutFull
			reset = shortStr
		case avail >= wFull:
			runsOutText = runsOutFull
			reset = ""
		case avail >= wShort+wRLong:
			runsOutText = runsOutShort
			reset = longStr
		case avail >= wShort+wRShort:
			runsOutText = runsOutShort
			reset = shortStr
		case avail >= wShort:
			runsOutText = runsOutShort
			reset = ""
		}
	} else if runsOutFull != "" {
		wFull := lipgloss.Width(runsOutFull)
		wShort := lipgloss.Width(runsOutShort)
		switch {
		case avail >= wFull:
			runsOutText = runsOutFull
		case avail >= wShort:
			runsOutText = runsOutShort
		}
	} else if longStr != "" {
		wRLong := lipgloss.Width(longStr)
		wRShort := lipgloss.Width(shortStr)
		switch {
		case avail >= wRLong:
			reset = longStr
		case avail >= wRShort:
			reset = shortStr
		}
	}

	extra := lipgloss.Width(reset) + lipgloss.Width(runsOutText)
	length := width - fixedBase - extra
	if length < 4 {
		length = 4
	}
	filled := int(math.Round(pct / 100 * float64(length)))
	bar := lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(cEmpty).Render(strings.Repeat("░", length-filled))
	dimS := lipgloss.NewStyle().Foreground(cDim)
	runsOutStr := ""
	if runsOutText != "" {
		runsOutStr = lipgloss.NewStyle().Foreground(cAmber).Render(runsOutText)
	}
	return dimS.Render(label) + " " + bar + " " + lipgloss.NewStyle().Foreground(c).Render(figure) + runsOutStr + dimS.Render(reset)
}

// Card for an agent: animated mascot, name, status, model, task, time and
// quota bars.
// height sets the total height (border included) to even out cards; 0 = natural.
func Card(f state.Row, a Animation, width int, selected bool, now time.Time, height int) string {
	return card(f, a, width, selected, now, height, true)
}

// card is Card with an optional mascot: without it the card is a few lines
// shorter, so the details fit in short terminals.
func card(f state.Row, a Animation, width int, selected bool, now time.Time, height int, withMascot bool) string {
	ac := agentColor(f.Agent)
	inner := width - 4 // border + padding
	dimS := lipgloss.NewStyle().Foreground(cDim)
	center := lipgloss.NewStyle().Width(inner).Align(lipgloss.Center)

	mode, n, fx := a.mascot(f, now)
	var lines []string

	// 1. Title band at the very top with the name and the status badge
	lines = append(lines, cardBand(f.Agent, f.Status, inner, f.Unseen)...)

	// 2. Mascot below the band
	if s, ok := mascots.All[f.Agent]; ok && withMascot {
		lines = append(lines, center.Render(s.RenderWith(mode, n, fx)))
	}
	if f.Review != "" {
		review := lipgloss.NewStyle().Foreground(cRed).Render(truncate("⚠ "+f.Review, inner))
		lines = append(lines, center.Render(review))
	}
	// The free line under the mascot: it talks there during a reaction, so the
	// card keeps its height.
	// With no reaction and while working, it tells what it is doing.
	bubble := ""
	if g := a.bubble(f.Agent); g != "" && withMascot {
		bubble = center.Render(lipgloss.NewStyle().Foreground(ac).Italic(true).Render(truncate(g, inner)))
	} else if r := repeatText(f); r != "" && withMascot {
		bubble = center.Render(lipgloss.NewStyle().Foreground(cAmber).Render(truncate(r, inner)))
	} else if t := activityText(f, now); t != "" && withMascot {
		bubble = center.Render(dimS.Render(truncate("› "+t, inner)))
	} else if f.Tests != "" && withMascot && f.Status != state.Working {
		c := cGreen
		if !f.TestsOK {
			c = cRed
		}
		bubble = center.Render(lipgloss.NewStyle().Foreground(c).Render(truncate(f.Tests, inner)))
	} else if f.Streak >= minStreak && withMascot && mode != mascots.Sleeping {
		bubble = center.Render(lipgloss.NewStyle().Foreground(cAmber).Render("★") + dimS.Render(fmt.Sprintf(" %d in a row, no failures", f.Streak)))
	}
	lines = append(lines, bubble)

	if f.Status == state.NoData {
		lines = append(lines, dimS.Render(truncate("no runs from "+f.Agent+" yet", inner)))
	} else {
		// Plain text is truncated first and colored afterwards: truncating
		// colored text can split a color code in half.
		row := func(lbl, val string) string {
			if val == "" {
				val = "—"
			}
			return dimS.Render(lbl) + " " + truncate(val, inner-lipgloss.Width(lbl)-1)
		}
		lines = append(lines, row("model", shortModel(f.Model)))
		timeStr := ""
		if f.Status == state.Working || f.Status == state.Orchestrating {
			if !f.Since.IsZero() {
				timeStr = readers.Ago(now.Sub(f.Since))
			}
		} else if !f.End.IsZero() {
			// The status already says "done": here only when and how long.
			ago := shortAgo(now.Sub(f.End))
			switch f.Status {
			case state.OutOfQuota:
				timeStr = "out of quota " + ago + " ago"
			case state.Failed:
				timeStr = "failed " + ago + " ago"
			case state.NoPermission:
				timeStr = "no permission " + ago + " ago"
			default:
				timeStr = ago + " ago"
				if !f.Start.IsZero() && f.End.After(f.Start) {
					timeStr += " · took " + readers.Ago(f.End.Sub(f.Start))
				}
			}
		} else if !f.Since.IsZero() {
			timeStr = readers.Ago(now.Sub(f.Since))
		}
		lines = append(lines, row("time ", timeStr))

		proj := shortProject(f.Dir)
		ttl := taskTitle(f.FullTask, f.Task)
		if ttl == "" {
			ttl = "—"
		}
		taskAvail := inner - lipgloss.Width("task ") - 1
		var taskVal string
		if proj == "" {
			taskVal = truncate(ttl, taskAvail)
		} else {
			sep := " · "
			fixed := lipgloss.Width(proj) + lipgloss.Width(sep)
			if fixed+1 <= taskAvail {
				taskVal = dimS.Render(proj+sep) + truncate(ttl, taskAvail-fixed)
			} else {
				taskVal = dimS.Render(truncate(proj, taskAvail))
			}
		}
		lines = append(lines, dimS.Render("task ")+" "+taskVal)
		lines = append(lines, "")

		switch {
		case len(f.Quota.Bars) > 0:
			for _, b := range f.Quota.Bars {
				lines = append(lines, Bar(b, inner, now))
			}
			if f.Quota.Credits != "" {
				cr := f.Quota.Credits
				if f.Quota.Extra != "" {
					cr += " · " + f.Quota.Extra
				}
				lines = append(lines, row("credits", cr))
			}
			if f.Quota.Spend != "" {
				c := cDim
				if f.Quota.SpendWarning {
					c = cAmber
				}
				lines = append(lines, lipgloss.NewStyle().Foreground(c).Render(truncate(f.Quota.Spend, inner)))
			}
			// Credits spent this session (the codex reader writes "-N cr this session").
			if strings.Contains(f.Quota.Summary, "this session") {
				lines = append(lines, lipgloss.NewStyle().Foreground(cAmber).Render(truncate(f.Quota.Summary, inner)))
			}
			if l := contextLine(f, inner); l != "" {
				lines = append(lines, l)
			}
			if v := f.Quota.SeenAt; !f.Quota.Live && !v.IsZero() && now.Sub(v) >= staleQuota {
				lines = append(lines, dimS.Render(truncate("≈ quota seen "+readers.Ago(now.Sub(v))+" ago", inner)))
			}
		case f.Quota.Summary != "":
			lines = append(lines, row("quota", f.Quota.Summary))
		default:
			lines = append(lines, dimS.Render("quota —"))
		}
	}

	border := lipgloss.RoundedBorder()
	var borderColor lipgloss.TerminalColor = cBorder
	if selected {
		border = lipgloss.ThickBorder()
		borderColor = ac
	}
	st := lipgloss.NewStyle().Border(border).BorderForeground(borderColor).Padding(0, 1).Width(inner + 2)
	if height > 2 {
		st = st.Height(height - 2)
	}
	return st.Render(strings.Join(lines, "\n"))
}

// truncate cuts plain text to n columns, ending in "…".
func truncate(s string, n int) string {
	if n < 1 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// Cards lays out one card per agent: in one row if they fit, otherwise in a
// two-column grid (a single column if even that doesn't fit). All the same
// height.
func Cards(rows []state.Row, a Animation, sel, width int, now time.Time) string {
	return cards(rows, a, sel, width, now, true)
}

// cardsPerRow: all cards in one row when they fit; five or more drop to
// three across before two, so a fifth card does not force a 2+2+1 stack.
// Four cards keep the old 4-across / 2×2 / one-column steps.
func cardsPerRow(n, width int) int {
	if n <= 0 {
		return 1
	}
	if width >= n*minWidth {
		return n
	}
	if n >= 5 && width >= 3*minWidth {
		return 3
	}
	if width >= 2*minWidth {
		return 2
	}
	return 1
}

func cards(rows []state.Row, a Animation, sel, width int, now time.Time, withMascot bool) string {
	if len(rows) == 0 {
		return ""
	}
	perRow := cardsPerRow(len(rows), width)
	w := min(max(width/perRow, minWidth), width)
	// Two passes: measure the tallest one and draw them all at that height,
	// so the borders line up.
	height := 0
	for i, f := range rows {
		if h := lipgloss.Height(card(f, Animation{}, w, i == sel, now, 0, withMascot)); h > height {
			height = h
		}
	}
	var cards []string
	for i, f := range rows {
		// Per-mascot offset so they don't all move at once.
		cards = append(cards, card(f, a.offsetBy(i*3), w, i == sel, now, height, withMascot))
	}
	var rowLines []string
	for i := 0; i < len(cards); i += perRow {
		end := i + perRow
		if end > len(cards) {
			end = len(cards)
		}
		rowLines = append(rowLines, lipgloss.JoinHorizontal(lipgloss.Top, cards[i:end]...))
	}
	return strings.Join(rowLines, "\n")
}

// MiniCards draws compact 5-line cards (no mascot): name and status, time and
// task, and the most used quota bar.
func MiniCards(rows []state.Row, sel, width int, now time.Time) string {
	if len(rows) == 0 {
		return ""
	}
	perRow := cardsPerRow(len(rows), width)
	w := min(max(width/perRow, minWidth), width)
	var cards []string
	for i, f := range rows {
		cards = append(cards, miniCard(f, w, i == sel, now))
	}
	var rowLines []string
	for i := 0; i < len(cards); i += perRow {
		end := i + perRow
		if end > len(cards) {
			end = len(cards)
		}
		rowLines = append(rowLines, lipgloss.JoinHorizontal(lipgloss.Top, cards[i:end]...))
	}
	return strings.Join(rowLines, "\n")
}

func miniCard(f state.Row, width int, selected bool, now time.Time) string {
	ac := agentColor(f.Agent)
	inner := width - 4
	dimS := lipgloss.NewStyle().Foreground(cDim)

	// Line 1: name and status in the band
	band := cardBand(f.Agent, f.Status, inner, f.Unseen)
	line1 := band[0]

	// Line 2: time and task
	timeStr := rowTime(f, now)
	proj := shortProject(f.Dir)
	ttl := taskTitle(f.FullTask, f.Task)
	task := ttl
	if proj != "" {
		task = proj + " · " + ttl
	}
	timeText := timeStr
	if task != "" && task != "—" {
		timeText += " · " + task
	}
	if f.Review != "" {
		timeText = "⚠ " + timeText
	}
	line2 := dimS.Render(truncate(timeText, inner))

	// Line 3: the most used quota bar
	var mostUsed *state.Bar
	for i := range f.Quota.Bars {
		b := &f.Quota.Bars[i]
		if mostUsed == nil || b.UsedPct > mostUsed.UsedPct {
			mostUsed = b
		}
	}
	var line3 string
	if mostUsed != nil {
		line3 = Bar(*mostUsed, inner, now)
	} else if f.Quota.Spend != "" {
		line3 = dimS.Render(truncate(f.Quota.Spend, inner))
	} else if f.Quota.Summary != "" {
		line3 = dimS.Render(truncate(f.Quota.Summary, inner))
	} else {
		line3 = dimS.Render("quota —")
	}

	border := lipgloss.RoundedBorder()
	var borderColor lipgloss.TerminalColor = cBorder
	if selected {
		border = lipgloss.ThickBorder()
		borderColor = ac
	}
	st := lipgloss.NewStyle().Border(border).BorderForeground(borderColor).Padding(0, 1).Width(inner + 2)
	return st.Render(strings.Join([]string{line1, line2, line3}, "\n"))
}

// Footer with the keys as "keycaps". If they don't fit the width they wrap to
// another line instead of letting the terminal split one in half.
func Footer(width int, keys ...string) string {
	key := lipgloss.NewStyle().Bold(true).Foreground(cKeyText).Background(cKeyBg).Padding(0, 1)
	txt := lipgloss.NewStyle().Foreground(cDim)
	var rowLines []string
	current := ""
	for i := 0; i+1 < len(keys); i += 2 {
		p := key.Render(keys[i]) + txt.Render(" "+keys[i+1])
		switch {
		case current == "":
			current = p
		case lipgloss.Width(current)+2+lipgloss.Width(p) <= width:
			current += "  " + p
		default:
			rowLines = append(rowLines, current)
			current = p
		}
	}
	return strings.Join(append(rowLines, current), "\n")
}

// contextLine: "context 34% · $15.51 this session". The percentage turns
// amber from 60 % and red from 85 %: that's when Claude soon compacts and loses
// detail of the conversation.
func contextLine(f state.Row, width int) string {
	if !f.HasContext {
		return ""
	}
	dimS := lipgloss.NewStyle().Foreground(cDim)
	c := cGreen
	switch {
	case f.ContextPct >= 85:
		c = cRed
	case f.ContextPct >= 60:
		c = cAmber
	}
	pct := fmt.Sprintf("%.0f%%", f.ContextPct)
	rest := ""
	if f.SessionCost != "" {
		for _, r := range []string{" · " + f.SessionCost + " this session", " · " + f.SessionCost} {
			if 8+len(pct)+lipgloss.Width(r) <= width {
				rest = r
				break
			}
		}
	}
	return dimS.Render("context ") + lipgloss.NewStyle().Foreground(c).Render(pct) + dimS.Render(rest)
}

// After how long an unconfirmed quota is marked as stale: the agent may have
// been used elsewhere or its quota reset by hand.
const staleQuota = 15 * time.Minute
