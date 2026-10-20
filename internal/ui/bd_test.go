package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRoundMore(t *testing.T) {
	box := "╭─ Title ──────────────╮\n│                      │\n│  text                │\n│                      │\n╰──────────────────────╯"
	got := ansi.Strip(RoundMore(box, "#ffffff"))
	fmt.Println(got)
	if !strings.Contains(got, " Title ") || !strings.Contains(got, "  text  ") || strings.ContainsAny(got, "─│╭╮╰╯") {
		t.Fatalf("got\n%s", got)
	}
}

func TestPaintKeepsText(t *testing.T) {
	Animate = false
	b := &Backdrop{Kind: "grid", Strength: 0.3}
	lines := []string{"\x1b[1mhi\x1b[0m" + strings.Repeat(" ", 30), strings.Repeat(" ", 32), strings.Repeat(" ", 32), "\x1b[48;2;1;2;3m" + strings.Repeat(" ", 32) + "\x1b[0m"}
	b.Paint(lines, 32, 4, nil)
	if p := ansi.Strip(lines[0]); !strings.HasPrefix(p, "hi") || ansi.StringWidth(p) != 32 {
		t.Fatalf("line 0: %q", p)
	}
	if p := ansi.Strip(lines[3]); strings.TrimSpace(p) != "" {
		t.Fatalf("painted over a background: %q", p)
	}
	if p := ansi.Strip(lines[2]); !strings.Contains(p, "┊") {
		t.Fatalf("grid missing: %q", p)
	}
}
