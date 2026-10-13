package delegate

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/history"
	"github.com/AlbertoVasquezR/panal/internal/models"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/report"
	"github.com/AlbertoVasquezR/panal/internal/router"
	"github.com/AlbertoVasquezR/panal/internal/runs"
	"github.com/AlbertoVasquezR/panal/internal/state"
)

// The router's side of panal delegate: -c auto, and `panal route`, which
// shows what -c auto would do without running anything.

// poolSpec picks the router's arms: PANAL_POOL, else pool = ... in the
// config, else the chain when there is one (and it is not "auto"). Empty
// means the default chain (every installed CLI).
func poolSpec(chain string, conf config.Conf) (spec, from string) {
	switch {
	case strings.TrimSpace(os.Getenv("PANAL_POOL")) != "":
		return os.Getenv("PANAL_POOL"), "PANAL_POOL"
	case strings.TrimSpace(conf.Pool) != "":
		return conf.Pool, "pool in " + config.Path()
	case strings.TrimSpace(chain) != "" && !config.IsAuto(chain):
		return chain, "the chain"
	}
	return "", "the installed CLIs"
}

// poolInfo is how the pool was made, for messages.
type poolInfo struct {
	From     string   // where it came from
	Notes    []string // warnings: unknown models in an explicit pool, a bad cap
	Suggest  []string // discovered arms an explicit pool doesn't have
	Auto     bool     // pool = auto: discovered arms
	Allowed  int      // with Auto: arms the rules allow before the cap
	Cap      int      // with Auto: the per-CLI cap
	Explicit bool     // the pool was written by hand (or given with -p)
}

// rulesOf reads the models / exclude / pool_max_per_cli keys.
func rulesOf(conf config.Conf) (models.Rules, []string) {
	r, err := models.ParseRules(conf.Models, conf.Exclude, conf.PoolMaxPerCLI)
	if err != nil {
		return r, []string{err.Error()}
	}
	return r, nil
}

// resolvePool turns the pool spec into links. "auto" is every discovered
// arm the models / exclude rules allow, at most pool_max_per_cli per CLI,
// ordered by the cost heuristic (internal/models), from the cache, which
// is refreshed only when it is missing. Any other spec is used exactly as
// written, and checked against the catalog when there is one (refresh
// says whether a missing cache may be filled for that).
func (rt routing) resolvePool(spec, from string, conf config.Conf, lookPath func(string) (string, error), refresh bool) ([]Link, poolInfo, error) {
	rules, notes := rulesOf(conf)
	info := poolInfo{From: from, Notes: notes}
	if config.IsAuto(spec) {
		var cat models.Catalog
		if rt.Catalog != nil {
			cat = rt.Catalog(true)
		}
		arms := rules.AutoPool(cat)
		if len(arms) == 0 {
			return nil, info, fmt.Errorf(`pool = auto: no discovered model is allowed (run "panal models" to see the catalog and your models / exclude rules)`)
		}
		info.Auto, info.Allowed, info.Cap = true, len(rules.Allowed(cat)), rules.Cap()
		var pool []Link
		for _, a := range arms {
			pool = append(pool, Link{CLI: a.CLI, Model: a.Model, Effort: a.Effort})
		}
		return pool, info, nil
	}
	pool, err := poolLinks(spec, lookPath)
	if err != nil {
		return nil, info, err
	}
	if strings.TrimSpace(spec) == "" || rt.Catalog == nil {
		return pool, info, nil
	}
	info.Explicit = true
	cat := rt.Catalog(refresh)
	var arms []string
	for _, l := range pool {
		arms = append(arms, l.String())
	}
	info.Notes = append(info.Notes, rules.Check(cat, arms)...)
	for _, a := range rules.Suggest(cat, arms) {
		info.Suggest = append(info.Suggest, a.String())
	}
	return pool, info, nil
}

// systemCatalog is the models cache (~/.panal/models.json); with refresh,
// the installed CLIs it has no entry for are asked first (15 s each, in
// parallel; listing only, nothing is spent).
func systemCatalog(refresh bool) models.Catalog {
	if !refresh {
		cat, _ := models.Load(models.Path())
		return cat
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return models.Ensure(ctx, models.Path(), models.Exec, config.LookPath, 15*time.Second, time.Now())
}

// poolLinks parses the pool, or makes the default one.
func poolLinks(spec string, lookPath func(string) (string, error)) ([]Link, error) {
	if strings.TrimSpace(spec) != "" {
		return ParseChain(spec)
	}
	if l := defaultChain(lookPath); len(l) > 0 {
		return l, nil
	}
	return nil, fmt.Errorf("none of %s is installed (or in PATH)", strings.Join(DefaultCLIs, ", "))
}

// routing is what the router needs from the system; tests fill it with fakes.
type routing struct {
	History  func() []history.Run   // past runs, newest first
	Checks   *router.Checks         // violation and test checks of past runs
	QuotaOut func() map[string]bool // CLIs out of quota right now
	Now      func() time.Time
	Rand     *rand.Rand // nil = from the clock
	External *router.External
	// Catalog is the discovered models (nil = none); refresh says whether a
	// missing cache may be filled first.
	Catalog func(refresh bool) models.Catalog
}

// systemRouting reads the real history (every run dir), caches checks in
// ~/.panal and asks panal's own quota readers (cheap file reads; nothing is
// launched and nothing is spent).
func systemRouting(conf config.Conf) routing {
	rt := routing{
		History:  func() []history.Run { return history.New().Read("", 0) },
		Checks:   router.LoadChecks(filepath.Join(runs.Home(), router.ChecksFile)),
		QuotaOut: quotaOutNow,
		Now:      time.Now,
		Catalog:  systemCatalog,
	}
	cmd := os.Getenv("PANAL_ROUTER")
	if strings.TrimSpace(cmd) == "" {
		cmd = conf.Router
	}
	if strings.TrimSpace(cmd) != "" {
		rt.External = &router.External{Command: cmd}
	}
	return rt
}

// quotaOutNow: the CLIs whose card would say "out of quota" now, from the
// same readers as the dashboard.
func quotaOutNow() map[string]bool {
	out := map[string]bool{}
	for _, r := range []readers.Reader{readers.NewAgy(), readers.NewCodex(), readers.NewOpencode()} {
		if readers.Current(r).Read().Status == state.OutOfQuota {
			out[r.Agent()] = true
		}
	}
	return out
}

// evidence gathers what past runs say and saves new checks.
func (rt routing) evidence() []router.Evidence {
	var rs []history.Run
	if rt.History != nil {
		rs = rt.History()
	}
	ev := router.Gather(rs, rt.Checks, rt.Now())
	if rt.Checks != nil {
		_ = rt.Checks.Save()
	}
	return ev
}

// decide routes a task over pool and returns the decision and the chain as links.
func (rt routing) decide(text, dir string, readOnly bool, pool []Link) (router.Decision, []Link, error) {
	byArm := map[string]Link{}
	var arms []string
	for _, l := range pool {
		arms = append(arms, l.String())
		byArm[l.String()] = l
	}
	out := map[string]bool{}
	if rt.QuotaOut != nil {
		out = rt.QuotaOut()
	}
	d, err := router.Decide(router.Request{
		Task: text, Dir: dir, ReadOnly: readOnly, Pool: arms,
		Evidence: rt.evidence(), OutOfQuota: out, Now: rt.Now(), Rand: rt.Rand, External: rt.External,
	})
	if err != nil {
		return d, nil, err
	}
	chain := make([]Link, 0, len(d.Chain))
	for _, a := range d.Chain {
		chain = append(chain, byArm[a])
	}
	return d, chain, nil
}

// recordCheck checks the run that ended a delegation right away (its
// changes and tests are freshest now) and caches the result for the router.
func recordCheck(checks *router.Checks, results []Result, text string, now time.Time) {
	if checks == nil || len(results) == 0 {
		return
	}
	last := results[len(results)-1]
	if last.Status != runs.Done || last.RunFile == "" {
		return
	}
	r, err := runs.ReadFile(last.RunFile)
	if err != nil {
		return
	}
	h := history.Run{
		Stamp: r.ID, Agent: r.Agent, Task: r.Task, FullTask: text, Dir: r.Dir,
		Status: r.Status, Start: r.StartTime(), End: r.EndTime(), Log: r.Log, ReadOnly: r.ReadOnly,
	}
	checks.Put(h.Key(), router.CheckRun(h, now))
	_ = checks.Save()
}

// --------------------------------------------------------- panal route --

const routeUsage = `Usage: panal route [-d DIR] [-r] "task"
       panal route [-d DIR] [-r] -f task.md
       panal route -stats

Shows what "panal delegate -c auto" would do with a task, without running
anything: its type and tier (and which rules said so), each arm of the pool
with its estimated success and the runs behind it, the chain it would try
and the one-line reason. With -stats, the learned table: every arm × task
type × tier with its record.

Pool (cheapest first): PANAL_POOL, else "pool = ..." in %s, else the chain,
else every installed CLI. "pool = auto" (or -p auto) is every discovered
model the models / exclude keys allow, at most pool_max_per_cli (4) per CLI,
ordered by an estimated cost: see "panal models". Run "panal delegate -h"
for the chain syntax.

Flags:
`

// RouteMain runs `panal route` with args (without "route") and returns the
// exit code.
func RouteMain(args []string, stdout, stderr io.Writer) int {
	conf := config.Read(config.Path())
	return routeMain(args, stdout, stderr, conf, systemRouting(conf), config.LookPath)
}

func routeMain(args []string, stdout, stderr io.Writer, conf config.Conf, rt routing, lookPath func(string) (string, error)) int {
	fs := flag.NewFlagSet("panal route", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("d", "", "the directory the task would run in (passed to an external router)")
	readOnly := fs.Bool("r", false, "the task would be read-only")
	file := fs.String("f", "", `read the task from this file ("-" = stdin)`)
	poolFlag := fs.String("p", "", `pool to use instead of the configured one, "cli:model[:effort] ..." cheapest first, or "auto"`)
	stats := fs.Bool("stats", false, "print the learned table (arm × type × tier) and exit")
	seed := fs.Uint64("seed", 0, "seed for the random draws (0 = from the clock), to repeat a result")
	fs.Usage = func() {
		fmt.Fprintf(stderr, routeUsage, config.Path())
		fs.PrintDefaults()
	}
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		words, args = append(words, rest[0]), rest[1:]
	}
	bad := func(msg string) int {
		fmt.Fprintln(stderr, "panal route:", msg)
		return 2
	}
	chain, _ := chainSpec("", conf)
	spec, from := poolSpec(chain, conf)
	if strings.TrimSpace(*poolFlag) != "" {
		spec, from = *poolFlag, "-p"
	}
	pool, info, err := rt.resolvePool(spec, from, conf, lookPath, true)
	if err != nil {
		return bad(fmt.Sprintf("%v (pool from %s)", err, from))
	}
	if *seed != 0 {
		rt.Rand = rand.New(rand.NewPCG(*seed, *seed^0x9e3779b97f4a7c15))
	}
	if *stats {
		fmt.Fprint(stdout, statsText(rt.evidence(), pool, rt.Now()))
		return 0
	}

	text := strings.Join(words, " ")
	switch {
	case *file != "" && text != "":
		return bad("give the task as an argument or with -f, not both")
	case *file == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return bad(err.Error())
		}
		text = string(b)
	case *file != "":
		b, err := os.ReadFile(*file)
		if err != nil {
			return bad(err.Error())
		}
		text = string(b)
	}
	if strings.TrimSpace(text) == "" {
		return bad("no task given (or -stats)")
	}
	d, _, err := rt.decide(text, *dir, *readOnly, pool)
	if err != nil {
		return bad(err.Error())
	}
	fmt.Fprint(stdout, decisionText(d, text, info, rt.QuotaOut))
	return 0
}

// maxSuggest: how many discovered arms `panal route` names after the pool.
const maxSuggest = 6

// poolNotes are the lines under the pool line: warnings and the discovered
// arms an explicit pool doesn't have.
func (p poolInfo) poolNotes() string {
	var b strings.Builder
	for _, n := range p.Notes {
		fmt.Fprintf(&b, "warn   %s\n", n)
	}
	if len(p.Suggest) > 0 {
		s := p.Suggest
		more := ""
		if len(s) > maxSuggest {
			s, more = s[:maxSuggest], fmt.Sprintf(" (+%d more)", len(p.Suggest)-maxSuggest)
		}
		fmt.Fprintf(&b, "also   discovered, not in the pool: %s%s; pool = auto would use them (panal models)\n", strings.Join(s, " "), more)
	}
	return b.String()
}

// decisionText is `panal route`'s answer.
func decisionText(d router.Decision, task string, info poolInfo, quotaOut func() map[string]bool) string {
	var b strings.Builder
	first, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	if r := []rune(first); len(r) > 90 {
		first = string(r[:89]) + "…"
	}
	fmt.Fprintf(&b, "task   %s\n", first)
	fmt.Fprintf(&b, "type   %s\n", d.Type)
	var why []string
	for _, h := range d.Hits {
		why = append(why, fmt.Sprintf("%s %+d", h.Rule, h.Points))
	}
	if len(why) == 0 {
		why = append(why, "no rule applied")
	}
	fmt.Fprintf(&b, "tier   %s  (score %+d: %s)\n", d.Tier, d.Score, strings.Join(why, ", "))
	if d.External != "" {
		fmt.Fprintf(&b, "       %s\n", d.External)
	}
	from := info.From
	if info.Auto {
		from = fmt.Sprintf("%s (auto: %d of %d allowed arms, at most %d per CLI)", info.From, len(d.Estimates), info.Allowed, info.Cap)
	}
	fmt.Fprintf(&b, "pool   %d %s from %s, cheapest first\n", len(d.Estimates), plural(len(d.Estimates), "arm", "arms"), from)
	b.WriteString(info.poolNotes())
	b.WriteString("\n")

	out := map[string]bool{}
	if quotaOut != nil {
		out = quotaOut()
	}
	w := len("arm")
	for _, e := range d.Estimates {
		w = max(w, len(e.Arm))
	}
	fmt.Fprintf(&b, "    %-*s  %5s  %5s  %-22s  %s\n", w, "arm", "est.", "draw", "runs: cell · tier · all", "note")
	for _, e := range d.Estimates {
		mark := "  "
		if e.Arm == d.Chain[0] {
			mark = "→ "
		}
		var notes []string
		if e.Arm == d.Favorite {
			notes = append(notes, "favorite")
		}
		if out[strings.SplitN(e.Arm, ":", 2)[0]] {
			notes = append(notes, "◐ out of quota now")
		}
		if e.Arm1.N == 0 {
			notes = append(notes, fmt.Sprintf("prior %.0f%%", 100*e.Prior))
		}
		runsCol := fmt.Sprintf("%d/%d · %d/%d · %d/%d", e.Cell.OK, e.Cell.N, e.ArmTier.OK, e.ArmTier.N, e.Arm1.OK, e.Arm1.N)
		row := fmt.Sprintf("  %s%-*s  %4.0f%%  %5.2f  %-22s  %s", mark, w, e.Arm, 100*e.Post.Mean(), d.Draws[e.Arm], runsCol, strings.Join(notes, ", "))
		b.WriteString(strings.TrimRight(row, " ") + "\n")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "chain  %s\n", strings.Join(d.Chain, " "))
	fmt.Fprintf(&b, "%s\n", d.Why)
	return b.String()
}

// statsText is `panal route -stats`: the learned record per arm × type ×
// tier, with the estimate each cell gives (pool arms by their cost rank,
// the others as middle ones).
func statsText(ev []router.Evidence, pool []Link, now time.Time) string {
	var arms []string
	for _, l := range pool {
		arms = append(arms, l.String())
	}
	ev = router.ResolveArms(ev, arms)
	type key struct{ arm, typ, tier string }
	type cell struct {
		ok, n, none int
		ws, wf      float64
	}
	cells := map[key]*cell{}
	signals, total := 0, 0
	for _, e := range ev {
		total++
		k := key{e.Arm, e.Type, e.Tier}
		if cells[k] == nil {
			cells[k] = &cell{}
		}
		c := cells[k]
		w := router.Weight(e.At, now)
		switch e.Outcome {
		case router.Success:
			c.ok++
			c.n++
			c.ws += w
			signals++
		case router.Failure:
			c.n++
			c.wf += w
			signals++
		default:
			c.none++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "learned from %d %s (%d with a signal; out of quota, skipped and interrupted ones say nothing), half-life %d days\n",
		total, plural(total, "run", "runs"), signals, int(router.HalfLife.Hours()/24))
	if len(cells) == 0 {
		b.WriteString("no delegated runs yet: the router uses its priors\n")
		return b.String()
	}
	keys := make([]key, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	rankOf := map[string]int{}
	for i, a := range arms {
		rankOf[a] = i
	}
	inPool := func(a string) (int, bool) { r, ok := rankOf[a]; return r, ok }
	idx := func(list []string, v string) int {
		for i, x := range list {
			if x == v {
				return i
			}
		}
		return len(list)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, oki := inPool(keys[i].arm)
		rj, okj := inPool(keys[j].arm)
		if oki != okj {
			return oki
		}
		if keys[i].arm != keys[j].arm {
			if oki {
				return ri < rj
			}
			return keys[i].arm < keys[j].arm
		}
		if a, c := idx(report.TypeOrder, keys[i].typ), idx(report.TypeOrder, keys[j].typ); a != c {
			return a < c
		}
		return idx(router.Tiers, keys[i].tier) < idx(router.Tiers, keys[j].tier)
	})
	table := router.Learn(ev, now, func(a string) (int, int) {
		if r, ok := rankOf[a]; ok {
			return r, max(len(arms), 1)
		}
		return (len(arms) - 1) / 2, max(len(arms), 1)
	})
	w := len("arm")
	for _, k := range keys {
		w = max(w, len(k.arm)+2)
	}
	fmt.Fprintf(&b, "\n%-*s  %-8s  %-7s  %7s  %9s  %5s\n", w, "arm", "type", "tier", "ok/runs", "weighted", "est.")
	n := len(arms)
	for _, k := range keys {
		c := cells[k]
		r, ok := inPool(k.arm)
		if !ok {
			r = (n - 1) / 2
		}
		est := table.Estimate(k.arm, r, max(n, 1), k.typ, k.tier).Post.Mean()
		name := k.arm
		if !ok {
			name += " *"
		}
		line := fmt.Sprintf("%-*s  %-8s  %-7s  %7s  %9s  %4.0f%%", w, name, k.typ, k.tier,
			fmt.Sprintf("%d/%d", c.ok, c.n), fmt.Sprintf("%.1f/%.1f", c.ws, c.ws+c.wf), 100*est)
		if c.none > 0 {
			line += fmt.Sprintf("  (+%d no signal)", c.none)
		}
		b.WriteString(line + "\n")
	}
	for _, k := range keys {
		if _, ok := inPool(k.arm); !ok {
			b.WriteString("\n* not in the pool: shown for the record, never picked\n")
			break
		}
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// autoTask lets the router pick t's chain from pool and records why.
func autoTask(rt routing, t *Task, pool []Link) (router.Decision, error) {
	d, chain, err := rt.decide(t.Text, t.Dir, t.ReadOnly, pool)
	if err != nil {
		return d, err
	}
	t.Chain, t.TaskType, t.Tier = chain, d.Type, d.Tier
	t.Auto, t.Route, t.Explored = true, d.Why, d.Explored
	return d, nil
}

// classifyTask is the type and tier recorded for a run whose chain was not
// picked by the router, so it can learn from it too.
func classifyTask(text string, readOnly bool) (string, string) {
	f := router.Extract(text, readOnly)
	tier, _, _ := router.Classify(f)
	return f.Type, tier
}

// runID is the ID to rate: the run that ended the chain.
func runID(results []Result) string {
	if len(results) == 0 {
		return "last"
	}
	return strings.TrimSuffix(filepath.Base(results[len(results)-1].RunFile), ".json")
}
