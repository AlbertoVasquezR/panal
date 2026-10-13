// Package models discovers which models each agent CLI (agy, codex,
// opencode, cursor) can use, caches the catalog in ~/.panal/models.json and turns it
// into router arms ("cli:model[:effort]"):
//
//   - discover.go asks each CLI for its catalog (read-only listing commands
//     that start no turn and spend no quota) and parses the answers;
//   - models.go (this file) is the catalog and its cache;
//   - cost.go orders models cheapest → strongest with a documented
//     heuristic, since none of these catalogs publish prices for agy or
//     codex;
//   - pool.go applies the user's rules (models = allow globs, exclude = deny
//     globs, pool_max_per_cli) to build `pool = auto`, and checks an explicit
//     pool against the catalog.
package models

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/AlbertoVasquezR/panal/internal/runs"
)

// CLIs are the agent CLIs whose models panal discovers, in display order.
var CLIs = []string{"agy", "codex", "cursor", "opencode"}

// FileName is the cache's name inside runs.Home() (~/.panal).
const FileName = "models.json"

// MaxAge: a CLI's catalog older than this is refreshed in the background
// when the dashboard starts.
const MaxAge = 24 * time.Hour

// Path of the cache: ~/.panal/models.json (PANAL_DATA moves it).
func Path() string { return filepath.Join(runs.Home(), FileName) }

// Model is one model a CLI can use.
type Model struct {
	// ID is what goes after "cli:" in a chain link: agy's id (its effort is
	// part of it, e.g. gemini-3.8-flash-medium), codex's slug, opencode's
	// provider/model.
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"` // display name
	Description string `json:"description,omitempty"`
	// Efforts the CLI accepts for this model as a separate setting (codex's
	// supported_reasoning_levels, opencode's variants). Empty for agy.
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	// Hidden: the catalog keeps it out of its own picker (codex visibility
	// "hide", opencode enabled false). Never in an auto pool unless an allow
	// pattern names it exactly.
	Hidden bool `json:"hidden,omitempty"`
	// Retiring: the catalog says it is going away (codex's upgrade notice,
	// opencode's status "deprecated"). Same rule as Hidden.
	Retiring string `json:"retiring,omitempty"`
	// Priority is the catalog's own order (codex; 1 = first in its picker).
	// It is not a price: it only breaks ties.
	Priority int `json:"priority,omitempty"`
	// Price in $ per million output tokens, when the catalog says (opencode).
	Price *float64 `json:"price,omitempty"`
}

// CLI is one CLI's part of the catalog.
type CLI struct {
	Command string    `json:"command,omitempty"` // what was run, e.g. "codex debug models"
	At      time.Time `json:"at,omitempty"`      // the last refresh that found models
	Tried   time.Time `json:"tried,omitempty"`   // the last attempt
	Error   string    `json:"error,omitempty"`   // why the last attempt found nothing ("" = it worked)
	Models  []Model   `json:"models"`
}

// Catalog is every CLI's models: what ~/.panal/models.json holds.
type Catalog struct {
	Version int             `json:"version"`
	CLIs    map[string]*CLI `json:"clis"`
}

// Version of the cache format.
const Version = 1

// Load reads the cache. A missing or unreadable file gives an empty catalog
// and an error (os.ErrNotExist when there is none).
func Load(path string) (Catalog, error) {
	c := Catalog{Version: Version, CLIs: map[string]*CLI{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	var got Catalog
	if err := json.Unmarshal(b, &got); err != nil {
		return c, err
	}
	if got.CLIs == nil {
		got.CLIs = map[string]*CLI{}
	}
	got.Version = Version
	return got, nil
}

// Save writes the cache atomically (a temporary file, then a rename), so a
// reader never sees half of it.
func (c Catalog) Save(path string) error {
	if path == "" {
		return errors.New("no path for the models cache")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	c.Version = Version
	b, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Get is the CLI's entry, or nil.
func (c Catalog) Get(cli string) *CLI {
	if c.CLIs == nil {
		return nil
	}
	return c.CLIs[cli]
}

// Find is the model with that id (case-insensitive), if the CLI's catalog has it.
func (c Catalog) Find(cli, id string) (Model, bool) {
	e := c.Get(cli)
	if e == nil {
		return Model{}, false
	}
	for _, m := range e.Models {
		if equalFold(m.ID, id) {
			return m, true
		}
	}
	return Model{}, false
}

// Known says whether the CLI's catalog has models (so a model missing from
// it is really unknown).
func (c Catalog) Known(cli string) bool {
	e := c.Get(cli)
	return e != nil && len(e.Models) > 0
}

// Stale: the CLI's catalog is missing or older than maxAge. A failed attempt
// in the last 10 minutes is not retried yet.
func (c Catalog) Stale(cli string, now time.Time, maxAge time.Duration) bool {
	e := c.Get(cli)
	if e == nil {
		return true
	}
	if !e.Tried.IsZero() && now.Sub(e.Tried) < 10*time.Minute {
		return false
	}
	return e.At.IsZero() || now.Sub(e.At) > maxAge
}

// Names of the CLIs in the catalog, in CLIs order, then any other.
func (c Catalog) Names() []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range CLIs {
		if c.Get(n) != nil {
			out = append(out, n)
			seen[n] = true
		}
	}
	var rest []string
	for n := range c.CLIs {
		if !seen[n] {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}
