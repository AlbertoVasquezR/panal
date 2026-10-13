// preview builds a PNG sheet with every frame of every mode of every mascot,
// to review a design before seeing it in the terminal:
//
//	go run ./cmd/preview docs/animations.png
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"

	"github.com/AlbertoVasquezR/panal/internal/mascots"
)

func hex(h string) color.RGBA {
	v, _ := strconv.ParseUint(h[1:], 16, 32)
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

func grayC(c color.RGBA) color.RGBA {
	l := uint8((299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000)
	l = uint8(60 + int(l)*100/255)
	return color.RGBA{l, l, l, 255}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/preview out.png")
		os.Exit(2)
	}
	const px, gap, cols = 6, 2, 8
	order := []string{"claude", "agy", "codex", "opencode", "cursor"}
	modes := []struct {
		m      mascots.Mode
		frames []int
	}{
		{mascots.Working, []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{mascots.Idle, []int{0, 9, 15, 36}},
		{mascots.Sleeping, []int{0, 2, 4, 6, 8}},
		{mascots.Stuck, []int{0, 1, 2, 3}},
		{mascots.Nap, []int{0, 2, 4, 6, 8}},
		{mascots.Celebrate, []int{0, 1, 2, 3, 4, 5}},
		{mascots.Scared, []int{0, 1, 2, 3, 4}},
		{mascots.WakeUp, []int{0, 1, 2, 3}},
		{mascots.Greet, []int{0, 1}},
		{mascots.Pet, []int{0, 1, 2, 3, 4}},
		{mascots.Conduct, []int{0, 1, 2, 3}},
		{mascots.LookLeft, []int{0, 1, 2, 3}},
		{mascots.LookRight, []int{0, 1, 2, 3}},
	}
	totalRows := len(order) * len(modes)
	w := (cols*(mascots.Width+gap) + gap) * px
	h := (totalRows*(mascots.Height+gap) + gap) * px
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{30, 30, 36, 255})
		}
	}
	row := 0
	for _, n := range order {
		s := mascots.All[n]
		for _, md := range modes {
			for c, fr := range md.frames {
				rows, z := s.Frame(md.m, fr)
				ox := (gap + c*(mascots.Width+gap)) * px
				oy := (gap + row*(mascots.Height+gap)) * px
				paint := func(y, x int, col color.RGBA) {
					for dy := 0; dy < px; dy++ {
						for dx := 0; dx < px; dx++ {
							img.Set(ox+x*px+dx, oy+y*px+dy, col)
						}
					}
				}
				for y, f := range rows {
					for x, r := range f {
						if r == '.' {
							continue
						}
						col := hex(s.Palette[r])
						if md.m == mascots.Sleeping || md.m == mascots.Stuck {
							col = grayC(col)
						}
						paint(y, x, col)
					}
				}
				for _, p := range z {
					if p.Y < 0 || p.Y >= mascots.Height || p.X < 0 || p.X >= mascots.Width {
						continue
					}
					paint(p.Y, p.X, hex(p.Color))
				}
			}
			row++
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview:", err)
		os.Exit(1)
	}
	png.Encode(f, img)
	f.Close()
}
