// Package mascots draws one pixel-art mascot per agent with half blocks:
// each ▀ character paints two pixels (the text color on top, the background
// color below). A 12×10 pixel sprite takes 12×5 characters.
//
// Each mascot has a base drawing and a hand-drawn work cycle; the other
// animations (blinking, falling asleep, shaking) come from the base drawing
// through transformations, so dozens of frames need not be drawn.
package mascots

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Width and Height in pixels; in characters the height is half.
const Width, Height = 12, 10

const transparent = '.'

// Mode is the animation that fits the agent's status, or the short reaction
// to something that just happened.
type Mode int

const (
	Idle     Mode = iota // idle, done, no data: blinks and looks around
	Working              // working or orchestrating: its work cycle
	Sleeping             // out of quota, no permission, failed: gray, eyes closed, zzz
	Stuck                // stuck: gray and shaking
	Nap                  // idle for hours: eyes closed and zzz, but in color

	// Reactions: they last Duration(m) ticks and then the status mode returns.
	Celebrate // the run finished: jumps with confetti
	Scared    // the run failed: shakes with a red «!»
	WakeUp    // started working: opens its eyes, «!» and a hop
	Greet     // you just selected it: a hop
	Pet       // you petted it (p): closes its eyes and a heart rises
	Conduct   // claude hands the work to another: its baton cycle, fast
	LookLeft  // turns to look at a mascot on its left
	LookRight // turns to look at a mascot on its right
)

// Duration is how many ticks a reaction lasts; 0 if the mode is not a reaction.
func Duration(m Mode) int {
	switch m {
	case Celebrate:
		return 6
	case Scared:
		return 5
	case WakeUp:
		return 4
	case Greet:
		return 2
	case Pet:
		return 5
	case Conduct:
		return 4
	case LookLeft, LookRight:
		return 4
	}
	return 0
}

// Point is a pixel painted over the drawing in its own color (the z, the
// confetti, the heart, the drop): it never turns gray.
type Point struct {
	Y, X  int
	Color string
}

// Effects are added to any mode depending on the agent's status.
type Effects struct {
	Sweat bool // quota above 80 %: a drop falling next to the face
}

// Sprite: each row is a strip of letters; each letter is looked up in Palette
// and the dot is transparent.
type Sprite struct {
	Name    string
	Palette map[rune]string // letter → color "#rrggbb"
	Base    []string        // the drawing at rest
	Work    [][]string      // work cycle, drawn by hand
	Eyes    [][2]int        // pixels (row, column) that close when blinking
	Eyelid  rune            // which letter closes the eyes
}

// All, by agent name.
var All = map[string]Sprite{
	"claude":   claude,
	"agy":      agy,
	"codex":    codex,
	"opencode": opencode,
	"cursor":   cursor,
}

// Every how many ticks the z rises when sleeping.
const zStep = 2

// The sleeping z: it rises zigzagging through the top right corner.
var zPath = [][2]int{{3, 11}, {2, 10}, {1, 11}, {0, 10}}

const (
	colorZ     = "#ECEFF1"
	colorAlarm = "#FF5252"
	colorWarn  = "#FFD54F"
	colorPet   = "#FF4081"
	colorDrop  = "#4FC3F7"
)

// Idle cycle, in ticks: off-beat blinks (one double) and glances to the
// sides, so it does not look frozen or mechanical.
const idleCycle = 44

var (
	blinks  = map[int]bool{9: true, 27: true, 29: true}
	glances = map[int]int{15: -1, 16: -1, 17: -1, 36: 1, 37: 1}
)

// Frame returns the pixel rows of frame n in the given mode, plus the points
// that go on top (the z, the confetti…).
func (s Sprite) Frame(m Mode, n int) (rows []string, points []Point) {
	return s.FrameWith(m, n, Effects{})
}

// FrameWith is Frame with effects. In reactions, n counts from when the
// reaction started.
func (s Sprite) FrameWith(m Mode, n int, e Effects) (rows []string, points []Point) {
	n = abs(n)
	switch m {
	case Working:
		rows = s.Work[n%len(s.Work)]
	case Sleeping, Nap:
		rows, points = s.closeEyes(), zFor(n)
	case Stuck:
		rows = shiftX(s.Base, []int{0, 1, 0, -1}[n%4])
	case Celebrate:
		rows = s.Base
		if n%2 == 0 {
			rows = shiftY(s.Base, -1)
		}
		points = confetti[n%len(confetti)]
	case Scared:
		rows = shiftX(s.Base, []int{-1, 1, -1, 1, 0}[n%5])
		points = exclamation(colorAlarm)
	case WakeUp:
		switch n % 4 {
		case 0:
			rows = s.closeEyes()
		case 2:
			rows, points = shiftY(s.Base, -1), exclamation(colorWarn)
		default:
			rows, points = s.Base, exclamation(colorWarn)
		}
	case Greet:
		rows = s.Base
		if n%2 == 0 {
			rows = shiftY(s.Base, -1)
		}
	case Pet:
		rows, points = s.closeEyes(), heart(2-n%5, 9)
	case Conduct:
		rows, points = s.Work[n%len(s.Work)], exclamation(colorWarn)
	case LookLeft, LookRight: // turns, blinks mid-glance and keeps looking
		dx := -1
		if m == LookRight {
			dx = 1
		}
		rows = s.look(dx)
		if n%4 == 2 {
			rows = s.closeEyes()
		}
	default:
		c := n % idleCycle
		switch {
		case blinks[c]:
			rows = s.closeEyes()
		case glances[c] != 0:
			rows = s.look(glances[c])
		default:
			rows = s.Base
		}
	}
	if e.Sweat && (m == Idle || m == Working) {
		if y := []int{2, 3, 4, -1}[n%4]; y >= 0 {
			points = append(points, Point{y, 0, colorDrop})
		}
	}
	return rows, points
}

func zFor(n int) []Point {
	step := (n / zStep) % (len(zPath) + 1)
	if step >= len(zPath) {
		return nil
	}
	z := []Point{{zPath[step][0], zPath[step][1], colorZ}}
	if step > 0 { // a second z further down, lagging behind
		z = append(z, Point{zPath[step-1][0], zPath[step-1][1], colorZ})
	}
	return z
}

// The confetti falls along the edges, where there is hardly any drawing.
var confetti = func() [][]Point {
	c := []string{"#FF5252", "#FFD54F", "#69F0AE", "#4FC3F7", "#E040FB"}
	return [][]Point{
		{{0, 0, c[0]}, {1, 11, c[1]}, {2, 1, c[2]}, {0, 10, c[3]}},
		{{1, 0, c[3]}, {0, 11, c[4]}, {3, 10, c[0]}, {2, 11, c[2]}},
		{{2, 0, c[1]}, {3, 11, c[2]}, {0, 1, c[4]}, {1, 10, c[0]}},
		{{3, 1, c[4]}, {2, 10, c[1]}, {1, 0, c[2]}, {0, 11, c[3]}},
	}
}()

// exclamation: a «!» in the top right (two pixels, a gap and the dot).
func exclamation(color string) []Point {
	return []Point{{0, 11, color}, {1, 11, color}, {3, 11, color}}
}

// heart of 3×3 with its top left corner at (y, x); whatever falls outside
// the drawing is not painted.
func heart(y, x int) []Point {
	var out []Point
	for _, p := range [][2]int{{0, 0}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {2, 1}} {
		if yy := y + p[0]; yy >= 0 && yy < Height {
			out = append(out, Point{yy, x + p[1], colorPet})
		}
	}
	return out
}

// Zs is the text that goes with a sleeping mascot, at the same pace as the z
// of the drawing: "z", "zZ", "zZz" and a breath with nothing. Only in Sleeping and Nap.
func Zs(m Mode, n int) string {
	if m != Sleeping && m != Nap {
		return ""
	}
	return []string{"z", "zZ", "zZz", ""}[(abs(n)/zStep)%4]
}

func (s Sprite) closeEyes() []string {
	rows := append([]string(nil), s.Base...)
	for _, o := range s.Eyes {
		b := []byte(rows[o[0]])
		b[o[1]] = byte(s.Eyelid)
		rows[o[0]] = string(b)
	}
	return rows
}

// look moves the eyes dx pixels (to the left if dx < 0), only over pixels of
// the eyelid color, so eyes are not painted outside the face.
func (s Sprite) look(dx int) []string {
	b := make([][]byte, len(s.Base))
	for i, f := range s.Base {
		b[i] = []byte(f)
	}
	isEye := func(y, x int) bool {
		for _, o := range s.Eyes {
			if o[0] == y && o[1] == x {
				return true
			}
		}
		return false
	}
	for _, o := range s.Eyes {
		b[o[0]][o[1]] = byte(s.Eyelid)
	}
	for _, o := range s.Eyes {
		x := o[1] + dx
		if x >= 0 && x < Width && (s.Base[o[0]][x] == byte(s.Eyelid) || isEye(o[0], x)) {
			b[o[0]][x] = s.Base[o[0]][o[1]]
		}
	}
	out := make([]string, len(b))
	for i := range b {
		out[i] = string(b[i])
	}
	return out
}

// shiftX moves the drawing dx pixels; what goes out one side is lost.
func shiftX(rows []string, dx int) []string {
	if dx == 0 {
		return rows
	}
	out := make([]string, len(rows))
	blank := strings.Repeat(".", abs(dx))
	for i, f := range rows {
		if dx > 0 {
			out[i] = blank + f[:len(f)-dx]
		} else {
			out[i] = f[-dx:] + blank
		}
	}
	return out
}

// shiftY moves the drawing down dy pixels (up if dy < 0).
func shiftY(rows []string, dy int) []string {
	blank := strings.Repeat(".", len(rows[0]))
	out := make([]string, 0, len(rows))
	switch {
	case dy > 0:
		for i := 0; i < dy; i++ {
			out = append(out, blank)
		}
		out = append(out, rows[:len(rows)-dy]...)
	case dy < 0:
		out = append(out, rows[-dy:]...)
		for i := 0; i < -dy; i++ {
			out = append(out, blank)
		}
	default:
		out = append(out, rows...)
	}
	return out
}

// swap replaces letters across the whole drawing (to make colors flicker).
func swap(rows []string, pares ...string) []string {
	r := strings.NewReplacer(pares...)
	out := make([]string, len(rows))
	for i, f := range rows {
		out[i] = r.Replace(f)
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---------------------------------------------------------------- claude --
// The orchestrator: an orange creature that conducts with a baton. When
// working it raises and lowers the baton and hops on the downbeat.
var claude = Sprite{
	Name:    "claude",
	Palette: map[rune]string{'O': "#D97757", 'o': "#A9502F", 'k': "#1B1B1B", 'y': "#F2D16B"},
	Base: []string{
		"............",
		"...OOOOOO...",
		"..OOOOOOOO..",
		"..OkOOOOkO..",
		"..OOOOOOOO..",
		".oOOOOOOOOo.",
		".oOOOOOOOOoy",
		"..OOOOOOOO.y",
		"..O.O..O.O..",
		"............",
	},
	Eyes:   [][2]int{{3, 3}, {3, 8}},
	Eyelid: 'O',
}

func init() {
	down := claude.Base
	mid := []string{
		"............",
		"...OOOOOO...",
		"..OOOOOOOO..",
		"..OkOOOOkO..",
		"..OOOOOOOOyy",
		".oOOOOOOOOo.",
		".oOOOOOOOO..",
		"..OOOOOOOO..",
		"..O.O..O.O..",
		"............",
	}
	up := []string{
		"..........y.",
		"...OOOOOO.y.",
		"..OOOOOOOOo.",
		"..OkOOOOkO..",
		"..OOOOOOOO..",
		".oOOOOOOOO..",
		".oOOOOOOOO..",
		"..OOOOOOOO..",
		"..O.O..O.O..",
		"............",
	}
	// Downbeat: baton up and the body one pixel higher.
	jump := shiftY(up, -1)
	claude.Work = [][]string{down, mid, up, jump, up, mid}
	All["claude"] = claude
}

// ------------------------------------------------------------------- agy --
// Antigravity: an astronaut. When working it floats up and down and the
// thrusters' flame flickers.
var agy = Sprite{
	Name: "agy",
	Palette: map[rune]string{
		'w': "#E8EAF6", 'b': "#4FC3F7", 'p': "#7E57C2", 'P': "#4527A0",
		'f': "#FF9800", 'F': "#FFEB3B",
	},
	Base: []string{
		"............",
		"....wwww....",
		"...wbbbbw...",
		"...wbwbbw...",
		"...wwwwww...",
		"..pppppppp..",
		"..pPppppPp..",
		"...pp..pp...",
		"...ff..ff...",
		"....F..F....",
	},
	Eyes:   [][2]int{{3, 5}}, // the visor's glint
	Eyelid: 'b',
}

func init() {
	b := agy.Base
	shortFlame := append(append([]string(nil), b[:8]...), "...f....f...", "............")
	flicker := swap(b, "f", "F", "F", "f")
	agy.Work = [][]string{
		b,
		shiftY(flicker, 1),
		shiftY(shortFlame, 1),
		flicker,
	}
	All["agy"] = agy
}

// ----------------------------------------------------------------- codex --
// Codex: a robot with a terminal face. When working its eyes scan the
// screen and the antenna blinks.
var codex = Sprite{
	Name: "codex",
	Palette: map[rune]string{
		'g': "#B0BEC5", 'G': "#607D8B", 's': "#263238", 'e': "#69F0AE", 'r': "#FF5252",
	},
	Base: []string{
		".....r......",
		".....G......",
		"..gggggggg..",
		"..gssssssg..",
		"..gsessesg..",
		"..gssssssg..",
		"..gggggggg..",
		".GggggggggG.",
		"..gg....gg..",
		"..GG....GG..",
	},
	Eyes:   [][2]int{{4, 4}, {4, 7}},
	Eyelid: 's',
}

func init() {
	eyes := func(row string, antenna rune) []string {
		f := append([]string(nil), codex.Base...)
		f[0] = strings.Replace(f[0], "r", string(antenna), 1)
		f[4] = row
		return f
	}
	codex.Work = [][]string{
		eyes("..gsessesg..", 'r'), // centered
		eyes("..gessessg..", 'G'), // to the left
		eyes("..gsessesg..", 'r'),
		eyes("..gssesseg..", 'G'), // to the right
	}
	All["codex"] = codex
}

// -------------------------------------------------------------- opencode --
// opencode: a terminal cube. When working it types: characters appear
// after the prompt and the cursor moves on.
var opencode = Sprite{
	Name:    "opencode",
	Palette: map[rune]string{'k': "#121212", 'W': "#F5F5F5", 'y': "#FFD54F", 'c': "#4DD0E1"},
	Base: []string{
		"............",
		".WWWWWWWWWW.",
		".WkkkkkkkkW.",
		".WkykkkkkkW.",
		".WkkykkkkkW.",
		".WkykkWWkkW.",
		".WkkkkkkkkW.",
		".WWWWWWWWWW.",
		"...W....W...",
		"..WW....WW..",
	},
	Eyes:   [][2]int{{5, 6}, {5, 7}}, // the cursor
	Eyelid: 'k',
}

func init() {
	line := func(row5 string) []string {
		f := append([]string(nil), opencode.Base...)
		f[5] = row5
		return f
	}
	opencode.Work = [][]string{
		line(".WkykkWWkkW."),
		line(".WkykcWWkkW."),
		line(".WkykccWWkW."),
		line(".WkykcckkkW."),
		line(".WkykcccWWW."),
		line(".WkykcccckW."),
	}
	All["opencode"] = opencode
}

// ----------------------------------------------------------------- cursor --
// Cursor CLI: a cyan diamond (the mark) with a pointer tail. When working
// the diamond hops and the tip blinks.
var cursor = Sprite{
	Name: "cursor",
	Palette: map[rune]string{
		'C': "#38BDF8", 'c': "#0284C7", 'w': "#E0F2FE", 'k': "#0F172A",
	},
	Base: []string{
		"............",
		"....CCCC....",
		"...CCCCCC...",
		"...CkCCkC...",
		"...CCCCCC...",
		"....CCCC....",
		".....Cc.....",
		".....cC.....",
		"......c.....",
		"............",
	},
	Eyes:   [][2]int{{3, 4}, {3, 7}},
	Eyelid: 'C',
}

func init() {
	tip := func(tail []string) []string {
		f := append([]string(nil), cursor.Base...)
		copy(f[6:], tail)
		return f
	}
	hop := shiftY(cursor.Base, -1)
	cursor.Work = [][]string{
		cursor.Base,
		tip([]string{".....CC.....", ".....cC.....", "......c.....", "............"}),
		hop,
		tip([]string{".....Cc.....", "......C.....", "......c.....", "............"}),
	}
	All["cursor"] = cursor
}

// Render returns frame n of the given mode, in half blocks. In Sleeping and
// Stuck everything is gray (except the points): that way you can tell from
// afar that an agent is stopped.
func (s Sprite) Render(m Mode, n int) string { return s.RenderWith(m, n, Effects{}) }

// Cell is one character of a rendered frame: a half block (or a space) with
// its foreground and background colors as "#rrggbb"; "" means the terminal's
// default color (a transparent pixel).
type Cell struct {
	Char   rune
	FG, BG string
}

// Cells returns frame n of the given mode as Height/2 rows of Width cells,
// with the half-block encoding Render uses: ' ' (both pixels transparent),
// '█' (both the same color), '▄' (only the bottom one), '▀' (only the top
// one, or top and bottom in different colors with the bottom one as
// background). In Sleeping and Stuck everything is gray except the points.
func (s Sprite) Cells(m Mode, n int, e Effects) [][]Cell {
	rows, points := s.FrameWith(m, n, e)
	dimmed := m == Sleeping || m == Stuck
	over := map[[2]int]string{}
	for _, p := range points {
		if p.Y >= 0 && p.Y < Height && p.X >= 0 && p.X < Width {
			over[[2]int{p.Y, p.X}] = p.Color
		}
	}
	color := func(y, x int) (string, bool) {
		if c, ok := over[[2]int{y, x}]; ok {
			return c, true
		}
		c := rune(rows[y][x])
		if c == transparent {
			return "", false
		}
		hex := s.Palette[c]
		if dimmed {
			hex = gray(hex)
		}
		return hex, true
	}
	out := make([][]Cell, 0, len(rows)/2)
	for y := 0; y < len(rows); y += 2 {
		line := make([]Cell, 0, len(rows[y]))
		for x := 0; x < len(rows[y]); x++ {
			up, hasUp := color(y, x)
			down, hasDown := color(y+1, x)
			switch {
			case !hasUp && !hasDown:
				line = append(line, Cell{Char: ' '})
			case hasUp && hasDown && up == down:
				line = append(line, Cell{Char: '█', FG: up})
			case !hasUp:
				line = append(line, Cell{Char: '▄', FG: down})
			case !hasDown:
				line = append(line, Cell{Char: '▀', FG: up})
			default:
				line = append(line, Cell{Char: '▀', FG: up, BG: down})
			}
		}
		out = append(out, line)
	}
	return out
}

// RenderWith is Render with effects.
func (s Sprite) RenderWith(m Mode, n int, e Effects) string {
	var b strings.Builder
	cells := s.Cells(m, n, e)
	for i, line := range cells {
		for _, c := range line {
			if c.FG == "" && c.BG == "" {
				b.WriteRune(c.Char)
				continue
			}
			st := lipgloss.NewStyle().Foreground(lipgloss.Color(c.FG))
			if c.BG != "" {
				st = st.Background(lipgloss.Color(c.BG))
			}
			b.WriteString(st.Render(string(c.Char)))
		}
		if i+1 < len(cells) {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// gray turns "#rrggbb" into its gray by luminance.
func gray(hex string) string {
	if len(hex) != 7 {
		return "#808080"
	}
	v := func(i int) int {
		n := 0
		for _, c := range hex[i : i+2] {
			n *= 16
			switch {
			case c >= '0' && c <= '9':
				n += int(c - '0')
			case c >= 'a' && c <= 'f':
				n += int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				n += int(c-'A') + 10
			}
		}
		return n
	}
	l := (299*v(1) + 587*v(3) + 114*v(5)) / 1000
	l = 60 + l*100/255 // mid grays: neither black nor white
	const dig = "0123456789abcdef"
	h := string([]byte{dig[l/16], dig[l%16]})
	return "#" + h + h + h
}
