package tui

// Motion effects, all off unless Settings → Animations is on:
//
//   - the cursor leaves a fading comet trail when it jumps between rows,
//     and the newly selected row flashes and its marker snaps into place
//   - switching tabs slides the new tab in from the side it came from
//   - dialogs open like an old CRT: a line that widens, then unfolds
//   - the name in the details pane unscrambles out of braille noise
//   - messages in the footer type themselves out
//   - a successful install, update or fix sets off a burst of sparkles
//   - the cursor marker and the logo gently pulse while idle

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ror2mm/internal/ui"
)

// fx is one running effect.
type fx struct {
	start    time.Time
	dur      time.Duration
	from, to int // rows, for the cursor trail
	dir      int // +1 or -1, for the tab slide
}

func newFx(base time.Duration) *fx {
	speed := ui.AnimSpeed
	if speed <= 0 {
		speed = 1
	}
	return &fx{start: time.Now(), dur: time.Duration(float64(base) / speed)}
}

// prog is how far along the effect is, 0 to 1 (1 when done or nil).
func (f *fx) prog() float64 {
	if f == nil || f.dur <= 0 {
		return 1
	}
	return math.Min(1, float64(time.Since(f.start))/float64(f.dur))
}

func (f *fx) active() bool { return f != nil && f.prog() < 1 }

func easeOut(p float64) float64 { return 1 - math.Pow(1-p, 3) }

type effects struct {
	sel, tab, modal, detail, toast, burst *fx
	ticking                               bool
	burstBD                               *ui.Backdrop
}

func (e *effects) any() bool {
	for _, f := range []*fx{e.sel, e.tab, e.modal, e.detail, e.toast, e.burst} {
		if f.active() {
			return true
		}
	}
	return false
}

type animTickMsg struct{}

// About 30 frames a second while something is moving.
func animTick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(time.Time) tea.Msg { return animTickMsg{} })
}

// snapshot is what the effects compare before and after each update.
type snapshot struct {
	tab   tab
	mode  mode
	cur   int
	vcur  int
	toast int
}

func (m *Model) snap() snapshot {
	s := snapshot{tab: m.tab, mode: m.mode, cur: m.cur[m.tab], toast: m.toastID, vcur: -1}
	if m.vw != nil {
		s.vcur = m.vw.cur
	}
	return s
}

func isDialog(md mode) bool {
	switch md {
	case mConfirm, mHelp, mView, mNewMod, mBusy, mReadme:
		return true
	}
	return false
}

// startEffects starts whatever the change from before to now calls for.
// It reports whether anything started.
func (m *Model) startEffects(before snapshot, msg tea.Msg) bool {
	if !ui.Animate {
		return false
	}
	now := m.snap()
	started := false
	if now.tab != before.tab {
		f := newFx(240 * time.Millisecond)
		f.dir = 1
		if now.tab < before.tab {
			f.dir = -1
		}
		m.fx.tab = f
		m.fx.detail = newFx(380 * time.Millisecond)
		started = true
	} else if now.cur != before.cur && now.mode == before.mode && !isDialog(now.mode) {
		dist := now.cur - before.cur
		f := newFx(time.Duration(170+min(abs(dist), 20)*8) * time.Millisecond)
		f.from, f.to = before.cur, now.cur
		m.fx.sel = f
		m.fx.detail = newFx(320 * time.Millisecond)
		started = true
	} else if now.vcur != before.vcur && now.vcur >= 0 && before.vcur >= 0 {
		f := newFx(200 * time.Millisecond)
		f.from, f.to = -1, -1 // no trail inside dialogs, just the flash
		m.fx.sel = f
		started = true
	}
	if isDialog(now.mode) && now.mode != before.mode {
		m.fx.modal = newFx(260 * time.Millisecond)
		started = true
	}
	if now.toast != before.toast {
		m.fx.toast = newFx(300 * time.Millisecond)
		started = true
	}
	if d, ok := msg.(opDoneMsg); ok && d.err == nil {
		m.fx.burst = newFx(1500 * time.Millisecond)
		started = true
	}
	return started
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---------- drawing ----------

// Set for each frame before the body is drawn; row() reads them.
var (
	selFlash   float64 // 0..1, how much the selected row still glows
	selMarker  = "▌"
	markerTint lipgloss.Color
)

func (m *Model) prepareFrameFx() {
	selFlash, selMarker, markerTint = 0, "▌", ui.Void
	if !ui.Animate {
		return
	}
	// The marker breathes slowly between the highlight and accent colors.
	markerTint = ui.Blend(ui.Void, ui.Equipment, 0.5+0.5*math.Sin(ui.Phase()*2.2))
	if f := m.fx.sel; f.active() {
		p := f.prog()
		selFlash = 1 - easeOut(p)
		switch {
		case p < 0.3:
			selMarker = "█"
		case p < 0.6:
			selMarker = "▊"
		}
	}
}

func selBg() lipgloss.Color {
	if selFlash <= 0 {
		return ui.SelBg
	}
	return ui.Blend(ui.SelBg, ui.Void, selFlash*0.55)
}

// scr unscrambles s while the details pane is changing.
func (m *Model) scr(s string) string {
	if f := m.fx.detail; f.active() {
		return ui.Scramble(s, easeOut(f.prog()))
	}
	return s
}

// tabPillBg flashes the newly active tab from the accent to its usual color.
func (m *Model) tabPillBg() lipgloss.Color {
	if f := m.fx.tab; f.active() {
		return ui.Blend(ui.Equipment, ui.Void, easeOut(f.prog()))
	}
	return ui.Void
}

// unfold opens a dialog box: a bright line widens, then the box unfolds
// from its middle.
func (m *Model) unfold(box string, accent lipgloss.Color) string {
	f := m.fx.modal
	if !f.active() {
		return box
	}
	p := f.prog()
	lines := strings.Split(box, "\n")
	bw, bh := lipgloss.Width(box), len(lines)
	blank := strings.Repeat(" ", bw)
	out := make([]string, bh)
	for i := range out {
		out[i] = blank
	}
	if p < 0.35 {
		lw := max(1, int(float64(bw)*easeOut(p/0.35)))
		line := strings.Repeat(" ", (bw-lw)/2) + lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("━", lw))
		out[bh/2] = line + strings.Repeat(" ", bw-lw-(bw-lw)/2)
		return strings.Join(out, "\n")
	}
	vis := max(1, int(float64(bh)*easeOut((p-0.35)/0.65)))
	top := (bh - vis) / 2
	for i := top; i < top+vis && i < bh; i++ {
		out[i] = lines[i]
	}
	return strings.Join(out, "\n")
}

// typed reveals a footer message a few letters at a time.
func (m *Model) typed(s string) string {
	f := m.fx.toast
	if !f.active() {
		return s
	}
	r := []rune(s)
	n := int(float64(len(r)) * easeOut(f.prog()))
	return string(r[:n]) + "▌"
}

// overlayFx draws the cursor trail, sparkles and tab slide onto the
// finished body lines.
func (m *Model) overlayFx(body []string, w, h int) {
	if !ui.Animate {
		return
	}
	if f := m.fx.burst; f.active() {
		if m.fx.burstBD == nil {
			m.fx.burstBD = &ui.Backdrop{Kind: "burst", Strength: 1}
		}
		m.fx.burstBD.Age = time.Since(f.start).Seconds() * ui.AnimSpeed
		m.fx.burstBD.Paint(body, w, h, nil)
	}
	if f := m.fx.sel; f.active() && f.from >= 0 && m.mode == mNormal && (m.tab == tabBrowse || m.tab == tabInstalled || m.tab == tabDoctor) {
		m.drawTrail(body, f)
	}
	if f := m.fx.tab; f.active() {
		shift := int((1 - easeOut(f.prog())) * 14)
		for i, l := range body {
			if shift <= 0 {
				break
			}
			if f.dir > 0 {
				body[i] = strings.Repeat(" ", shift) + ansi.Truncate(l, w-shift, "")
			} else {
				body[i] = ansi.Cut(l, shift, w) + strings.Repeat(" ", shift)
			}
		}
	}
}

// drawTrail paints a comet tail in the marker column behind a moving cursor.
func (m *Model) drawTrail(body []string, f *fx) {
	head := float64(f.from) + float64(f.to-f.from)*easeOut(f.prog())
	off := m.off[m.tab]
	step := 1
	if f.to < f.from {
		step = -1
	}
	const tail = 7.0
	for r := f.from; r != f.to; r += step {
		behind := (step > 0 && float64(r) <= head) || (step < 0 && float64(r) >= head)
		dist := math.Abs(head - float64(r))
		if !behind || dist > tail {
			continue
		}
		y := 1 + r - off // panel border is line 0
		if y < 1 || y >= len(body)-1 {
			continue
		}
		k := 1 - dist/tail
		ch := "▏"
		if k > 0.55 {
			ch = "▌"
		}
		cell := lipgloss.NewStyle().Foreground(ui.Blend(ui.Bg, ui.Void, k)).Render(ch)
		l := body[y]
		lw := ansi.StringWidth(l)
		if lw > 3 {
			body[y] = ansi.Cut(l, 0, 2) + cell + ansi.Cut(l, 3, lw)
		}
	}
}
