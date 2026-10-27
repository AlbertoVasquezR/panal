package models

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The cost heuristic. agy's and codex's catalogs publish no prices, so a
// model's cost is estimated from its name. The score is
//
//	class + effort + tiebreak
//
// and a lower score is cheaper (and, the router assumes, weaker). The
// class, an integer, comes first, so a cheap model at high effort still
// ranks below a strong model at low effort:
//
//	class  words in the id or display name
//	  0    free                                       (or a price of 0)
//	  1    nano, micro
//	  2    mini, lite, tiny, small, flash, haiku, luna, lightning, spark, air, oss
//	  3    no hint; sonnet, terra, sol, plus
//	  4    pro, max, ultra, opus, large, astra
//
// Precedence when several appear: free, then nano/micro, then the strong
// words, then the cheap ones. With no word at all, the description decides
// ("affordable", "efficient", "easier", "lightweight" → 2; "frontier",
// "most demanding", "hardest", "most capable" → 4), else 3.
//
// When the catalog has a real price (opencode: $ per million output tokens)
// the price sets the class instead: 0 → 0, up to 0.30 → 1, up to 1.50 → 2,
// up to 5 → 3, above → 4.
//
// effort (codex's and opencode's own setting, or the suffix of an agy id
// like gemini-3.8-flash-high):
//
//	none 0 · minimal 0.1 · low 0.2 · medium 0.4 · high 0.6 · xhigh 0.7 · max 0.8 · ultra 0.9
//
// plus 0.05 for a "thinking" model. tiebreak (under 0.1): the price/1000
// when known, else the model's version number/1000 (a newer version of the
// same line ranks a little stronger). Exact ties keep the catalog's own
// priority (codex), then the id.
//
// An explicit `pool = ...` always wins over all of this: its order is the
// cost rank.

// Effort offsets, cheapest first.
var effortCost = map[string]float64{
	"none": 0, "minimal": 0.1, "low": 0.2, "medium": 0.4, "high": 0.6, "xhigh": 0.7, "max": 0.8, "ultra": 0.9,
}

var classWords = []struct {
	class int
	words []string
}{
	{0, []string{"free"}},
	{1, []string{"nano", "micro"}},
	{4, []string{"pro", "max", "ultra", "opus", "large", "astra"}},
	{2, []string{"mini", "lite", "tiny", "small", "flash", "haiku", "luna", "lightning", "spark", "air", "oss", "composer"}},
	{3, []string{"sonnet", "terra", "sol", "plus"}},
}

var descWords = []struct {
	class int
	words []string
}{
	{4, []string{"frontier", "most demanding", "hardest", "most capable"}},
	{2, []string{"affordable", "efficient", "easier", "lightweight"}},
}

var (
	tokenSplit = regexp.MustCompile(`[^a-z0-9]+`)
	versionRe  = regexp.MustCompile(`\d+(?:\.\d+)?`)
)

func equalFold(a, b string) bool { return strings.EqualFold(a, b) }

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range tokenSplit.Split(strings.ToLower(s), -1) {
		if t != "" {
			out[t] = true
		}
	}
	return out
}

// idEffort: the effort an agy id carries as its last part
// ("gemini-3.8-flash-high" → "high").
func idEffort(id string) string {
	i := strings.LastIndexAny(id, "-_")
	if i < 0 {
		return ""
	}
	e := strings.ToLower(id[i+1:])
	if _, ok := effortCost[e]; ok {
		return e
	}
	return ""
}

// Cost is the heuristic score of one of cli's models at an effort ("" =
// the model's default, or for agy the effort its id carries) and why, e.g.
// "flash → class 2 · high +0.6 · v3.8".
func Cost(cli string, m Model, effort string) (float64, string) {
	base := m.ID
	idEff := ""
	if cli == "agy" {
		idEff = idEffort(m.ID)
	}
	if idEff != "" {
		base = m.ID[:len(m.ID)-len(idEff)-1]
	}
	// The provider of an opencode id ("opencode-go/") says nothing about the model.
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	tk := tokens(base + " " + stripEffortWords(m.Name))
	class, why := -1, ""
	if m.Price != nil {
		p := *m.Price
		switch {
		case p == 0:
			class = 0
		case p <= 0.30:
			class = 1
		case p <= 1.50:
			class = 2
		case p <= 5:
			class = 3
		default:
			class = 4
		}
		why = fmt.Sprintf("$%g/M out → class %d", p, class)
	}
	if class < 0 {
	words:
		for _, cw := range classWords {
			for _, w := range cw.words {
				if tk[w] {
					class, why = cw.class, fmt.Sprintf("%s → class %d", w, cw.class)
					break words
				}
			}
		}
	}
	if class < 0 {
		d := strings.ToLower(m.Description)
	desc:
		for _, dw := range descWords {
			for _, w := range dw.words {
				if strings.Contains(d, w) {
					class, why = dw.class, fmt.Sprintf("%q → class %d", w, dw.class)
					break desc
				}
			}
		}
	}
	if class < 0 {
		class, why = 3, "no hint → class 3"
	}
	score := float64(class)

	e := strings.ToLower(effort)
	if e == "" {
		e = idEff
	}
	if e == "" {
		e = strings.ToLower(m.DefaultEffort)
	}
	if off, ok := effortCost[e]; ok {
		score += off
		why += fmt.Sprintf(" · %s +%.1f", e, off)
	} else if e == "" && len(m.Efforts) > 0 {
		score += effortCost["medium"]
		why += " · default effort +0.4"
	}
	if tk["thinking"] {
		score += 0.05
		why += " · thinking +0.05"
	}
	switch {
	case m.Price != nil:
		score += math.Min(*m.Price, 99) / 1000
	default:
		if v := version(base); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				score += f / 1000
				why += " · v" + v
			}
		}
	}
	return score, why
}

// version is the first version number in an id: one or two digits, maybe
// with a dot part ("gpt-6.1-sol" → 6.1, "qwen3.8-max" → 3.8), not a size
// ("gpt-oss-120b", "llama-8b").
func version(id string) string {
	for _, ix := range versionRe.FindAllStringIndex(id, -1) {
		v := id[ix[0]:ix[1]]
		whole, _, _ := strings.Cut(v, ".")
		if len(whole) > 2 {
			continue
		}
		if ix[1] < len(id) {
			if c := id[ix[1]]; c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				continue
			}
		}
		return v
	}
	return ""
}

// stripEffortWords drops "(High)", "(Medium)"… from a display name, so they
// don't read as class words ("max").
func stripEffortWords(name string) string {
	if i := strings.LastIndex(name, "("); i >= 0 {
		inner := strings.ToLower(strings.Trim(name[i:], "() "))
		if _, ok := effortCost[inner]; ok {
			return name[:i]
		}
	}
	return name
}
