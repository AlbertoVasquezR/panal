package mascots

import (
	"fmt"
	"strings"
	"testing"
)

var modes = []Mode{Idle, Working, Sleeping, Stuck, Nap, Celebrate, Scared, WakeUp, Greet, Pet, Conduct, LookLeft, LookRight}

// A badly drawn frame does not crash: it paints a gap or a default color and
// nobody notices. This checks every frame of every mode of every mascot.
func TestFramesWellFormed(t *testing.T) {
	if len(All) != 5 {
		t.Fatalf("expected 5 mascots, there are %d", len(All))
	}
	for name, s := range All {
		if len(s.Work) < 4 {
			t.Errorf("%s: the work cycle has %d frames, want at least 4", name, len(s.Work))
		}
		for _, m := range modes {
			for n := 0; n < 40; n++ {
				rows, _ := s.Frame(m, n)
				check(t, name, m, n, s, rows)
			}
		}
	}
}

func check(t *testing.T, name string, m Mode, n int, s Sprite, rows []string) {
	t.Helper()
	if len(rows) != Height {
		t.Errorf("%s mode %d n=%d: %d rows, want %d", name, m, n, len(rows), Height)
		return
	}
	for y, row := range rows {
		if len(row) != Width {
			t.Errorf("%s mode %d n=%d row %d: %q is %d wide, want %d", name, m, n, y, row, len(row), Width)
		}
		for _, r := range row {
			if r == transparent {
				continue
			}
			if _, ok := s.Palette[r]; !ok {
				t.Errorf("%s mode %d n=%d row %d: the letter %q is not in the palette", name, m, n, y, r)
			}
		}
	}
}

// Every animation has to really move: if all its frames are equal, "animated"
// does not show.
func TestEveryModeMoves(t *testing.T) {
	for name, s := range All {
		for _, m := range modes {
			distinct := map[string]bool{}
			for n := 0; n < 40; n++ {
				rows, z := s.Frame(m, n)
				key := strings.Join(rows, "")
				for _, p := range z {
					key += fmt.Sprint(p)
				}
				distinct[key] = true
			}
			if len(distinct) < 2 {
				t.Errorf("%s mode %d: all its frames are equal", name, m)
			}
		}
	}
}

// Blinking and sleeping close the eyes: the Eyes pixels change letter.
func TestEyesClosed(t *testing.T) {
	for name, s := range All {
		closed := s.closeEyes()
		for _, o := range s.Eyes {
			if s.Base[o[0]][o[1]] == byte(s.Eyelid) {
				t.Errorf("%s: the eye (%d,%d) already has the eyelid letter; the blink would not show", name, o[0], o[1])
			}
			if closed[o[0]][o[1]] != byte(s.Eyelid) {
				t.Errorf("%s: the eye (%d,%d) did not close", name, o[0], o[1])
			}
		}
	}
}

func TestSleepingHasZ(t *testing.T) {
	for name, s := range All {
		seen := false
		for n := 0; n < 20; n++ {
			if _, z := s.Frame(Sleeping, n); len(z) > 0 {
				seen = true
			}
		}
		if !seen {
			t.Errorf("%s: sleeping never shows the z", name)
		}
	}
}

func TestRenderHasExpectedSize(t *testing.T) {
	for name, s := range All {
		for _, m := range modes {
			if l := strings.Split(s.Render(m, 3), "\n"); len(l) != Height/2 {
				t.Errorf("%s mode %d: %d lines painted, want %d", name, m, len(l), Height/2)
			}
		}
	}
}

func TestGray(t *testing.T) {
	g := gray("#D97757")
	if len(g) != 7 || g[1:3] != g[3:5] || g[3:5] != g[5:7] {
		t.Fatalf("gray(#D97757) = %q, not a gray", g)
	}
}

func TestZsOnlyWhenSleepingAndGrows(t *testing.T) {
	for _, m := range []Mode{Idle, Working, Stuck, Celebrate, Pet} {
		for n := 0; n < 10; n++ {
			if z := Zs(m, n); z != "" {
				t.Fatalf("mode %d n=%d: Zs = %q, there should only be zzz when sleeping", m, n, z)
			}
		}
	}
	var seen []string
	for n := 0; n < 4*zStep; n += zStep {
		seen = append(seen, Zs(Sleeping, n))
	}
	if strings.Join(seen, "|") != "z|zZ|zZz|" {
		t.Fatalf("the zzz does not grow as it should: %q", seen)
	}
}
