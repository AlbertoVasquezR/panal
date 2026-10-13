package delegate

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/models"
)

// `panal models`: the discovered catalog and what the router may do with it.

const modelsUsage = `Usage: panal models [-refresh]

Lists the models each agent CLI says it can use, cached in %s:

  agy       agy models
  codex     codex debug models
  opencode  opencode api model.list (opencode models as a fallback)
  cursor    cursor-agent models (cursor-agent --list-models as a fallback)

Those commands only list: nothing is run and no quota is spent. The cache
is refreshed with -refresh, in the background when the dashboard starts and
it is older than 24 h, and by panal route / panal delegate -c auto only
when it is missing.

For each model: its estimated cost (lower is cheaper; the heuristic is in
docs/models.md), and whether it is in the router's pool, allowed by the models /
exclude keys of %s, or left out and why. "pool = auto" uses the allowed
ones, at most pool_max_per_cli (4) per CLI, cheapest first.

Flags:
`

// ModelsMain runs `panal models` with args (without "models") and returns
// the exit code.
func ModelsMain(args []string, stdout, stderr io.Writer) int {
	conf := config.Read(config.Path())
	return modelsMain(args, stdout, stderr, conf, models.Path(), models.Exec, config.LookPath, time.Now)
}

func modelsMain(args []string, stdout, stderr io.Writer, conf config.Conf, path string, run models.Runner, lookPath func(string) (string, error), now func() time.Time) int {
	fs := flag.NewFlagSet("panal models", flag.ContinueOnError)
	fs.SetOutput(stderr)
	refresh := fs.Bool("refresh", false, "ask every installed CLI for its models again")
	fs.Usage = func() {
		fmt.Fprintf(stderr, modelsUsage, path, config.Path())
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "panal models: unexpected argument", fs.Arg(0))
		return 2
	}
	cat, err := models.Load(path)
	switch {
	case *refresh:
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cat = models.Refresh(ctx, run, lookPath, cat, models.CLIs, 30*time.Second, now())
		cancel()
		if err := cat.Save(path); err != nil {
			fmt.Fprintln(stderr, "panal models: could not save the cache:", err)
		}
	case errors.Is(err, os.ErrNotExist):
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cat = models.Ensure(ctx, path, run, lookPath, 20*time.Second, now())
		cancel()
	case err != nil:
		fmt.Fprintf(stderr, "panal models: %s is unreadable (%v); run panal models -refresh\n", path, err)
	}

	chain, _ := chainSpec("", conf)
	spec, from := poolSpec(chain, conf)
	rt := routing{Catalog: func(bool) models.Catalog { return cat }}
	pool, info, perr := rt.resolvePool(spec, from, conf, lookPath, false)
	rules, _ := rulesOf(conf)
	fmt.Fprint(stdout, modelsText(cat, rules, pool, info, perr, path, now()))
	return 0
}

// modelsText is `panal models`' answer.
func modelsText(cat models.Catalog, rules models.Rules, pool []Link, info poolInfo, perr error, path string, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cache  %s\n", path)
	allow, deny := strings.Join(rules.Allow, " "), strings.Join(rules.Exclude, " ")
	if allow == "" {
		allow = "(unset: every discovered model)"
	}
	if deny == "" {
		deny = "(unset)"
	}
	fmt.Fprintf(&b, "rules  models = %s\n       exclude = %s\n       pool_max_per_cli = %d\n", allow, deny, rules.Cap())

	// Where each pool arm is: "cli:model" → its places and efforts.
	type place struct {
		n      int
		effort string
	}
	inPool := map[string][]place{}
	if perr != nil {
		fmt.Fprintf(&b, "pool   %v (from %s)\n", perr, info.From)
	} else {
		what := info.From
		switch {
		case info.Auto:
			what = fmt.Sprintf("%s (auto: %d of %d allowed arms, at most %d per CLI)", info.From, len(pool), info.Allowed, info.Cap)
		case !info.Explicit:
			what += " (each CLI's own default model)"
		}
		fmt.Fprintf(&b, "pool   %d %s from %s, cheapest first\n", len(pool), plural(len(pool), "arm", "arms"), what)
		b.WriteString(info.poolNotes())
		w := len("arm")
		for _, l := range pool {
			w = max(w, len(l.String()))
		}
		fmt.Fprintf(&b, "\n   #  %-*s  %5s  %s\n", w, "arm", "cost", "estimated from")
		for i, l := range pool {
			cost, how := "    ?", "not in the catalog"
			if m, ok := cat.Find(l.CLI, l.Model); ok {
				s, why := models.Cost(l.CLI, m, l.Effort)
				cost, how = fmt.Sprintf("%5.2f", s), why
			} else if l.Model == "" {
				how = l.CLI + "'s own default model"
			}
			fmt.Fprintf(&b, "  %2d  %-*s  %s  %s\n", i+1, w, l.String(), cost, how)
			k := strings.ToLower(l.CLI + ":" + l.Model)
			inPool[k] = append(inPool[k], place{i + 1, l.Effort})
		}
	}

	for _, cli := range models.CLIs {
		e := cat.Get(cli)
		b.WriteString("\n")
		switch {
		case e == nil:
			fmt.Fprintf(&b, "%s — not asked yet (panal models -refresh)\n", cli)
			continue
		case len(e.Models) == 0:
			fmt.Fprintf(&b, "%s — no models: %s\n", cli, e.Error)
			continue
		}
		fmt.Fprintf(&b, "%s — %d %s, cached %s ago (%s)\n", cli, len(e.Models), plural(len(e.Models), "model", "models"), cat.Ago(cli, now), e.Command)
		if e.Error != "" {
			fmt.Fprintf(&b, "  last refresh failed, showing the cached ones: %s\n", e.Error)
		}
		type row struct {
			cost               float64
			id, efforts, state string
		}
		var rows []row
		verdicts := rules.Judge(cat, cli)
		var allowed []models.Arm
		for _, v := range verdicts {
			allowed = append(allowed, v.Arms...)
		}
		newest := map[string]bool{}
		for _, a := range models.Newest(allowed) {
			newest[strings.ToLower(a.CLI+":"+a.Model)] = true
		}
		for _, v := range verdicts {
			m := v.Model
			// Models compare at the same effort: medium when they take one.
			at := ""
			if len(m.Efforts) > 0 {
				at = "medium"
			}
			s, _ := models.Cost(cli, m, at)
			r := row{cost: s, id: m.ID, efforts: strings.Join(m.Efforts, " ")}
			if r.efforts == "" {
				r.efforts = "-"
			}
			if ps := inPool[strings.ToLower(cli+":"+m.ID)]; len(ps) > 0 {
				var parts []string
				for _, p := range ps {
					if p.effort != "" {
						parts = append(parts, fmt.Sprintf("#%d %s", p.n, p.effort))
					} else {
						parts = append(parts, fmt.Sprintf("#%d", p.n))
					}
				}
				r.state = "in pool " + strings.Join(parts, ", ")
			} else if len(v.Arms) > 0 {
				switch {
				case info.Auto && !newest[strings.ToLower(cli+":"+m.ID)]:
					r.state = "allowed, but a newer version is too"
				case info.Auto:
					r.state = "allowed, over the cap (pool_max_per_cli)"
				default:
					r.state = "allowed, not in the pool"
				}
			} else {
				r.state = v.Why
			}
			rows = append(rows, r)
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].cost < rows[j].cost })
		wi, we := len("model"), len("efforts")
		for _, r := range rows {
			wi, we = max(wi, len(r.id)), max(we, len(r.efforts))
		}
		fmt.Fprintf(&b, "  %5s  %-*s  %-*s  %s\n", "cost", wi, "model", we, "efforts", "state")
		for _, r := range rows {
			fmt.Fprintf(&b, "  %5.2f  %-*s  %-*s  %s\n", r.cost, wi, r.id, we, r.efforts, r.state)
		}
	}
	b.WriteString("\ncost: estimated, lower is cheaper (class 0 free … 4 strong, + effort, + version); an explicit pool's order wins.\n")
	return b.String()
}
