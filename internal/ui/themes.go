package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
)

// Theme is a full color scheme. All colors are hex.
type Theme struct {
	Name  string
	About string
	Light bool

	Bg, Surface, Text, Muted, Faint    string
	Accent, Sel, OK, Warn, Bad, Info   string
	SelBg, Border, BorderFocus, PillFg string
}

// Themes lists every built-in theme. The first five are Risk of Rain 2 inspired.
var Themes = []Theme{
	{Name: "Risk of Rain", About: "Item-tier colors: equipment orange, void purple, uncommon green.",
		Bg: "#15121C", Surface: "#1E1A28", Text: "#E8E6EE", Muted: "#9A94A8", Faint: "#5A5468",
		Accent: "#FF9F43", Sel: "#C77DFF", OK: "#8BD450", Warn: "#F2C94C", Bad: "#FF5F57", Info: "#5AA9FF",
		SelBg: "#2E2342", Border: "#383148", BorderFocus: "#5B4880", PillFg: "#15121C"},
	{Name: "Lunar", About: "Cold blues of the Lunar items and the Moon.",
		Bg: "#0D1320", Surface: "#141C2E", Text: "#DDE7F5", Muted: "#8C9BB3", Faint: "#4A5670",
		Accent: "#6FB7FF", Sel: "#7AE0FF", OK: "#7FD8A6", Warn: "#F5D76E", Bad: "#FF6B81", Info: "#9FB4FF",
		SelBg: "#1B2A45", Border: "#263552", BorderFocus: "#3D5A8C", PillFg: "#0D1320"},
	{Name: "Void", About: "The Void Fields: magenta and violet on deep black.",
		Bg: "#120A18", Surface: "#1C1024", Text: "#EEDDF3", Muted: "#A88DB5", Faint: "#5E4669",
		Accent: "#FF5FD2", Sel: "#B57CFF", OK: "#9BE38A", Warn: "#FFD166", Bad: "#FF4F6D", Info: "#7CC8FF",
		SelBg: "#2C1638", Border: "#3A2148", BorderFocus: "#6A3A85", PillFg: "#120A18"},
	{Name: "Monsoon", About: "Hardest difficulty: storm reds and ember orange.",
		Bg: "#170F11", Surface: "#22161A", Text: "#F0E2E0", Muted: "#B19A97", Faint: "#634C4A",
		Accent: "#FF5E5B", Sel: "#FFB26B", OK: "#A3C96B", Warn: "#FFD166", Bad: "#FF3B3B", Info: "#8FB8DE",
		SelBg: "#3A1E22", Border: "#42282C", BorderFocus: "#7A3A40", PillFg: "#170F11"},
	{Name: "Commencement", About: "Mithrix's moon: pale gold and lavender.",
		Bg: "#101218", Surface: "#181B24", Text: "#ECEAE4", Muted: "#A19E95", Faint: "#55534D",
		Accent: "#F4D35E", Sel: "#B8B3E9", OK: "#9BC53D", Warn: "#F4A259", Bad: "#E5383B", Info: "#9AD1D4",
		SelBg: "#23263A", Border: "#2C3040", BorderFocus: "#4C5370", PillFg: "#101218"},
	{Name: "Catppuccin Mocha", About: "Soothing pastels.",
		Bg: "#1E1E2E", Surface: "#181825", Text: "#CDD6F4", Muted: "#A6ADC8", Faint: "#585B70",
		Accent: "#FAB387", Sel: "#CBA6F7", OK: "#A6E3A1", Warn: "#F9E2AF", Bad: "#F38BA8", Info: "#89B4FA",
		SelBg: "#313244", Border: "#45475A", BorderFocus: "#7F849C", PillFg: "#1E1E2E"},
	{Name: "Tokyo Night", About: "Neon city lights.",
		Bg: "#1A1B26", Surface: "#16161E", Text: "#C0CAF5", Muted: "#9AA5CE", Faint: "#565F89",
		Accent: "#FF9E64", Sel: "#BB9AF7", OK: "#9ECE6A", Warn: "#E0AF68", Bad: "#F7768E", Info: "#7AA2F7",
		SelBg: "#283457", Border: "#2F3549", BorderFocus: "#3D59A1", PillFg: "#1A1B26"},
	{Name: "Gruvbox", About: "Warm retro groove.",
		Bg: "#282828", Surface: "#1D2021", Text: "#EBDBB2", Muted: "#A89984", Faint: "#665C54",
		Accent: "#FE8019", Sel: "#D3869B", OK: "#B8BB26", Warn: "#FABD2F", Bad: "#FB4934", Info: "#83A598",
		SelBg: "#3C3836", Border: "#504945", BorderFocus: "#7C6F64", PillFg: "#282828"},
	{Name: "Nord", About: "Arctic, north-bluish calm.",
		Bg: "#2E3440", Surface: "#272C36", Text: "#ECEFF4", Muted: "#A3ABBA", Faint: "#4C566A",
		Accent: "#88C0D0", Sel: "#B48EAD", OK: "#A3BE8C", Warn: "#EBCB8B", Bad: "#BF616A", Info: "#81A1C1",
		SelBg: "#3B4252", Border: "#434C5E", BorderFocus: "#5E81AC", PillFg: "#2E3440"},
	{Name: "Rosé Pine", About: "All natural pine, faux fur and a bit of soho vibes.",
		Bg: "#191724", Surface: "#1F1D2E", Text: "#E0DEF4", Muted: "#908CAA", Faint: "#524F67",
		Accent: "#EBBCBA", Sel: "#C4A7E7", OK: "#9CCFD8", Warn: "#F6C177", Bad: "#EB6F92", Info: "#31A1C4",
		SelBg: "#26233A", Border: "#403D52", BorderFocus: "#6E6A86", PillFg: "#191724"},
	{Name: "Dracula", About: "The classic dark theme.",
		Bg: "#282A36", Surface: "#21222C", Text: "#F8F8F2", Muted: "#B6B8C8", Faint: "#6272A4",
		Accent: "#FFB86C", Sel: "#BD93F9", OK: "#50FA7B", Warn: "#F1FA8C", Bad: "#FF5555", Info: "#8BE9FD",
		SelBg: "#44475A", Border: "#3B3D4E", BorderFocus: "#6272A4", PillFg: "#282A36"},
	{Name: "Risk of Rain Light", About: "The default palette for light backgrounds.", Light: true,
		Bg: "#FAF8F5", Surface: "#EFEAE3", Text: "#1F1B24", Muted: "#6B6475", Faint: "#ABA4B4",
		Accent: "#C2410C", Sel: "#7E3AA8", OK: "#3F7F1F", Warn: "#946200", Bad: "#B42318", Info: "#1D5FC1",
		SelBg: "#EDE3F7", Border: "#D9D2C9", BorderFocus: "#B9A3D6", PillFg: "#FFFFFF"},
	{Name: "Catppuccin Latte", About: "Catppuccin's light flavor.", Light: true,
		Bg: "#EFF1F5", Surface: "#E6E9EF", Text: "#4C4F69", Muted: "#6C6F85", Faint: "#ACB0BE",
		Accent: "#FE640B", Sel: "#8839EF", OK: "#40A02B", Warn: "#DF8E1D", Bad: "#D20F39", Info: "#1E66F5",
		SelBg: "#DCE0E8", Border: "#CCD0DA", BorderFocus: "#9CA0B0", PillFg: "#EFF1F5"},
}

// ThemeNames lists theme names in order.
func ThemeNames() []string {
	out := make([]string, len(Themes))
	for i, t := range Themes {
		out[i] = t.Name
	}
	return out
}

// ThemeByName finds a theme. An empty or unknown name picks the default for
// the terminal's background.
func ThemeByName(name string, darkTerminal bool) Theme {
	for _, t := range Themes {
		if strings.EqualFold(t.Name, name) {
			return t
		}
	}
	if !darkTerminal {
		return Themes[len(Themes)-2]
	}
	return Themes[0]
}

// Current is the theme in use.
var Current Theme

var hooks []func()

// OnThemeChange registers f to run after every Apply (to rebuild styles).
func OnThemeChange(f func()) { hooks = append(hooks, f) }

// Apply switches every color and style to t.
func Apply(t Theme) {
	Current = t
	c := func(h string) lipgloss.Color { return lipgloss.Color(h) }
	Equipment, Void, Uncommon, Boss, Legendary, Lunar = c(t.Accent), c(t.Sel), c(t.OK), c(t.Warn), c(t.Bad), c(t.Info)
	Text, Muted, Faint, SelBg = c(t.Text), c(t.Muted), c(t.Faint), c(t.SelBg)
	Border, BorderFocus, PillFg, Bg, Surface = c(t.Border), c(t.BorderFocus), c(t.PillFg), c(t.Bg), c(t.Surface)

	Bold = lipgloss.NewStyle().Bold(true).Foreground(Text)
	Dim = lipgloss.NewStyle().Foreground(Muted)
	Fainter = lipgloss.NewStyle().Foreground(Faint)
	Accent = lipgloss.NewStyle().Foreground(Equipment).Bold(true)
	OK = lipgloss.NewStyle().Foreground(Uncommon)
	Bad = lipgloss.NewStyle().Foreground(Legendary)
	Warn = lipgloss.NewStyle().Foreground(Boss)
	Info = lipgloss.NewStyle().Foreground(Lunar)
	Sel = lipgloss.NewStyle().Foreground(Void)
	for _, h := range hooks {
		h()
	}
}

func init() { Apply(Themes[0]) }

// Blend mixes two colors (t=0 gives a, t=1 gives b).
func Blend(a, b lipgloss.Color, t float64) lipgloss.Color {
	ca, err1 := colorful.Hex(string(a))
	cb, err2 := colorful.Hex(string(b))
	if err1 != nil || err2 != nil {
		return a
	}
	return lipgloss.Color(ca.BlendLab(cb, t).Clamped().Hex())
}

// Gradient colors each character of s along a gradient.
func Gradient(s string, from, to lipgloss.Color, bold bool) string {
	r := []rune(s)
	var b strings.Builder
	for i, ch := range r {
		t := 0.0
		if len(r) > 1 {
			t = float64(i) / float64(len(r)-1)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(Blend(from, to, t)).Bold(bold).Render(string(ch)))
	}
	return b.String()
}

// GradientPill renders text on a gradient background.
func GradientPill(s string, from, to lipgloss.Color) string {
	r := []rune(s)
	var b strings.Builder
	for i, ch := range r {
		t := 0.0
		if len(r) > 1 {
			t = float64(i) / float64(len(r)-1)
		}
		b.WriteString(lipgloss.NewStyle().Background(Blend(from, to, t)).Foreground(PillFg).Bold(true).Render(string(ch)))
	}
	return b.String()
}

var bigLogo = []string{
	"██████╗  ██████╗ ██████╗ ██████╗ ",
	"██╔══██╗██╔═══██╗██╔══██╗╚════██╗",
	"██████╔╝██║   ██║██████╔╝ █████╔╝",
	"██╔══██╗██║   ██║██╔══██╗██╔═══╝ ",
	"██║  ██║╚██████╔╝██║  ██║███████╗",
	"╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝╚══════╝",
}

// BigLogo is the block-letter logo with a diagonal gradient.
func BigLogo() string {
	lines := make([]string, len(bigLogo))
	for y, l := range bigLogo {
		r := []rune(l)
		var b strings.Builder
		for x, ch := range r {
			t := (float64(x)/float64(len(r)) + float64(y)/float64(len(bigLogo))*0.35) / 1.35
			b.WriteString(lipgloss.NewStyle().Foreground(Blend(Void, Equipment, t)).Render(string(ch)))
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// FillBackground paints bg behind every cell of s, padding each line to width.
// Colors set inside s still win; wherever s resets its background, bg returns.
func FillBackground(s string, bg lipgloss.Color, width int) string {
	c, err := colorful.Hex(string(bg))
	if err != nil {
		return s
	}
	r, g, b := c.RGB255()
	set := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		line = sgr.ReplaceAllStringFunc(line, func(seq string) string {
			if bgReset(seq) {
				return seq + set
			}
			return seq
		})
		if w := lipgloss.Width(line); w < width {
			line += strings.Repeat(" ", width-w)
		}
		lines[i] = set + line + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}

// bgReset reports whether an SGR sequence leaves the background at default.
func bgReset(seq string) bool {
	params := strings.Split(strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m"), ";")
	state := 0 // 0 untouched, 1 reset, 2 set
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case "", "0":
			state = 1
		case "49":
			state = 1
		case "48":
			state = 2
			if i+1 < len(params) && params[i+1] == "2" {
				i += 4
			} else if i+1 < len(params) && params[i+1] == "5" {
				i += 2
			}
		case "38", "58":
			if i+1 < len(params) && params[i+1] == "2" {
				i += 4
			} else if i+1 < len(params) && params[i+1] == "5" {
				i += 2
			}
		default:
			if strings.HasPrefix(p, "48:") {
				state = 2
			}
		}
	}
	return state == 1
}
