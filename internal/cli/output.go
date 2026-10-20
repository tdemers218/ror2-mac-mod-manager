package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"ror2mm/internal/core"
	"ror2mm/internal/ui"
)

var isTTY = term.IsTerminal(int(os.Stdout.Fd()))

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return 100
}

func out(format string, a ...any) { fmt.Printf(format+"\n", a...) }

func header(title string) {
	fmt.Println()
	out("  %s  %s", ui.Logo(), ui.Bold.Render(title))
	fmt.Println()
}

func success(msg string) { out("  %s %s", ui.OK.Render(ui.GOK), msg) }
func warnf(msg string)   { out("  %s %s", ui.Warn.Render(ui.GWarn), msg) }
func errorf(msg string)  { out("  %s %s", ui.Bad.Render(ui.GError), msg) }
func note(msg string)    { out("  %s", ui.Dim.Render(msg)) }

// newTable makes a table in the shared style.
func newTable(headers ...string) *table.Table {
	head := lipgloss.NewStyle().Foreground(ui.Equipment).Bold(true).Padding(0, 1)
	cell := lipgloss.NewStyle().Padding(0, 1)
	return table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.Border)).
		Headers(headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return head
			}
			return cell
		})
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

// confirm asks a yes/no question. Default is yes.
func confirm(q string, assumeYes bool) bool {
	if assumeYes {
		return true
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		note("Not a terminal; pass -y to confirm.")
		return false
	}
	fmt.Printf("\n  %s %s %s ", ui.Accent.Render("?"), q, ui.Dim.Render("[Y/n]"))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "" || a == "y" || a == "yes"
}

func kindGlyph(k core.StepKind) string {
	switch k {
	case core.StepUpdate:
		return ui.Warn.Render(ui.GUpdate)
	case core.StepAdopt:
		return ui.Info.Render(ui.GLocal)
	case core.StepReinstall:
		return ui.Dim.Render("↻")
	default:
		return ui.OK.Render("+")
	}
}

// printPlan shows what a plan will do.
func printPlan(p *core.Plan) {
	for _, s := range p.Skipped {
		out("  %s %s", ui.Dim.Render(ui.GOK), ui.Dim.Render(s))
	}
	if !p.Empty() {
		nameW, verW := 0, 0
		vers := make([]string, len(p.Steps))
		for i, s := range p.Steps {
			if w := lipgloss.Width(s.Pkg.Name); w > nameW {
				nameW = w
			}
			vers[i] = s.Version
			if s.From != "" && s.From != s.Version {
				vers[i] = ui.Dim.Render(s.From+" "+ui.GArrow+" ") + s.Version
			}
			if w := lipgloss.Width(vers[i]); w > verW {
				verW = w
			}
		}
		var size int64
		for i, s := range p.Steps {
			ver := lipgloss.NewStyle().Width(verW).Render(vers[i])
			name := lipgloss.NewStyle().Width(nameW).Render(ui.Bold.Render(s.Pkg.Name))
			out("  %s %s  %s  %s", kindGlyph(s.Kind), name, ver, ui.Fainter.Render(s.Reason))
			if v := s.Pkg.Version(s.Version); v != nil {
				size += v.Size
			}
		}
		fmt.Println()
		note(fmt.Sprintf("%d package(s), %s to download", len(p.Steps), ui.Bytes(size)))
	}
	for _, w := range p.Warnings {
		warnf(w)
	}
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// progressPrinter draws a live progress line for plan events.
type progressPrinter struct {
	mu    sync.Mutex
	frame int
	last  time.Time
}

func (pp *progressPrinter) event(ev core.Event) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	label := fmt.Sprintf("[%d/%d] %s %s", ev.Index+1, ev.Total, ev.Step.Pkg.Name, ev.Step.Version)
	switch {
	case ev.Err != nil:
		pp.clear()
		errorf(label + ": " + ev.Err.Error())
	case ev.Phase == "done":
		pp.clear()
		verb := map[core.StepKind]string{core.StepInstall: "Installed", core.StepUpdate: "Updated", core.StepAdopt: "Adopted", core.StepReinstall: "Reinstalled"}[ev.Step.Kind]
		success(fmt.Sprintf("%s %s %s", verb, ui.Bold.Render(ev.Step.Pkg.Name), ui.Dim.Render(ev.Step.Version)))
	default:
		if !isTTY || time.Since(pp.last) < 60*time.Millisecond && ev.Read != ev.Size {
			return
		}
		pp.last = time.Now()
		pp.frame = (pp.frame + 1) % len(spinFrames)
		line := "  " + ui.Sel.Render(spinFrames[pp.frame]) + " " + label
		if ev.Phase == "download" && ev.Size > 0 {
			frac := float64(ev.Read) / float64(ev.Size)
			line += "  " + ui.Bar(frac, 24) + ui.Dim.Render(fmt.Sprintf(" %3.0f%%  %s", frac*100, ui.Bytes(ev.Size)))
		} else if ev.Phase == "install" {
			line += ui.Dim.Render("  installing…")
		}
		fmt.Print("\r\033[K" + ansi.Truncate(line, termWidth()-1, "…"))
	}
}

func (pp *progressPrinter) clear() {
	if isTTY {
		fmt.Print("\r\033[K")
	}
}

// spinner shows a message with a spinner until stop is called.
func spinner(msg string) (update func(string), stop func()) {
	if !isTTY {
		// Keep stdout clean for scripts and AI agents piping the output.
		fmt.Fprintln(os.Stderr, "  "+msg)
		return func(string) {}, func() {}
	}
	var mu sync.Mutex
	cur := msg
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		i := 0
		t := time.NewTicker(80 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				fmt.Print("\r\033[K")
				return
			case <-t.C:
				mu.Lock()
				fmt.Print("\r\033[K  " + ui.Sel.Render(spinFrames[i%len(spinFrames)]) + " " + ansi.Truncate(cur, termWidth()-6, "…"))
				mu.Unlock()
				i++
			}
		}
	}()
	return func(m string) { mu.Lock(); cur = m; mu.Unlock() }, func() { close(done); <-finished }
}
