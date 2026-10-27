// panal: a read-only terminal dashboard of the agents Claude Code
// orchestrates (opencode, codex, agy, cursor); `panal delegate`, which hands them
// tasks; `panal route`, which shows which agent the router would pick; and
// `panal feedback`, which rates a run so the router learns; and `panal
// models`, the models each CLI can use. See README.md and docs/.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AlbertoVasquezR/panal/internal/alerts"
	"github.com/AlbertoVasquezR/panal/internal/changes"
	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/delegate"
	"github.com/AlbertoVasquezR/panal/internal/feedback"
	"github.com/AlbertoVasquezR/panal/internal/forecast"
	"github.com/AlbertoVasquezR/panal/internal/history"
	"github.com/AlbertoVasquezR/panal/internal/live"
	"github.com/AlbertoVasquezR/panal/internal/models"
	"github.com/AlbertoVasquezR/panal/internal/pet"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/remote"
	"github.com/AlbertoVasquezR/panal/internal/report"
	"github.com/AlbertoVasquezR/panal/internal/router"
	"github.com/AlbertoVasquezR/panal/internal/runs"
	"github.com/AlbertoVasquezR/panal/internal/state"
	"github.com/AlbertoVasquezR/panal/internal/ui"
)

// runBroke checks with git whether the run broke its task. It skips claude's
// runs and those without a dir.
func runBroke(c history.Run) bool {
	if c.Agent == "claude" || c.Dir == "" {
		return false
	}
	return changes.Check(ui.RunChanges(c), time.Now()).HasViolations()
}

// liveQuota: whether to ask the CLIs for their quota. Yes by default; "no" in
// panal.conf (live_quota) or in PANAL_LIVE_QUOTA turns it off.
func liveQuota(conf string) bool {
	v := os.Getenv("PANAL_LIVE_QUOTA")
	if v == "" {
		v = conf
	}
	return !config.IsOff(v)
}

func screenName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// formatName maps -format values: text is the default, which report.Export
// takes as "".
func formatName(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text", "txt":
		return ""
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// probeLive asks codex app-server (and the other CLIs) for their quota once
// (for -doctor) and leaves the reading wired to the reader.
func probeLive(ls []readers.Reader) []string {
	cv := live.NewCodex()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	go cv.Start(ctx)
	for ctx.Err() == nil {
		if l := cv.Latest(); l.HasData || l.Error != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var out []string
	if l := cv.Latest(); l.HasData {
		readers.WithCodexLive(ls, cv)
		txt := fmt.Sprintf("✔ codex: answered in %.1f s · plan %s", time.Since(start).Seconds(), l.Plan)
		if l.Resets >= 0 {
			txt += fmt.Sprintf(" · saved resets: %d", l.Resets)
		}
		out = append(out, txt)
	} else {
		out = append(out, "○ codex: "+l.Error)
	}

	av := live.NewAgy()
	start = time.Now()
	ctxA, cancelA := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancelA()
	go av.Start(ctxA)
	for ctxA.Err() == nil {
		if l := av.LatestAgy(); l.HasData || l.Error != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cl := live.NewClaude()
	startC := time.Now()
	ctxC, cancelC := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancelC()
	go cl.Start(ctxC)
	for ctxC.Err() == nil {
		if l := cl.LatestClaude(); l.HasData || l.Error != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if l := cl.LatestClaude(); l.HasData {
		readers.WithClaudeLive(ls, cl)
		out = append(out, fmt.Sprintf("✔ claude: answered in %.1f s · plan %s", time.Since(startC).Seconds(), l.Plan))
	} else {
		out = append(out, "○ claude: "+l.Error)
	}

	oc := live.NewOpencode()
	startO := time.Now()
	ctxO, cancelO := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelO()
	go oc.Start(ctxO)
	for ctxO.Err() == nil {
		if l := oc.LatestOpencode(); l.HasData || l.Error != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if l := oc.LatestOpencode(); l.HasData {
		readers.WithOpencodeLive(ls, oc)
		out = append(out, fmt.Sprintf("✔ opencode: answered in %.1f s · plan Go", time.Since(startO).Seconds()))
	} else {
		out = append(out, "○ opencode: "+l.Error)
	}

	if l := av.LatestAgy(); l.HasData {
		readers.WithAgyLive(ls, av)
		var gs []string
		for _, g := range l.Groups {
			gs = append(gs, g.Name)
		}
		out = append(out, fmt.Sprintf("✔ agy: answered in %.1f s · groups: %s", time.Since(start).Seconds(), strings.Join(gs, ", ")))
	} else {
		out = append(out, "○ agy: "+l.Error)
	}
	out = append(out, "○ cursor: no usage query that spends nothing (cursor-agent lists models and status; /usage is interactive only)")
	return out
}

func fail(code int, a ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"panal:"}, a...)...)
	os.Exit(code)
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "delegate":
			os.Exit(delegate.Main(os.Args[2:], os.Stdout, os.Stderr))
		case "route":
			os.Exit(delegate.RouteMain(os.Args[2:], os.Stdout, os.Stderr))
		case "models":
			os.Exit(delegate.ModelsMain(os.Args[2:], os.Stdout, os.Stderr))
		case "feedback":
			os.Exit(feedback.Main(os.Args[2:], os.Stdout, os.Stderr))
		case "pet":
			os.Exit(pet.Main(os.Args[2:], os.Stdout, os.Stderr))
		}
	}
	every := flag.Duration("every", 2*time.Second, "how often the dashboard refreshes")
	once := flag.Bool("once", false, "print the status once, without the UI (for scripts and tests)")
	historyFlag := flag.Bool("history", false, "print the run history as plain text and exit")
	reportFlag := flag.Int("report", 0, "print a per-agent report of the last N days and exit")
	formatFlag := flag.String("format", "text", "with -report: text, md, csv or json (e.g. panal -report 7 -format md > week.md)")
	summaryFlag := flag.Bool("summary", false, "print today's summary as markdown and exit")
	mascotsFlag := flag.Bool("mascots", false, "draw the mascots with the current status and exit")
	preview := flag.Int("preview", 0, "draw one screen once at this width and exit (see -screen)")
	screen := flag.String("screen", "", "with -preview: table, detail, history, history-detail, report, timeline or help")
	off := flag.String("off", "", "agents you do not use, comma-separated: when idle they go on one line instead of a card. Without the option they are read from "+config.Path()+" (\"\" to see them all)")
	alertsFlag := flag.String("alerts", string(alerts.All), "alerts when a run finishes or fails: all (Windows notification and bell), bell or none")
	statuslineFlag := flag.Bool("statusline", false, "print the status on one line («claude ● · codex ✔ 100% · …») for Claude Code's status line or tmux, and exit")
	themeFlag := flag.String("theme", os.Getenv("PANAL_THEME"), "colors: auto, dark, light or contrast (or PANAL_THEME)")
	serveFlag := flag.String("serve", "", "serve this machine's status at that address (e.g. :8765) for another dashboard to read, without the UI; needs serve_token in panal.conf or PANAL_TOKEN")
	doctorFlag := flag.Bool("doctor", false, "show which sources were found, what was read from each agent and what is left to configure, and exit")
	noAnim := flag.Bool("no-animation", ui.NoAnimation || os.Getenv("PANAL_NO_ANIMATION") != "", "still mascots, no reactions (or PANAL_NO_ANIMATION=1)")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), `Usage: panal [flags]                          the dashboard (see the flags below)
       panal delegate -d DIR [flags] "task"  hand a task to codex, agy, opencode or cursor
       panal route [-r] "task"               which agent the router would pick, without running it
       panal route -stats                    what the router has learned
       panal models [-refresh]               the models each agent CLI can use, and which the router may pick
       panal feedback RUN good|bad [note]    rate a run so the router learns
       panal pet [-json|-stream]             a mascot for a small split pane (or JSON frames)

Each command has its own -h.

Flags:
`)
		flag.PrintDefaults()
	}
	flag.Parse()
	given := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { given[f.Name] = true })
	// Whatever the command line (or its variable) did not say, panal.conf says.
	conf := config.Read(config.Path())
	if !given["alerts"] && conf.Alerts != "" {
		*alertsFlag = conf.Alerts
	}
	if !given["theme"] && *themeFlag == "" {
		*themeFlag = conf.Theme
	}
	if !given["no-animation"] && !*noAnim && config.IsOff(conf.Animation) {
		*noAnim = true
	}
	if !given["every"] && conf.Interval != "" {
		if d, err := time.ParseDuration(conf.Interval); err == nil && d > 0 {
			*every = d
		} else {
			fmt.Fprintf(os.Stderr, "panal: in %s, «every = %s» is not a duration (e.g. 5s)\n", config.Path(), conf.Interval)
		}
	}
	ui.AlertMode = alerts.ParseMode(*alertsFlag)
	ui.OutboundAlerts = alerts.Outbound{Ntfy: conf.Ntfy, Webhook: conf.Webhook}
	if p, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSpace(conf.Price), "$"), 64); err == nil && p > 0 {
		ui.ClaudePrice = p
	}
	ui.NoAnimation = *noAnim
	if err := ui.ApplyTheme(config.Theme(*themeFlag)); err != nil {
		fail(2, err)
	}
	// -off wins; without the option, panal.conf says (and is re-read on its own).
	if given["off"] {
		ui.Disabled = map[string]bool{}
		for _, a := range strings.Split(*off, ",") {
			if a = strings.TrimSpace(a); a != "" {
				ui.Disabled[a] = true
			}
		}
	} else {
		ui.Conf = &config.Resolver{Path: config.Path()}
	}

	token := os.Getenv("PANAL_TOKEN")
	if token == "" {
		token = conf.ServeToken
	}
	if *serveFlag != "" {
		fmt.Fprintf(os.Stderr, "panal: serving this machine's status at %s%s (ctrl+c to stop)\n", *serveFlag, remote.StatusPath)
		if err := remote.Serve(context.Background(), *serveFlag, token, readers.All()); err != nil {
			fail(1, err)
		}
		return
	}

	if *doctorFlag {
		ls := readers.All()
		var liveLines []string
		if liveQuota(conf.LiveQuota) {
			liveLines = probeLive(ls)
		} else {
			liveLines = []string{"off (live_quota = no)"}
		}
		// The models cache is filled first if it is missing (listing only).
		mctx, mcancel := context.WithTimeout(context.Background(), 30*time.Second)
		cat := models.Ensure(mctx, models.Path(), models.Exec, config.LookPath, 20*time.Second, time.Now())
		mcancel()
		fmt.Print(ui.Diagnostics(ls, config.Path(), liveLines, models.DoctorLines(cat, time.Now())))
		return
	}

	if *statuslineFlag {
		fmt.Println(ui.StatusLine(readers.All()))
		return
	}

	if *preview > 0 {
		fmt.Println(ui.CaptureView(readers.All(), *preview, screenName(*screen)))
		return
	}

	if *mascotsFlag {
		ls := readers.All()
		rows := make([]state.Row, 0, len(ls))
		for _, l := range ls {
			rows = append(rows, l.Read())
		}
		fmt.Println(ui.Corral(rows, ui.Animation{}, -1, time.Now()))
		return
	}

	if *reportFlag > 0 {
		cs := history.New().Read("", 0)
		now := time.Now()
		// The router's section reuses the checks panal delegate cached, and
		// does not write them (a report only reads).
		checks := router.LoadChecks(filepath.Join(runs.Home(), router.ChecksFile))
		checks.Path = ""
		rs := router.Summarize(cs, now.AddDate(0, 0, -*reportFlag), checks, now)
		txt, err := report.Export(cs, *reportFlag, now, runBroke, formatName(*formatFlag), ui.ClaudePrice, &rs)
		if err != nil {
			fail(2, err)
		}
		fmt.Print(txt)
		return
	}

	if *summaryFlag {
		cs := history.New().Read("", 0)
		fmt.Print(report.Markdown(cs, time.Now(), runBroke))
		return
	}

	if *historyFlag {
		fmt.Print(ui.HistoryText(100))
		return
	}

	if *once {
		fmt.Print(ui.Text(readers.All()))
		return
	}

	ui.SamplesPath = forecast.Path()
	ls := readers.All()
	// Live quota: codex app-server is asked every minute (without spending
	// quota) while the dashboard is open. Turned off with live_quota = no.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if liveQuota(conf.LiveQuota) {
		cv := live.NewCodex()
		go cv.Start(ctx)
		readers.WithCodexLive(ls, cv)
		av := live.NewAgy()
		go av.Start(ctx)
		readers.WithAgyLive(ls, av)
		cl := live.NewClaude()
		go cl.Start(ctx)
		readers.WithClaudeLive(ls, cl)
		oc := live.NewOpencode()
		go oc.Start(ctx)
		readers.WithOpencodeLive(ls, oc)
		ui.LiveQuota = live.Combine(cv, av, cl, oc)
	}
	// The models each CLI can use, for the router: refreshed in the
	// background when the cache is older than a day. It never blocks the
	// UI, and its only write is panal's own ~/.panal/models.json.
	models.RefreshInBackground(ctx, models.Path(), models.Exec, config.LookPath, 30*time.Second, models.MaxAge, time.Now)
	if conf.Serve != "" {
		go func() {
			if err := remote.Serve(ctx, conf.Serve, token, readers.All()); err != nil {
				fmt.Fprintln(os.Stderr, "panal: could not serve the status:", err)
			}
		}()
	}
	var machines []remote.Machine
	for _, s := range conf.Machines {
		if mq, err := remote.ParseMachine(s); err == nil {
			machines = append(machines, mq)
		} else {
			fmt.Fprintln(os.Stderr, "panal:", err)
		}
	}
	if len(machines) > 0 {
		cr := remote.NewClient(machines)
		go cr.Start(ctx)
		ui.Remotes = cr
	}
	p := tea.NewProgram(ui.New(ls, *every), tea.WithAltScreen())
	_, err := p.Run()
	cancel()
	if err != nil {
		fail(1, err)
	}
}
