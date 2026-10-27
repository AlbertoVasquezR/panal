package delegate

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/models"
)

// Link is one entry of the fallback chain: which CLI to run, with which model
// and reasoning effort. Empty Model or Effort means "the CLI's own default".
type Link struct {
	CLI    string
	Model  string
	Effort string
}

func (l Link) String() string {
	s := l.CLI
	if l.Model != "" {
		s += ":" + l.Model
	}
	if l.Effort != "" {
		if l.Model == "" {
			s += ":"
		}
		s += ":" + l.Effort
	}
	return s
}

// efforts are the reasoning effort names any of the CLIs accepts. The last
// ":word" of a link is taken as the effort only when it is one of these, so a
// model name that itself contains ":" (e.g. ollama's "llama3:8b") still works.
// The list lives in internal/models, which builds links from discovered
// models the same way.
var efforts = func() map[string]bool {
	m := map[string]bool{}
	for _, e := range models.ChainEfforts {
		m[e] = true
	}
	return m
}()

// DefaultCLIs is the order of the default chain, used when neither -c,
// PANAL_CHAIN nor the config key chain say otherwise: every installed one of
// these, each with its own default model.
var DefaultCLIs = []string{"codex", "agy", "opencode", "cursor"}

// ParseChain parses "cli:model[:effort] cli[:model] ...". Links are separated
// by spaces or commas.
func ParseChain(s string) ([]Link, error) {
	var out []Link
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r' }) {
		cli, rest, _ := strings.Cut(f, ":")
		cli = strings.ToLower(strings.TrimSpace(cli))
		if _, ok := agents[cli]; !ok {
			return nil, fmt.Errorf("unknown agent %q in chain link %q (known: codex, agy, opencode, cursor)", cli, f)
		}
		l := Link{CLI: cli, Model: rest}
		if i := strings.LastIndex(rest, ":"); i >= 0 && efforts[strings.ToLower(rest[i+1:])] {
			l.Model, l.Effort = rest[:i], strings.ToLower(rest[i+1:])
		} else if efforts[strings.ToLower(rest)] && !strings.Contains(rest, "/") {
			// "codex:low" is ambiguous; treat a bare effort word as an effort.
			l.Model, l.Effort = "", strings.ToLower(rest)
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the chain is empty")
	}
	return out, nil
}

// chainSpec picks the chain text: -c, else PANAL_CHAIN, else the config key
// chain. Empty means the default chain. from says where it came from.
func chainSpec(flagValue string, conf config.Conf) (spec, from string) {
	switch {
	case strings.TrimSpace(flagValue) != "":
		return flagValue, "-c"
	case strings.TrimSpace(os.Getenv("PANAL_CHAIN")) != "":
		return os.Getenv("PANAL_CHAIN"), "PANAL_CHAIN"
	case strings.TrimSpace(conf.Chain) != "":
		return conf.Chain, "chain in " + config.Path()
	}
	return "", "default"
}

// defaultChain is every installed CLI of DefaultCLIs, in that order.
func defaultChain(lookPath func(string) (string, error)) []Link {
	var out []Link
	for _, c := range DefaultCLIs {
		if _, err := lookPath(c); err == nil {
			out = append(out, Link{CLI: c})
		}
	}
	return out
}
