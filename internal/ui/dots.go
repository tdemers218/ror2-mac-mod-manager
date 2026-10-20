package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Decor turns the dotted accents on: braille bars, sparklines, dotted rules,
// leaders and pictures. Off gives the plain look.
var Decor = true

// ---------- braille canvas ----------

// Braille characters pack a 2×4 grid of dots into one cell. These are the bit
// values for each dot, indexed [y][x].
var brailleBits = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

// Canvas is a grid of braille dots. Each cell remembers the strongest "ink"
// drawn into it, which picks its color.
type Canvas struct {
	W, H  int // in cells
	cells []rune
	ink   []int
}

// NewCanvas makes an empty canvas of w×h cells (2w×4h dots).
func NewCanvas(w, h int) *Canvas {
	return &Canvas{W: w, H: h, cells: make([]rune, w*h), ink: make([]int, w*h)}
}

// Set turns on the dot at (x, y), counted in dots, with the given ink level.
func (c *Canvas) Set(x, y, ink int) {
	if x < 0 || y < 0 || x >= c.W*2 || y >= c.H*4 {
		return
	}
	i := (y/4)*c.W + x/2
	c.cells[i] |= brailleBits[y%4][x%2]
	if ink > c.ink[i] {
		c.ink[i] = ink
	}
}

// Render draws the canvas. color picks a style for each non-empty cell from
// its position (0..1 across, 0..1 down) and ink level.
func (c *Canvas) Render(color func(fx, fy float64, ink int) lipgloss.Style) string {
	lines := make([]string, c.H)
	for y := 0; y < c.H; y++ {
		var b strings.Builder
		for x := 0; x < c.W; x++ {
			i := y*c.W + x
			if c.cells[i] == 0 {
				b.WriteByte(' ')
				continue
			}
			ch := string(0x2800 + c.cells[i])
			fx, fy := float64(x)/math.Max(1, float64(c.W-1)), float64(y)/math.Max(1, float64(c.H-1))
			b.WriteString(color(fx, fy, c.ink[i]).Render(ch))
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

// bayer is a 4×4 ordered-dither matrix; comparing brightness against it turns
// shading into an even dot pattern.
var bayer = [4][4]float64{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}

func dither(x, y int, v float64) bool { return v > (bayer[y%4][x%4]+0.5)/16 }

// hash gives a stable pseudo-random number in [0,1) for a position.
func hash(x, y, seed int) float64 {
	h := uint32(x*374761393 + y*668265263 + seed*2147483647)
	h = (h ^ (h >> 13)) * 1274126177
	h ^= h >> 16
	return float64(h%10000) / 10000
}

// Planet draws a ringed planet with a sprinkle of stars, shaded with dots,
// in w×h cells. It reads as Petrichor V at a glance.
func Planet(w, h int) string { return PlanetAt(w, h, Phase()) }

// PlanetAt draws the planet with its cloud bands turned by phase (radians).
func PlanetAt(w, h int, phase float64) string {
	return planetCanvas(w, h, phase, true).Render(planetColor)
}

func planetColor(fx, fy float64, ink int) lipgloss.Style {
	switch ink {
	case 1:
		return lipgloss.NewStyle().Foreground(Faint)
	case 2:
		return lipgloss.NewStyle().Foreground(Blend(Equipment, Void, fx))
	}
	return lipgloss.NewStyle().Foreground(Blend(Void, Equipment, (fx+fy)/2))
}

func planetCanvas(w, h int, phase float64, stars bool) *Canvas {
	c := NewCanvas(w, h)
	W, H := float64(w*2), float64(h*4)
	cx, cy := W*0.5, H*0.5
	r := math.Min(W*0.22, H*0.40)
	const tilt = -0.18
	ringAt := func(t float64) (float64, float64) {
		x, y := math.Cos(t)*r*2.05, math.Sin(t)*r*0.36
		return cx + x*math.Cos(tilt) - y*math.Sin(tilt), cy + x*math.Sin(tilt) + y*math.Cos(tilt)
	}
	// Stars first so the planet covers them.
	for y := 0; y < h*4 && stars; y++ {
		for x := 0; x < w*2; x++ {
			if hash(x, y, 7) < 0.012 {
				c.Set(x, y, 1)
			}
		}
	}
	// Planet body, lit from the upper left, with a few bands.
	lx, ly, lz := -0.55, -0.5, 0.67
	for y := 0; y < h*4; y++ {
		for x := 0; x < w*2; x++ {
			dx, dy := (float64(x)+0.5-cx)/r, (float64(y)+0.5-cy)/r
			d := dx*dx + dy*dy
			if d > 1 {
				continue
			}
			dz := math.Sqrt(1 - d)
			lit := math.Max(0, dx*lx+dy*ly+dz*lz)
			v := 0.05 + 1.25*lit*lit
			// Bands drift sideways as the planet turns.
			lon := math.Atan2(dx, dz) + phase
			if math.Abs(math.Sin(dy*5.5+math.Sin(lon*2)*0.6)) < 0.18 {
				v *= 0.45 // darker cloud bands
			}
			if d > 0.86 && dx*lx+dy*ly > 0 {
				v = 1 // bright rim on the lit side
			}
			if dither(x, y, v) {
				c.Set(x, y, 3)
			}
		}
	}
	// Ring: the far half hides behind the planet.
	for i := 0; i < 900; i++ {
		t := float64(i) / 900 * 2 * math.Pi
		for _, scale := range []float64{1, 0.93, 0.8} {
			x, y := ringAt(t)
			x, y = cx+(x-cx)*scale, cy+(y-cy)*scale
			dx, dy := (x-cx)/r, (y-cy)/r
			behind := math.Sin(t) < 0 && dx*dx+dy*dy < 1
			if !behind {
				c.Set(int(x), int(y), 2)
			}
		}
	}
	return c
}

// ---------- bars, meters and charts ----------

// Bar draws a progress bar that fades from the highlight to the accent color.
func Bar(frac float64, width int) string {
	frac = math.Max(0, math.Min(1, frac))
	if width <= 0 {
		return ""
	}
	full, part, empty := "━", "", "━"
	units := int(frac*float64(width) + 0.5)
	if Decor {
		// Two columns of dots per cell gives half-cell steps.
		full, empty = "⣿", "⣀"
		halves := int(frac*float64(width*2) + 0.5)
		units = halves / 2
		if halves%2 == 1 {
			part = "⡇"
		}
	}
	var b strings.Builder
	for i := 0; i < units; i++ {
		c := Blend(Void, Equipment, at(i, width))
		if Animate {
			// A glint runs along the filled part.
			g := math.Pow(math.Max(0, math.Sin(float64(i)*0.35-Phase()*5)), 12)
			c = Blend(c, Text, g*0.7)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render(full))
	}
	rest := width - units
	if part != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(Blend(Void, Equipment, at(units, width))).Render(part))
		rest--
	}
	b.WriteString(lipgloss.NewStyle().Foreground(Faint).Render(strings.Repeat(empty, max(0, rest))))
	return b.String()
}

func at(i, n int) float64 {
	if n <= 1 {
		return 0
	}
	return float64(i) / float64(n-1)
}

// Meter splits a small dotted gauge into its filled and empty parts, so a
// caller can color each one. cells is the width.
func Meter(frac float64, cells int) (filled, empty string) {
	frac = math.Max(0, math.Min(1, frac))
	halves := int(frac*float64(cells*2) + 0.5)
	if frac > 0 && halves == 0 {
		halves = 1
	}
	filled = strings.Repeat("⣿", halves/2)
	n := halves / 2
	if halves%2 == 1 {
		filled += "⡇"
		n++
	}
	return filled, strings.Repeat("⣀", max(0, cells-n))
}

// LogFrac places n on a log scale from floor (empty) to top (full).
func LogFrac(n, floor, top float64) float64 {
	if n <= floor || top <= floor {
		return 0
	}
	return math.Min(1, math.Log10(n/floor)/math.Log10(top/floor))
}

// Spark draws values as a braille bar chart, width cells wide and rows cells
// tall, oldest on the left.
func Spark(values []float64, width, rows int) string {
	if len(values) == 0 || width <= 0 || rows <= 0 {
		return ""
	}
	cols := width * 2
	// Resample to one value per dot column, keeping peaks.
	buckets := make([]float64, cols)
	if len(values) >= cols {
		for i, v := range values {
			j := i * cols / len(values)
			buckets[j] = math.Max(buckets[j], v)
		}
	} else {
		// Few values: widen each into equal steps, flush right.
		step := max(1, cols/len(values))
		start := cols - step*len(values)
		for i, v := range values {
			for k := 0; k < step-boolInt(step > 2); k++ {
				buckets[start+i*step+k] = v
			}
		}
	}
	top := 0.0
	for _, v := range buckets {
		top = math.Max(top, v)
	}
	c := NewCanvas(width, rows)
	dots := rows * 4
	for x, v := range buckets {
		if top <= 0 {
			break
		}
		// Square root keeps small versions visible next to a big one.
		hgt := int(math.Round(math.Sqrt(v/top) * float64(dots)))
		if v > 0 && hgt == 0 {
			hgt = 1
		}
		for y := 0; y < hgt; y++ {
			c.Set(x, dots-1-y, 1)
		}
	}
	// Baseline dots so empty stretches still read as a chart.
	for x := 0; x < cols; x += 2 {
		c.Set(x, dots-1, 0)
	}
	return c.Render(func(fx, fy float64, ink int) lipgloss.Style {
		if ink == 0 {
			return lipgloss.NewStyle().Foreground(Faint)
		}
		return lipgloss.NewStyle().Foreground(Blend(Void, Equipment, fx))
	})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- rules, leaders, stars ----------

// Rule is a dotted divider, optionally with a label set into it.
func Rule(label string, width int) string {
	ch := "┄"
	if !Decor {
		ch = "─"
	}
	if label == "" {
		return Fainter.Render(strings.Repeat(ch, max(0, width)))
	}
	head := lipgloss.NewStyle().Foreground(Void).Render("⠶ ") + Bold.Render(label) + " "
	return head + Fainter.Render(strings.Repeat(ch, max(0, width-lipgloss.Width(head))))
}

// Stars is a faint, sparse field of single braille dots, width cells wide.
// The same seed always gives the same sky; with animations on, it twinkles.
func Stars(width, seed int) string {
	t := Phase()
	if !Decor || width <= 0 {
		return strings.Repeat(" ", max(0, width))
	}
	dots := []rune("⠁⠂⠄⠈⠐⠠⡀⢀")
	var b strings.Builder
	for x := 0; x < width; x++ {
		if v := hash(x, seed, 3); v < 0.09 && x > 0 && x < width-1 {
			// Each star cycles through dot positions at its own pace.
			k := int(v*1000) + int(t*(0.5+hash(x, seed, 5)))
			b.WriteRune(dots[k%len(dots)])
		} else {
			b.WriteByte(' ')
		}
	}
	return Fainter.Render(b.String())
}

// ---------- borders ----------

// DottedBorder is a rounded frame drawn with dashed lines.
func DottedBorder() lipgloss.Border {
	return lipgloss.Border{Top: "┄", Bottom: "┄", Left: "┆", Right: "┆",
		TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯",
		MiddleLeft: "├", MiddleRight: "┤", Middle: "┼", MiddleTop: "┬", MiddleBottom: "┴"}
}

// BrailleBorder is a fine frame made of braille dots.
func BrailleBorder() lipgloss.Border {
	return lipgloss.Border{Top: "⠉", Bottom: "⣀", Left: "⡇", Right: "⢸",
		TopLeft: "⡏", TopRight: "⢹", BottomLeft: "⣇", BottomRight: "⣸",
		MiddleLeft: "⡇", MiddleRight: "⢸", Middle: "⣿", MiddleTop: "⡏", MiddleBottom: "⣇"}
}
