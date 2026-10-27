package pet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AlbertoVasquezR/panal/internal/config"
	"github.com/AlbertoVasquezR/panal/internal/mascots"
	"github.com/AlbertoVasquezR/panal/internal/readers"
	"github.com/AlbertoVasquezR/panal/internal/state"
	"github.com/AlbertoVasquezR/panal/internal/ui"
)

// Heartbeat: -stream writes a frame at least this often, even if nothing
// changed, so a consumer can tell panal is still alive.
const Heartbeat = 2 * time.Second

// Encode is one frame as a single line of JSON (no newline).
func Encode(f Frame) []byte {
	b, _ := json.Marshal(f) // only strings, ints and slices of them: cannot fail
	return b
}

// JSON reads every agent once and returns the frame at now.
func JSON(ls []readers.Reader, opts Options, now time.Time) Frame {
	p := New(opts)
	rows := readAll(ls)
	p.Observe(rows, now)
	return p.Frame(rows, now)
}

// StreamConfig: how often to re-read and how often at least to write.
type StreamConfig struct {
	Every     time.Duration // re-read the agents (2 s)
	Frame     time.Duration // animation pace (FrameEvery)
	Heartbeat time.Duration // write at least this often (Heartbeat)
	Now       func() time.Time
}

// Stream writes one JSON frame per line every time it changes (at most once
// per Frame) and at least once per Heartbeat. It returns nil when ctx ends
// or when w stops accepting writes (the reader closed the pipe).
func Stream(ctx context.Context, w io.Writer, ls []readers.Reader, opts Options, c StreamConfig) error {
	if c.Every <= 0 {
		c.Every = 2 * time.Second
	}
	if c.Frame <= 0 {
		c.Frame = FrameEvery
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = Heartbeat
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	p := New(opts)
	ticker := time.NewTicker(c.Frame)
	defer ticker.Stop()
	var (
		rows            []state.Row
		readAt, wroteAt time.Time
		last            []byte
	)
	for {
		now := c.Now()
		if readAt.IsZero() || now.Sub(readAt) >= c.Every {
			rows = readAll(ls)
			p.Observe(rows, now)
			readAt = now
		}
		b := Encode(p.Frame(rows, now))
		// One frame early, so the gap never goes over the heartbeat.
		if !bytes.Equal(b, last) || now.Sub(wroteAt) >= c.Heartbeat-c.Frame {
			if _, err := w.Write(append(b, '\n')); err != nil {
				return nil
			}
			last, wroteAt = b, now
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Main is `panal pet`.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pet", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "", "show this agent ("+strings.Join(agentNames(), ", ")+") instead of picking one")
	every := fs.Duration("every", 2*time.Second, "how often to re-read the agents")
	jsonFlag := fs.Bool("json", false, "print one JSON frame and exit")
	stream := fs.Bool("stream", false, "print one JSON frame per line while something changes (at least every 2 s) until the pipe closes")
	noAnim := fs.Bool("no-animation", ui.NoAnimation || os.Getenv("PANAL_NO_ANIMATION") != "", "still mascot, no reactions (or PANAL_NO_ANIMATION=1)")
	theme := fs.String("theme", os.Getenv("PANAL_THEME"), "colors: auto, dark, light or contrast (or PANAL_THEME)")
	cols := fs.Int("cols", 0, "with -json/-stream: at most this many raster columns (default: the sprite's 12)")
	rows := fs.Int("rows", 0, "with -json/-stream: at most this many raster rows (default: the sprite's 5)")
	all := fs.Bool("all", false, "with -json/-stream: also every shown agent's own mascot, in \"pets\"")
	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: panal pet [flags]          a mascot to keep in a small split pane (q quits)
       panal pet -json [flags]    one JSON frame, for other programs
       panal pet -stream [flags]  one JSON frame per line while it changes

The pet is claude while it orchestrates; otherwise the agent working (or the
one that finished last). See docs/pet.md.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	conf := config.Read(config.Path())
	if !given["theme"] && *theme == "" {
		*theme = conf.Theme
	}
	if !given["no-animation"] && !*noAnim && config.IsOff(conf.Animation) {
		*noAnim = true
	}
	if !given["every"] && conf.Interval != "" {
		if d, err := time.ParseDuration(conf.Interval); err == nil && d > 0 {
			*every = d
		}
	}
	switch {
	case *every <= 0:
		fmt.Fprintln(stderr, "panal pet: -every must be positive")
		return 2
	case *cols < 0 || *rows < 0:
		fmt.Fprintln(stderr, "panal pet: -cols and -rows cannot be negative")
		return 2
	case *agent != "" && !known(*agent):
		fmt.Fprintf(stderr, "panal pet: unknown agent %q: %s\n", *agent, strings.Join(agentNames(), ", "))
		return 2
	case *jsonFlag && *stream:
		fmt.Fprintln(stderr, "panal pet: -json and -stream go separately")
		return 2
	}
	ui.NoAnimation = *noAnim
	if err := ui.ApplyTheme(config.Theme(*theme)); err != nil {
		fmt.Fprintln(stderr, "panal pet:", err)
		return 2
	}
	ui.Conf = &config.Resolver{Path: config.Path()}
	opts := Options{Agent: *agent, Cols: *cols, Rows: *rows, NoAnimation: *noAnim, All: *all}
	ls := readers.All()

	switch {
	case *jsonFlag:
		if _, err := stdout.Write(append(Encode(JSON(ls, opts, time.Now())), '\n')); err != nil {
			return 1
		}
		return 0
	case *stream:
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := Stream(ctx, stdout, ls, opts, StreamConfig{Every: *every}); err != nil {
			fmt.Fprintln(stderr, "panal pet:", err)
			return 1
		}
		return 0
	}
	opts.Cols, opts.Rows = 0, 0
	if _, err := tea.NewProgram(newModel(New(opts), ls, *every, time.Now), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(stderr, "panal pet:", err)
		return 1
	}
	return 0
}

func agentNames() []string { return []string{"claude", "agy", "codex", "opencode", "cursor"} }

func known(a string) bool {
	_, ok := mascots.All[a]
	return ok
}
