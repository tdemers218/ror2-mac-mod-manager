// Package ui holds the shared look: colors (Risk of Rain 2 item tiers),
// glyphs and small formatting helpers.
package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Version is set at build time.
var Version = "dev"

// TrueColor reports whether the terminal takes 24-bit colors, which painting
// the theme background needs.
func TrueColor() bool {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	term := strings.ToLower(os.Getenv("TERM"))
	return ct == "truecolor" || ct == "24bit" || strings.Contains(term, "kitty") ||
		strings.Contains(term, "ghostty") || strings.Contains(term, "direct")
}

// Current theme colors. Names come from Risk of Rain 2 item tiers:
// Equipment = accent, Uncommon = ok, Legendary = error, Boss = warning,
// Lunar = info, Void = highlight. Apply swaps them.
var (
	Equipment   lipgloss.Color
	Uncommon    lipgloss.Color
	Legendary   lipgloss.Color
	Boss        lipgloss.Color
	Lunar       lipgloss.Color
	Void        lipgloss.Color
	Text        lipgloss.Color
	Muted       lipgloss.Color
	Faint       lipgloss.Color
	SelBg       lipgloss.Color
	Border      lipgloss.Color
	BorderFocus lipgloss.Color
	PillFg      lipgloss.Color
	Bg          lipgloss.Color
	Surface     lipgloss.Color
)

// Glyphs.
const (
	GOK       = "✓"
	GFail     = "✗"
	GUpdate   = "↑"
	GOn       = "●"
	GOff      = "○"
	GLocal    = "◆"
	GError    = "✖"
	GWarn     = "▲"
	GInfo     = "●"
	GArrow    = "→"
	GBullet   = "•"
	GDownload = "↓"
	GStar     = "★"
)

// Common styles, rebuilt by Apply.
var (
	Bold    lipgloss.Style
	Dim     lipgloss.Style
	Fainter lipgloss.Style
	Accent  lipgloss.Style
	OK      lipgloss.Style
	Bad     lipgloss.Style
	Warn    lipgloss.Style
	Info    lipgloss.Style
	Sel     lipgloss.Style
)

// Pill renders text as a colored badge.
func Pill(text string, bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Background(bg).Foreground(PillFg).Bold(true).Padding(0, 1).Render(text)
}

// Logo is the small brand mark: a gradient pill.
func Logo() string {
	r := []rune(" ◢ ROR2 MODS ")
	var b strings.Builder
	for i, ch := range r {
		t := shimmer(float64(i) / float64(len(r)-1))
		b.WriteString(lipgloss.NewStyle().Background(Blend(Void, Equipment, t)).Foreground(PillFg).Bold(true).Render(string(ch)))
	}
	return b.String()
}

// Count formats a download count like 2.8M or 146K.
func Count(n int) string {
	switch {
	case n >= 1_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e6)) + "M"
	case n >= 1_000:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1e3)) + "K"
	default:
		return fmt.Sprintf("%d", n)
	}
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

// Bytes formats a size like 1.2 MB.
func Bytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// Ago formats a time like "3d ago".
func Ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 730*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
}
