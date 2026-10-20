// Package termimg downloads images and draws them in the terminal, either as
// real images (kitty graphics protocol with Unicode placeholders, used by kitty
// and Ghostty) or as colored half-block characters (any truecolor terminal).
package termimg

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // first frame of GIFs
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Mode is how images are drawn.
type Mode int

const (
	Off Mode = iota
	Blocks
	Kitty
)

func (m Mode) String() string { return [...]string{"off", "blocks", "kitty"}[m] }

// Detect picks the best mode for the current terminal.
// ROR2MM_IMAGES=off|blocks|kitty overrides it.
func Detect() Mode {
	switch strings.ToLower(os.Getenv("ROR2MM_IMAGES")) {
	case "off", "none", "0":
		return Off
	case "blocks":
		return Blocks
	case "kitty":
		return Kitty
	}
	term := strings.ToLower(os.Getenv("TERM"))
	prog := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	if os.Getenv("TMUX") == "" && (prog == "ghostty" || strings.Contains(term, "ghostty") ||
		strings.Contains(term, "kitty") || os.Getenv("KITTY_WINDOW_ID") != "") {
		return Kitty
	}
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if ct == "truecolor" || ct == "24bit" {
		return Blocks
	}
	return Off
}

// Skip reports whether an image URL is decoration not worth drawing (badges, SVGs).
func Skip(url string) bool {
	u := strings.ToLower(url)
	return strings.Contains(u, "shields.io") || strings.Contains(u, "badgen.net") ||
		strings.Contains(u, "badge") || strings.HasSuffix(strings.SplitN(u, "?", 2)[0], ".svg") ||
		strings.HasPrefix(u, "data:")
}

type entry struct {
	img    image.Image
	err    error
	kitty  map[[2]int]uint32
	blocks map[[2]int]string
}

// Store downloads, caches and renders images. Safe for concurrent use.
type Store struct {
	Mode Mode
	Dark bool
	bg   *color.NRGBA

	dir    string
	out    io.Writer
	mu     sync.Mutex
	items  map[string]*entry
	order  []string
	nextID uint32
	sem    chan struct{}
	client *http.Client
}

// NewStore creates a store caching files under dir. out receives kitty
// transmissions; it must be the same stream the screen is drawn on.
func NewStore(dir string, mode Mode, dark bool, out io.Writer) *Store {
	return &Store{Mode: mode, Dark: dark, dir: dir, out: out, items: map[string]*entry{},
		nextID: 0x4F0000, sem: make(chan struct{}, 6), client: &http.Client{Timeout: 30 * time.Second}}
}

// SetMode switches how images are drawn. Leaving kitty mode deletes the
// images already sent to the terminal.
func (s *Store) SetMode(mode Mode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Mode == mode {
		return
	}
	if s.Mode == Kitty {
		for _, e := range s.items {
			for _, id := range e.kitty {
				s.deleteKitty(id)
			}
			e.kitty = map[[2]int]uint32{}
		}
	}
	s.Mode = mode
}

// SetBackground sets the color half-transparent pixels blend into (hex) and
// forgets drawings made for the old one.
func (s *Store) SetBackground(hex string, dark bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Dark = dark
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.TrimPrefix(hex, "#"), "%02x%02x%02x", &r, &g, &b); err == nil {
		s.bg = &color.NRGBA{r, g, b, 255}
	}
	for _, e := range s.items {
		e.blocks = map[[2]int]string{}
	}
}

// Has reports whether url has been loaded (successfully or not).
func (s *Store) Has(url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[url]
	return ok
}

// Err returns the load error for url, if any.
func (s *Store) Err(url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.items[url]; ok {
		return e.err
	}
	return nil
}

// Bounds returns the pixel size of a loaded image.
func (s *Store) Bounds(url string) (int, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[url]
	if !ok || e.img == nil {
		return 0, 0, false
	}
	b := e.img.Bounds()
	return b.Dx(), b.Dy(), true
}

const maxBytes = 25 << 20
const maxPixels = 40_000_000

// Load fetches url (from disk cache when possible) and decodes it.
func (s *Store) Load(ctx context.Context, url string) error {
	if s.Has(url) {
		return s.Err(url)
	}
	s.sem <- struct{}{}
	img, err := s.load(ctx, url)
	<-s.sem
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[url] = &entry{img: img, err: err, kitty: map[[2]int]uint32{}, blocks: map[[2]int]string{}}
	s.order = append(s.order, url)
	// Keep memory in check: forget the oldest decoded images.
	for len(s.order) > 120 {
		old := s.order[0]
		s.order = s.order[1:]
		if e := s.items[old]; e != nil {
			for _, id := range e.kitty {
				s.deleteKitty(id)
			}
		}
		delete(s.items, old)
	}
	return err
}

func (s *Store) load(ctx context.Context, url string) (image.Image, error) {
	sum := sha1.Sum([]byte(url))
	path := filepath.Join(s.dir, hex.EncodeToString(sum[:]))
	data, err := os.ReadFile(path)
	if err != nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ror2mm/0.1")
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s", resp.Status)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxBytes {
			return nil, errors.New("image too large")
		}
		_ = os.MkdirAll(s.dir, 0o755)
		_ = os.WriteFile(path, data, 0o644)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("unsupported image: %w", err)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, errors.New("image too large")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// Big screenshots only ever get drawn small; shrink once to save memory.
	if b := img.Bounds(); b.Dx() > 1400 {
		h := b.Dy() * 1400 / b.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, 1400, max(1, h)))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	return img, nil
}

// FitSize picks a cell size for an image: about 8 pixels per column, no wider
// than maxCols and no taller than maxRows, keeping the aspect ratio
// (a terminal cell is roughly twice as tall as it is wide).
func FitSize(w, h, maxCols, maxRows int) (int, int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	cols := max(6, w/8)
	cols = min(cols, maxCols)
	rows := (cols*h + w) / (2 * w)
	if maxRows > 0 && rows > maxRows {
		rows = maxRows
		cols = max(1, rows*2*w/h)
	}
	return cols, max(1, rows)
}

// Render returns the image drawn in cols x rows cells, one string per line.
// It returns nil if the image is not loaded or failed.
func (s *Store) Render(url string, cols, rows int) []string {
	if s.Mode == Off || cols <= 0 || rows <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[url]
	if !ok || e.img == nil {
		return nil
	}
	key := [2]int{cols, rows}
	if s.Mode == Kitty {
		id, ok := e.kitty[key]
		if !ok {
			s.nextID++
			id = s.nextID
			if err := s.transmit(id, e.img, cols, rows); err != nil {
				return nil
			}
			e.kitty[key] = id
		}
		return placeholders(id, cols, rows)
	}
	if str, ok := e.blocks[key]; ok {
		return strings.Split(str, "\n")
	}
	str := halfBlocks(e.img, cols, rows, s.Dark, s.bg)
	e.blocks[key] = str
	return strings.Split(str, "\n")
}

// ---------- half blocks ----------

func halfBlocks(img image.Image, cols, rows int, dark bool, themeBg *color.NRGBA) string {
	dst := image.NewNRGBA(image.Rect(0, 0, cols, rows*2))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	bg := color.NRGBA{30, 30, 30, 255}
	if !dark {
		bg = color.NRGBA{255, 255, 255, 255}
	}
	if themeBg != nil {
		bg = *themeBg
	}
	px := func(x, y int) (color.NRGBA, bool) {
		c := dst.NRGBAAt(x, y)
		if c.A < 100 {
			return c, false
		}
		if c.A < 255 {
			a := int(c.A)
			blend := func(f, b uint8) uint8 { return uint8((int(f)*a + int(b)*(255-a)) / 255) }
			c = color.NRGBA{blend(c.R, bg.R), blend(c.G, bg.G), blend(c.B, bg.B), 255}
		}
		return c, true
	}
	var b strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			top, tOK := px(x, 2*y)
			bot, bOK := px(x, 2*y+1)
			switch {
			case tOK && bOK:
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.R, top.G, top.B, bot.R, bot.G, bot.B)
			case tOK:
				fmt.Fprintf(&b, "\x1b[49m\x1b[38;2;%d;%d;%dm▀", top.R, top.G, top.B)
			case bOK:
				fmt.Fprintf(&b, "\x1b[49m\x1b[38;2;%d;%d;%dm▄", bot.R, bot.G, bot.B)
			default:
				b.WriteString("\x1b[0m ")
			}
		}
		b.WriteString("\x1b[0m")
		if y < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// ---------- kitty ----------

func (s *Store) transmit(id uint32, img image.Image, cols, rows int) error {
	if s.out == nil {
		return errors.New("no output")
	}
	// Send roughly what will be shown (cells are ~10x20 px) to keep it quick.
	b := img.Bounds()
	tw := min(b.Dx(), cols*12)
	if tw < b.Dx() {
		th := max(1, b.Dy()*tw/b.Dx())
		dst := image.NewNRGBA(image.Rect(0, 0, tw, th))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	enc := base64.StdEncoding.EncodeToString(buf.Bytes())
	var out strings.Builder
	const chunk = 4096
	for i := 0; i < len(enc); i += chunk {
		end := min(i+chunk, len(enc))
		more := 0
		if end < len(enc) {
			more = 1
		}
		if i == 0 {
			fmt.Fprintf(&out, "\x1b_Ga=T,U=1,f=100,q=2,i=%d,c=%d,r=%d,m=%d;%s\x1b\\", id, cols, rows, more, enc[i:end])
		} else {
			fmt.Fprintf(&out, "\x1b_Gm=%d;%s\x1b\\", more, enc[i:end])
		}
	}
	_, err := io.WriteString(s.out, out.String())
	return err
}

func (s *Store) deleteKitty(id uint32) {
	if s.out != nil {
		fmt.Fprintf(s.out, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
	}
}

// Cleanup removes every image this store sent to the terminal.
func (s *Store) Cleanup() {
	if s.Mode != Kitty || s.out == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.items {
		for _, id := range e.kitty {
			s.deleteKitty(id)
		}
	}
}

// placeholders builds the Unicode placeholder cells that show image id.
func placeholders(id uint32, cols, rows int) []string {
	rows = min(rows, len(diacritics))
	cols = min(cols, len(diacritics))
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	lines := make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		b.WriteString(fg)
		for c := 0; c < cols; c++ {
			b.WriteRune(0x10EEEE)
			b.WriteRune(diacritics[r])
			b.WriteRune(diacritics[c])
		}
		b.WriteString("\x1b[39m")
		lines[r] = b.String()
	}
	return lines
}

// LockedWriter serializes writes so image transmissions never land in the
// middle of a screen redraw. It also passes through the terminal file
// descriptor so Bubble Tea still treats it as a terminal.
type LockedWriter struct {
	mu sync.Mutex
	F  *os.File
}

func (w *LockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.F.Write(p)
}
func (w *LockedWriter) Read(p []byte) (int, error) { return w.F.Read(p) }
func (w *LockedWriter) Close() error               { return nil }
func (w *LockedWriter) Fd() uintptr                { return w.F.Fd() }
