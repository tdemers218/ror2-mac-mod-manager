package ui

// Backdrops paint a faint pattern, picture or text art into the empty cells
// of the screen, behind everything else. Text is never covered: only plain
// spaces with no background color of their own are painted.

import (
	"fmt"
	"image"
	_ "image/gif" // decoders for backdrop pictures
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Backdrop kinds.
var BackdropKinds = []string{"off", "stars", "rain", "dots", "grid", "waves", "planet", "image", "image-blocks", "text"}

// Backdrop is the background in effect.
type Backdrop struct {
	Kind     string
	Strength float64 // 0..1, how far painted cells move from the background color
	Image    string  // picture file for "image" and "image-blocks"
	Text     string  // text art file for "text"
	Age      float64 // seconds since a "burst" started

	cacheKey string
	cells    []bdCell
	w, h     int
	err      error
}

type bdCell struct {
	ch     string
	fg, bg colorful.Color
	hasBg  bool
	set    bool
}

// Err is the problem loading the picture or text file, if any.
func (b *Backdrop) Err() error { return b.err }

// Paint draws the backdrop into lines (the body of the screen, w×h cells).
// skip reports cells to leave alone, such as the inside of a dialog.
func (b *Backdrop) Paint(lines []string, w, h int, skip func(x, y int) bool) {
	if b == nil || b.Kind == "" || b.Kind == "off" || w <= 0 || h <= 0 {
		return
	}
	b.prepare(w, h)
	if b.cells == nil {
		return
	}
	bg, err := colorful.Hex(string(Bg))
	if err != nil {
		return
	}
	for y := range lines {
		if y >= h {
			break
		}
		lines[y] = b.paintLine(lines[y], y, bg, skip)
	}
}

// prepare fills b.cells for this frame. Still pictures are cached; moving
// patterns are drawn fresh each time.
func (b *Backdrop) prepare(w, h int) {
	key := fmt.Sprintf("%s|%s|%s|%d|%d|%s|%v", b.Kind, b.Image, b.Text, w, h, Current.Name, b.Strength)
	still := b.Kind != "burst" && (b.Kind == "image" || b.Kind == "image-blocks" || b.Kind == "text" || b.Kind == "grid" || !Animate)
	if still && key == b.cacheKey && b.w == w && b.h == h {
		return
	}
	b.cacheKey, b.w, b.h, b.err = key, w, h, nil
	b.cells = make([]bdCell, w*h)
	t := Phase()
	switch b.Kind {
	case "stars":
		b.drawStars(t)
	case "rain":
		b.drawRain(t)
	case "dots":
		b.drawDots(t)
	case "grid":
		b.drawGrid()
	case "waves":
		b.drawWaves(t)
	case "planet":
		b.drawPlanet(t)
	case "burst":
		b.drawBurst()
	case "image", "image-blocks":
		if err := b.drawImage(b.Kind == "image-blocks"); err != nil {
			b.err = err
			b.cells = nil
		}
	case "text":
		if err := b.drawText(); err != nil {
			b.err = err
			b.cells = nil
		}
	}
}

func (b *Backdrop) put(x, y int, ch string, c colorful.Color) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	b.cells[y*b.w+x] = bdCell{ch: ch, fg: c, set: true}
}

// tone mixes the theme background toward a theme color by the strength,
// scaled by k (so brighter things can stand out a little more).
func (b *Backdrop) tone(col string, k float64) colorful.Color {
	bg, _ := colorful.Hex(string(Bg))
	c, err := colorful.Hex(col)
	if err != nil {
		return bg
	}
	return bg.BlendLab(c, math.Min(1, b.Strength*k)).Clamped()
}

func (b *Backdrop) drawStars(t float64) {
	dots := []string{"⠁", "⠂", "⠄", "⠈", "⠐", "⠠", "⡀", "⢀"}
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			v := hash(x, y, 21)
			if v > 0.035 {
				continue
			}
			// Twinkle: brightness rises and falls at each star's own pace.
			tw := 0.55 + 0.45*math.Sin(t*(0.8+hash(x, y, 4)*1.5)+v*600)
			ch := dots[int(v*10000)%len(dots)]
			col := string(Muted)
			if v < 0.004 {
				ch, col = "✦", string(Equipment)
				if tw < 0.5 {
					ch = "✧"
				}
			}
			b.put(x, y, ch, b.tone(col, 0.6+tw*0.9))
		}
	}
}

// drawRain is falling, slanted rain: it is Risk of Rain after all.
func (b *Backdrop) drawRain(t float64) {
	for x := 0; x < b.w; x++ {
		if hash(x, 0, 31) > 0.33 {
			continue
		}
		speed := 6 + hash(x, 1, 31)*8
		span := float64(b.h + 8)
		head := math.Mod(hash(x, 2, 31)*span+t*speed, span) - 4
		length := 2 + int(hash(x, 3, 31)*3)
		for k := 0; k < length; k++ {
			y := int(head) - k
			// Drift one column left every few rows for a slant.
			xx := x - (y+int(t*speed))/6%2
			ch := "⡇"
			if k == 0 {
				ch = "⡆"
			} else if k == length-1 {
				ch = "⠃"
			}
			b.put(xx, y, ch, b.tone(string(Lunar), 1.2-float64(k)*0.25))
		}
	}
}

func (b *Backdrop) drawDots(t float64) {
	for y := 0; y < b.h; y += 2 {
		for x := (y / 2 % 2) * 2; x < b.w; x += 4 {
			// A slow diagonal swell of brightness passes over the grid.
			wave := 0.5 + 0.5*math.Sin(float64(x)*0.08+float64(y)*0.15-t*1.2)
			ch := "⠄"
			if wave > 0.8 {
				ch = "⠆"
			}
			b.put(x, y, ch, b.tone(string(Void), 0.5+wave))
		}
	}
}

func (b *Backdrop) drawGrid() {
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			onRow, onCol := y%4 == 3, x%12 == 6
			switch {
			case onRow && onCol:
				b.put(x, y, "┼", b.tone(string(Muted), 1))
			case onRow:
				b.put(x, y, "┈", b.tone(string(Muted), 0.6))
			case onCol:
				b.put(x, y, "┊", b.tone(string(Muted), 0.6))
			}
		}
	}
}

// drawWaves draws a few dotted sine waves rolling across the screen.
func (b *Backdrop) drawWaves(t float64) {
	c := NewCanvas(b.w, b.h)
	H := float64(b.h * 4)
	n := max(2, b.h/7)
	for i := 0; i < n; i++ {
		base := H * (float64(i) + 0.5) / float64(n)
		amp := H / float64(n) * 0.28
		k := 0.035 + 0.01*float64(i%3)
		for x := 0; x < b.w*2; x++ {
			y := base + amp*math.Sin(float64(x)*k+t*(0.6+0.2*float64(i))+float64(i))
			c.Set(x, int(y), 1+i%2)
		}
	}
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			i := y*b.w + x
			if c.cells[i] == 0 {
				continue
			}
			col := string(Void)
			if c.ink[i] == 2 {
				col = string(Equipment)
			}
			b.put(x, y, string(rune(0x2800)+c.cells[i]), b.tone(col, 1))
		}
	}
}

// drawPlanet sets a big, slowly turning planet in the lower right.
func (b *Backdrop) drawPlanet(t float64) {
	pw, ph := min(b.w*2/3, 90), min(b.h-2, 24)
	if pw < 10 || ph < 5 {
		return
	}
	c := planetCanvas(pw, ph, t*0.3, false)
	ox, oy := b.w-pw-1, b.h-ph
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			i := y*pw + x
			if c.cells[i] == 0 {
				continue
			}
			fx, fy := float64(x)/float64(pw), float64(y)/float64(ph)
			col := string(Blend(Void, Equipment, (fx+fy)/2))
			if c.ink[i] == 2 {
				col = string(Blend(Equipment, Void, fx))
			}
			b.put(ox+x, oy+y, string(rune(0x2800)+c.cells[i]), b.tone(col, 1))
		}
	}
	b.drawStarsSparse(t)
}

func (b *Backdrop) drawStarsSparse(t float64) {
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			if v := hash(x, y, 9); v < 0.012 && !b.cells[y*b.w+x].set {
				tw := 0.6 + 0.4*math.Sin(t*(1+hash(x, y, 2))+v*900)
				b.put(x, y, "⠂", b.tone(string(Muted), tw))
			}
		}
	}
}

// drawBurst is a one-off shower of sparkles from the middle of the screen,
// pulled down by gravity and fading out.
func (b *Backdrop) drawBurst() {
	const life = 1.4
	t := b.Age
	if t > life {
		return
	}
	cols := []string{string(Equipment), string(Void), string(Uncommon), string(Boss), string(Lunar)}
	ox, oy := float64(b.w)/2, float64(b.h)*0.42
	for i := 0; i < 90; i++ {
		a := hash(i, 1, 77) * 2 * math.Pi
		speed := 8 + hash(i, 2, 77)*34
		vx := math.Cos(a) * speed
		vy := math.Sin(a)*speed*0.5 - 6 // rows are tall: slower, with a lift
		x := ox + vx*t
		y := oy + vy*t + 9*t*t
		fade := 1 - t/life
		ch := "✦"
		switch {
		case t > life*0.66:
			ch = "⠂"
		case t > life*0.33:
			ch = "✧"
		}
		b.put(int(x), int(y), ch, b.tone(cols[i%len(cols)], fade))
	}
}

// drawImage fits a picture to the screen (cropping the edges) and draws it
// as braille dots tinted with the picture's colors, or as soft half-blocks.
func (b *Backdrop) drawImage(blocks bool) error {
	if b.Image == "" {
		return fmt.Errorf("no picture chosen")
	}
	f, err := os.Open(b.Image)
	if err != nil {
		return err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("could not read the picture: %w", err)
	}
	dw, dh := b.w*2, b.h*4 // dot grid, roughly square dots
	if blocks {
		dw, dh = b.w, b.h*2
	}
	// Cover: scale to fill, crop the overflow evenly.
	sb := src.Bounds()
	scale := math.Max(float64(dw)/float64(sb.Dx()), float64(dh)/float64(sb.Dy()))
	cw, ch := int(float64(dw)/scale), int(float64(dh)/scale)
	crop := image.Rect(sb.Min.X+(sb.Dx()-cw)/2, sb.Min.Y+(sb.Dy()-ch)/2, 0, 0)
	crop.Max = crop.Min.Add(image.Pt(cw, ch))
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)
	px := func(x, y int) colorful.Color {
		o := dst.PixOffset(x, y)
		return colorful.Color{R: float64(dst.Pix[o]) / 255, G: float64(dst.Pix[o+1]) / 255, B: float64(dst.Pix[o+2]) / 255}
	}
	bg, _ := colorful.Hex(string(Bg))
	mix := func(c colorful.Color, k float64) colorful.Color {
		return bg.BlendLab(c, math.Min(1, b.Strength*k)).Clamped()
	}
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			if blocks {
				top, bot := px(x, y*2), px(x, y*2+1)
				b.cells[y*b.w+x] = bdCell{ch: "▀", fg: mix(top, 1.6), bg: mix(bot, 1.6), hasBg: true, set: true}
				continue
			}
			var bits rune
			var sum colorful.Color
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					c := px(x*2+dx, y*4+dy)
					sum.R, sum.G, sum.B = sum.R+c.R, sum.G+c.G, sum.B+c.B
					_, _, l := c.Hcl()
					if Current.Light {
						l = 1 - l
					}
					if dither(x*2+dx, y*4+dy, l*1.1) {
						bits |= brailleBits[dy][dx]
					}
				}
			}
			if bits == 0 {
				continue
			}
			avg := colorful.Color{R: sum.R / 8, G: sum.G / 8, B: sum.B / 8}
			b.put(x, y, string(rune(0x2800)+bits), mix(avg, 2.4))
		}
	}
	return nil
}

// drawText centers a text art file on the screen, colored along the theme
// gradient.
func (b *Backdrop) drawText() error {
	if b.Text == "" {
		return fmt.Errorf("no text art file chosen")
	}
	data, err := os.ReadFile(b.Text)
	if err != nil {
		return err
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("the text art file is not plain text")
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(data), "\t", "    "), "\n"), "\n")
	tw := 0
	for _, l := range lines {
		tw = max(tw, ansi.StringWidth(l))
	}
	ox, oy := (b.w-tw)/2, (b.h-len(lines))/2
	for y, l := range lines {
		x := 0
		for _, r := range l {
			rw := max(1, ansi.StringWidth(string(r)))
			if r != ' ' && rw == 1 {
				fx := float64(x) / math.Max(1, float64(tw))
				b.put(ox+x, oy+y, string(r), b.tone(string(Blend(Void, Equipment, fx)), 1.4))
			}
			x += rw
		}
	}
	return nil
}

// paintLine walks one styled line cell by cell and swaps plain spaces for
// backdrop cells. It tracks the active styling so it can restore it.
func (b *Backdrop) paintLine(line string, y int, _ colorful.Color, skip func(x, y int) bool) string {
	var out strings.Builder
	out.Grow(len(line) + 64)
	active := ""   // SGR sequences in effect since the last reset
	bgSet := false // a background color is in effect
	// Only paint spaces with two spaces on each side, so the gaps between
	// words (and some breathing room around text) stay clear.
	plain := []rune(ansi.Strip(line))
	blank := make([]bool, b.w+4)
	cx := 0
	for _, r := range plain {
		rw := ansi.StringWidth(string(r))
		if cx < b.w {
			blank[cx+2] = r == ' '
		}
		cx += rw
	}
	for k := cx + 2; k < len(blank); k++ {
		blank[k] = true
	}
	blank[0], blank[1] = true, true
	x := 0
	for i := 0; i < len(line); {
		c := line[i]
		if c == 0x1b && i+1 < len(line) {
			j := escEnd(line, i)
			seq := line[i:j]
			out.WriteString(seq)
			if line[i+1] == '[' && seq[len(seq)-1] == 'm' {
				switch sgrBg(seq) {
				case -1:
					bgSet, active = false, ""
					if !isFullReset(seq) {
						active = seq
					}
				case 1:
					bgSet = true
					active += seq
				default:
					active += seq
				}
			}
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if r == ' ' && !bgSet && x < b.w && blank[x] && blank[x+1] && blank[x+3] && blank[x+4] && (skip == nil || !skip(x, y)) {
			if cell := b.cells[y*b.w+x]; cell.set {
				out.WriteString("\x1b[0m")
				out.WriteString(fgSeq(cell.fg))
				if cell.hasBg {
					out.WriteString(bgSeq(cell.bg))
				}
				out.WriteString(cell.ch)
				out.WriteString("\x1b[0m")
				out.WriteString(active)
				i += size
				x++
				continue
			}
		}
		out.WriteString(line[i : i+size])
		i += size
		x += ansi.StringWidth(string(r)) // combining marks count as 0
	}
	return out.String()
}

// escEnd finds where the escape sequence starting at i ends.
func escEnd(s string, i int) int {
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[':
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
	case ']', 'P', '_':
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
	default:
		return i + 2
	}
	return len(s)
}

func isFullReset(seq string) bool { return seq == "\x1b[m" || seq == "\x1b[0m" }

// sgrBg says what an SGR sequence does to the background: -1 resets it,
// 1 sets it, 0 leaves it alone.
func sgrBg(seq string) int {
	params := strings.Split(strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m"), ";")
	state := 0
	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == "" || p == "0" || p == "49":
			state = -1
		case p == "38" || p == "48" || p == "58":
			if p == "48" {
				state = 1
			}
			if i+1 < len(params) && params[i+1] == "5" {
				i += 2
			} else if i+1 < len(params) && params[i+1] == "2" {
				i += 4
			}
		case len(p) == 2 && p[0] == '4':
			state = 1
		case len(p) == 3 && p[:2] == "10":
			state = 1
		case p == "7":
			state = 1 // reverse video paints a background
		}
	}
	return state
}

func fgSeq(c colorful.Color) string {
	r, g, b := c.RGB255()
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

func bgSeq(c colorful.Color) string {
	r, g, b := c.RGB255()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

// RoundMore redraws the frame of a box drawn with ╭╮╰╯ corners as a fine
// line of braille dots with real quarter-circle corners, about r dots across
// (a cell is 2 dots wide and 4 tall, so dots are roughly square). Box-drawing
// characters only offer one tiny curve; dots are the only way to draw a
// bigger, smooth one. Text set into the frame (like a panel title) is kept.
func RoundMore(box string, color string) string { return roundFrame(box, color, 7) }

func roundFrame(box string, color string, r int) string {
	lines := strings.Split(box, "\n")
	n := len(lines)
	if n < 3 {
		return box
	}
	w := ansi.StringWidth(lines[0])
	if w < 4 {
		return box
	}
	cells := make([][]string, n)
	for y, l := range lines {
		if ansi.StringWidth(l) != w {
			return box
		}
		if y == 0 || y == n-1 {
			cells[y] = make([]string, w)
			for x := 0; x < w; x++ {
				cells[y][x] = ansi.Strip(ansi.Cut(l, x, x+1))
			}
		} else {
			cells[y] = []string{ansi.Strip(ansi.Cut(l, 0, 1)), ansi.Strip(ansi.Cut(l, w-1, w))}
		}
	}
	if cells[0][0] != "╭" || cells[0][w-1] != "╮" || cells[n-1][0] != "╰" || cells[n-1][w-1] != "╯" {
		return box
	}
	// Keep the corner inside the frame cells on small boxes.
	r = min(r, w-1, 2*n-1)
	W, H := w*2, n*4
	c := NewCanvas(w, n)
	for x := r; x < W-r; x++ {
		c.Set(x, 0, 1)
		c.Set(x, H-1, 1)
	}
	for y := r; y < H-r; y++ {
		c.Set(0, y, 1)
		c.Set(W-1, y, 1)
	}
	R := float64(r)
	corners := [4][3]float64{
		{R, R, math.Pi},                         // top left
		{float64(W-1) - R, R, 1.5 * math.Pi},    // top right
		{float64(W-1) - R, float64(H-1) - R, 0}, // bottom right
		{R, float64(H-1) - R, 0.5 * math.Pi},    // bottom left
	}
	for _, k := range corners {
		for i := 0; i <= 64; i++ {
			a := k[2] + float64(i)/64*math.Pi/2
			c.Set(int(math.Round(k[0]+R*math.Cos(a))), int(math.Round(k[1]+R*math.Sin(a))), 1)
		}
	}
	col, err := colorful.Hex(color)
	st := func(s string) string {
		if err != nil {
			return s
		}
		return fgSeq(col) + s + "\x1b[39m"
	}
	dot := func(x, y int) string {
		if b := c.cells[y*w+x]; b != 0 {
			return st(string(rune(0x2800) + b))
		}
		return " "
	}
	isFrame := func(s string) bool { return strings.Contains("─│╭╮╰╯", s) && s != "" }
	for y := 0; y < n; y++ {
		l := lines[y]
		if y == 0 || y == n-1 {
			var b strings.Builder
			for x := 0; x < w; x++ {
				if isFrame(cells[y][x]) {
					b.WriteString(dot(x, y))
				} else {
					b.WriteString(ansi.Cut(l, x, x+1))
				}
			}
			lines[y] = b.String()
			continue
		}
		left, right := ansi.Cut(l, 0, 1), ansi.Cut(l, w-1, w)
		if isFrame(cells[y][0]) {
			left = dot(0, y)
		}
		if isFrame(cells[y][1]) {
			right = dot(w-1, y)
		}
		lines[y] = left + ansi.Cut(l, 1, w-1) + right
	}
	return strings.Join(lines, "\n")
}
