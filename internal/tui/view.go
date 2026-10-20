package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"ror2mm/internal/config"
	"ror2mm/internal/core"
	"ror2mm/internal/semver"
	"ror2mm/internal/termimg"
	ts "ror2mm/internal/thunderstore"
	"ror2mm/internal/ui"
)

const headerHeight = 2

func (m *Model) bodySize() (int, int) {
	return m.w, max(5, m.h-headerHeight-1)
}

func (m *Model) paneWidths() (int, int) {
	lw := m.w * 52 / 100
	if lw < 44 {
		lw = min(m.w, 44)
	}
	return lw, m.w - lw
}

func (m *Model) readmeSize() (int, int) {
	w, h := m.bodySize()
	return max(20, min(w-4, 120)), max(3, h-4)
}

// View renders the whole screen.
func (m *Model) View() string {
	if m.w == 0 {
		return ""
	}
	m.clickables = m.clickables[:0]
	m.prepareFrameFx()
	header := m.headerView()
	if m.mode == mBusy && m.frozenHeader != "" {
		header = m.frozenHeader
	}
	w, h := m.bodySize()
	var body string
	m.mask = [4]int{}
	switch m.mode {
	case mBusy:
		body = m.place(w, h, m.busyView())
	case mConfirm:
		body = m.place(w, h, m.confirmView())
	case mHelp:
		body = m.place(w, h, m.helpView())
	case mView:
		body = m.place(w, h, m.viewPanelView())
	case mNewMod:
		body = m.place(w, h, m.wizardView())
	case mReadme:
		body = m.readmeView()
	default:
		body = m.bodyView()
	}
	body = lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Render(body)
	screen := zone.Scan(header + "\n" + body + "\n" + m.footerView())
	if !ui.TrueColor() {
		return screen
	}
	lines := strings.Split(screen, "\n")
	if m.backdrop != nil && len(lines) > headerHeight+h {
		// Background art goes into the empty cells of the body, never inside
		// an open dialog.
		mk := m.mask
		m.backdrop.Paint(lines[headerHeight:headerHeight+h], w, h, func(x, y int) bool {
			return mk[2] > 0 && x >= mk[0] && x < mk[0]+mk[2] && y >= mk[1] && y < mk[1]+mk[3]
		})
	}
	if len(lines) > headerHeight+h {
		m.overlayFx(lines[headerHeight:headerHeight+h], w, h)
	}
	// Paint the bars in the surface color and, if enabled, everything else in
	// the theme background, so the app looks the same in any terminal.
	for i := range lines {
		switch {
		case i == 0 || i == len(lines)-1:
			lines[i] = ui.FillBackground(lines[i], ui.Surface, m.w)
		case m.fillBg:
			lines[i] = ui.FillBackground(lines[i], ui.Bg, m.w)
		}
	}
	return strings.Join(lines, "\n")
}

func place(w, h int, box string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

// place centers a dialog and remembers where it is, so background art keeps out.
func (m *Model) place(w, h int, box string) string {
	box = m.unfold(box, ui.Void)
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	m.mask = [4]int{max(0, (w-bw)/2), max(0, (h-bh)/2), bw, bh}
	return place(w, h, box)
}

// frame applies the extra-round corners when that border style is chosen.
func (m *Model) frame(box string, color lipgloss.Color) string {
	if !m.roundMore {
		return box
	}
	return ui.RoundMore(box, string(color))
}

// ---------- header ----------

// mark registers a clickable area for this frame.
func (m *Model) mark(id, s string) string {
	m.clickables = append(m.clickables, id)
	return zone.Mark(id, s)
}

func (m *Model) tabLabel(i int, name string) string {
	label := tabIcons[i] + " " + name
	switch tab(i) {
	case tabDoctor:
		if n := m.countSev(core.SevError); n > 0 {
			label += " " + ui.GError + fmt.Sprint(n)
		} else if n := m.countSev(core.SevWarn); n > 0 {
			label += " " + ui.GWarn + fmt.Sprint(n)
		}
	case tabInstalled:
		if n := len(m.app.Inv.Updates()); n > 0 {
			label += " " + ui.GUpdate + fmt.Sprint(n)
		}
	}
	if tab(i) == m.tab {
		return lipgloss.NewStyle().Background(m.tabPillBg()).Foreground(ui.PillFg).Bold(true).Padding(0, 1).Render(label)
	}
	st := lipgloss.NewStyle().Foreground(ui.Muted).Padding(0, 1)
	if tab(i) == tabDoctor && m.countSev(core.SevError) > 0 {
		st = st.Foreground(ui.Legendary)
	}
	return st.Render(label)
}

func (m *Model) headerView() string {
	left := " " + ui.Logo() + "  "
	var tabs []string
	for i, n := range tabNames {
		tabs = append(tabs, m.mark(fmt.Sprintf("tab:%d", i), m.tabLabel(i, n)))
	}
	left += strings.Join(tabs, " ")

	var right []string
	if m.gameRunning {
		right = append(right, ui.OK.Render(ui.GOn+" game running"))
	} else if m.launching != "" {
		right = append(right, m.spin.View()+" "+ui.Dim.Render(m.launching))
	}
	if m.catLoading {
		right = append(right, m.spin.View()+ui.Dim.Render(fmt.Sprintf(" catalog %d%%", int(m.catFrac*100))))
	}
	if m.binChanged {
		right = append(right, m.mark("key:`#hdr", lipgloss.NewStyle().Background(ui.Equipment).Foreground(ui.PillFg).Bold(true).Padding(0, 1).Render("⟳ new version · restart")))
	} else {
		right = append(right, m.mark("key:`#hdr", ui.Dim.Render("⟳ reload ")+ui.Fainter.Render("`")))
	}
	r := strings.Join(right, "   ") + " "
	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(r))
	line1 := left + " " + ui.Stars(gap-2, 11) + " " + r
	if gap < 3 {
		line1 = left + strings.Repeat(" ", gap) + r
	}
	line1 = ansi.Truncate(line1, m.w, "")

	var line2 string
	switch m.tab {
	case tabBrowse:
		line2 = m.searchBar()
	case tabInstalled:
		b := m.app.Inv.BepInEx
		if b.Installed {
			line2 = " " + ui.OK.Render(ui.GOK) + " BepInEx " + b.Version + ui.Dim.Render("  ·  "+config.ShortPath(m.app.Game.Dir))
		} else {
			line2 = " " + ui.Bad.Render(ui.GError+" BepInEx is not installed in "+config.ShortPath(m.app.Game.Dir))
		}
	case tabDoctor:
		if l := m.app.Log; l != nil {
			line2 = " " + ui.Dim.Render(fmt.Sprintf("Last launch %s · %d plugins loaded · %d failed", ui.Ago(l.Time), len(l.Loaded), len(l.Failures)))
		} else {
			line2 = " " + ui.Dim.Render("No BepInEx log yet; launch the game once (L).")
		}
	case tabCreate:
		line2 = " " + m.createSubheader()
	case tabSettings:
		line2 = " " + ui.Dim.Render("Changes apply instantly and are saved to ") + ui.Fainter.Render(config.ShortPath(config.Dir()+"/config.json"))
	}
	line2 = ansi.Truncate(line2, m.w, "…")
	return line1 + "\n" + line2
}

func (m *Model) searchBar() string {
	icon := ui.Dim.Render(" ⌕ ")
	if m.mode == mSearch {
		icon = ui.Sel.Render(" ⌕ ")
	}
	field := m.search.View()
	if m.mode != mSearch && m.search.Value() == "" {
		field = ui.Fainter.Render("press / to search")
	}
	chip := func(k, v string, on bool) string {
		st := lipgloss.NewStyle().Foreground(ui.Muted)
		if on {
			st = st.Foreground(ui.Void)
		}
		return ui.Fainter.Render(k+" ") + st.Render(v)
	}
	filt, filtOn := m.filterSummary(tabBrowse)
	ok, _ := catKeys(tabBrowse)
	cats := "all"
	if n := len(m.catList(ok)); n == 1 {
		cats = m.catList(ok)[0]
	} else if n > 1 {
		cats = fmt.Sprintf("%d picked", n)
	}
	gap := max(2, m.search.Width+2-lipgloss.Width(field))
	sortOn := len(m.sorts(tabBrowse)) > 2 || m.primarySort().field != "relevance" && m.primarySort().field != "downloads"
	chips := "   " + m.mark("key:f#sort", chip("sort", m.sortSummary(tabBrowse), sortOn)) +
		"   " + m.mark("key:c", chip("categories", cats, cats != "all")) +
		"   " + m.mark("key:f#filt", chip("filters", filt, filtOn))
	return m.mark("key:/", icon+field+strings.Repeat(" ", gap)) + chips
}

// ---------- body ----------

// panel draws a framed box with its title set into the top border.
func (m *Model) panel(w, h int, title, content string, focus bool) string {
	bc := ui.Border
	tst := lipgloss.NewStyle().Foreground(ui.Muted)
	if focus {
		bc = ui.BorderFocus
		tst = lipgloss.NewStyle().Foreground(ui.Equipment).Bold(true)
	}
	b := m.border
	bst := lipgloss.NewStyle().Foreground(bc)
	t := ""
	if title != "" {
		t = ansi.Truncate(" "+ansi.Strip(title)+" ", max(0, w-6), "… ")
	}
	lead := 1
	if m.roundMore {
		lead = 3 // leave room for the wide curve before the title
	}
	fill := max(0, w-2-lead-lipgloss.Width(t))
	top := bst.Render(b.TopLeft+strings.Repeat(b.Top, lead)) + tst.Render(t) + bst.Render(strings.Repeat(b.Top, fill)+b.TopRight)
	// Clip first: a wrapped line must never push the bottom border off screen.
	body := lipgloss.NewStyle().Border(b, false, true, true, true).BorderForeground(bc).
		Padding(0, 1).Width(w - 2).Height(h - 2).MaxHeight(h - 1).Render(clipLines(content, h-2))
	return m.frame(top+"\n"+body, bc)
}

func (m *Model) bodyView() string {
	_, h := m.bodySize()
	lw, rw := m.paneWidths()
	listH := h - 2 // top and bottom border
	m.scroll(listH)
	var title, dtitle string
	var rows []string
	var detail string
	iw, dw := lw-4, rw-4
	switch m.tab {
	case tabBrowse:
		title, rows = m.browseRows(iw, listH)
		detail, dtitle = m.browseDetail(dw), "Details"
	case tabInstalled:
		title, rows = m.installedRows(iw, listH)
		detail, dtitle = m.installedDetail(dw), "Details"
	case tabDoctor:
		title, rows = m.doctorRows(iw, listH)
		detail, dtitle = m.doctorDetail(dw), "Issue"
	case tabCreate:
		title, rows = m.createRows(iw, listH)
		detail, dtitle = m.createDetail(dw), "Project"
	case tabSettings:
		title, rows = m.settingsRows(iw, listH)
		detail, dtitle = m.settingsDetail(dw), "Preview"
	}
	left := m.panel(lw, h, title, strings.Join(rows, "\n"), true)
	right := m.panel(rw, h, dtitle, clipLines(detail, h-2), false)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m *Model) scroll(rows int) {
	t := m.tab
	if m.cur[t] < m.off[t] {
		m.off[t] = m.cur[t]
	}
	if m.cur[t] >= m.off[t]+rows {
		m.off[t] = m.cur[t] - rows + 1
	}
	if m.off[t] < 0 {
		m.off[t] = 0
	}
}

func clipLines(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = append(lines[:h-1], ui.Fainter.Render("…"))
	}
	return strings.Join(lines, "\n")
}

// row builds one list line. Each segment is styled with the selection
// background so it does not get cut off by inner color resets.
type seg struct {
	text  string
	style lipgloss.Style
}

func row(width int, sel bool, left []seg, right []seg) string {
	return rowLead(width, sel, left, right, "")
}

// rowLead is row with the gap between left and right filled by a faint
// dotted leader (when dotted accents are on), like a table of contents.
func rowLead(width int, sel bool, left []seg, right []seg, lead string) string {
	render := func(ss []seg) (string, int) {
		var b strings.Builder
		w := 0
		for _, s := range ss {
			st := s.style
			if sel {
				st = st.Background(selBg())
			}
			b.WriteString(st.Render(s.text))
			w += lipgloss.Width(s.text)
		}
		return b.String(), w
	}
	// Truncate left plain text to fit beside right.
	rw := 0
	for _, s := range right {
		rw += lipgloss.Width(s.text)
	}
	avail := width - rw - 2
	var fitted []seg
	used := 0
	for _, s := range left {
		if used >= avail {
			break
		}
		t := s.text
		if used+lipgloss.Width(t) > avail {
			t = ansi.Truncate(t, avail-used, "…")
		}
		fitted = append(fitted, seg{t, s.style})
		used += lipgloss.Width(t)
	}
	ls, lw := render(fitted)
	rs, _ := render(right)
	padSt := lipgloss.NewStyle()
	if sel {
		padSt = padSt.Background(selBg())
	}
	marker := padSt.Render(" ")
	if sel {
		marker = lipgloss.NewStyle().Foreground(markerTint).Background(selBg()).Render(selMarker)
	}
	gap := max(1, width-1-lw-rw)
	fill := padSt.Render(strings.Repeat(" ", gap))
	if lead != "" && ui.Decor && gap >= 4 && len(right) > 0 {
		fill = padSt.Render(" ") + stFaint.Inherit(padSt).Render(strings.Repeat(lead, gap-2)) + padSt.Render(" ")
	}
	return marker + ls + fill + rs
}

var stName, stBold, stDim, stFaint, stOK, stWarn, stBad, stInfo, stVoid lipgloss.Style

// rebuildStyles recreates list styles after a theme change.
func rebuildStyles() {
	stName = lipgloss.NewStyle().Foreground(ui.Text)
	stBold = lipgloss.NewStyle().Foreground(ui.Text).Bold(true)
	stDim = lipgloss.NewStyle().Foreground(ui.Muted)
	stFaint = lipgloss.NewStyle().Foreground(ui.Faint)
	stOK = lipgloss.NewStyle().Foreground(ui.Uncommon)
	stWarn = lipgloss.NewStyle().Foreground(ui.Boss)
	stBad = lipgloss.NewStyle().Foreground(ui.Legendary)
	stInfo = lipgloss.NewStyle().Foreground(ui.Lunar)
	stVoid = lipgloss.NewStyle().Foreground(ui.Void)
}

func init() { rebuildStyles() }

func (m *Model) browseRows(w, h int) (string, []string) {
	if m.app.Cat == nil {
		msg := emptyState(w, h-3, "Fetching the Thunderstore catalog…", "")
		msg = append(msg, "  "+ui.Bar(m.catFrac, max(10, w-6)))
		return "Loading", msg
	}
	title := fmt.Sprintf("%s mods", ui.Count(len(m.results)))
	if len(m.results) < 1000 {
		title = fmt.Sprintf("%d mods", len(m.results))
	}
	if q := m.search.Value(); q != "" {
		title += fmt.Sprintf(" matching %q", q)
	}
	title += " · " + m.sortSummary(tabBrowse)
	if len(m.results) == 0 {
		return title, emptyState(w, h, "Nothing found.", "Try fewer words, esc to clear, or f to loosen the filters.")
	}
	var rows []string
	off := m.off[tabBrowse]
	for i := off; i < len(m.results) && i < off+h; i++ {
		p := m.results[i]
		sel := i == m.cur[tabBrowse]
		g := seg{"  ", stName}
		if e := m.app.Inv.Find(p.FullName); e != nil {
			switch {
			case e.HasUpdate():
				g = seg{ui.GUpdate + " ", stWarn}
			case !e.Managed:
				g = seg{ui.GLocal + " ", stInfo}
			case !e.Enabled:
				g = seg{ui.GOff + " ", stDim}
			default:
				g = seg{ui.GOK + " ", stOK}
			}
		}
		nameSt := stName
		if sel {
			nameSt = stBold
		}
		if p.Deprecated {
			nameSt = nameSt.Strikethrough(true).Foreground(ui.Muted)
		}
		left := []seg{g, {p.Name, nameSt}, {"  " + p.Owner, stFaint}}
		if hasLibraryCat(p) {
			left = append(left, seg{"  library", stInfo.Faint(true)})
		}
		right := m.browseMetric(p, sel)
		rows = append(rows, m.mark(fmt.Sprintf("row:%d", i), row(w, sel, left, right)))
	}
	return title, rows
}

func wrap(s string, w int) string { return ansi.Wordwrap(s, w, "") }

func (m *Model) hints(pairs ...string) string { return m.hintsW(0, pairs...) }

// keyHintsW lays out "key action" pairs, starting a new line rather than
// splitting a pair when width (if > 0) runs out.
func (m *Model) hintsW(width int, pairs ...string) string {
	var lines []string
	cur, curW := "", 0
	for i := 0; i+1 < len(pairs); i += 2 {
		part := ui.Accent.Render(pairs[i]) + " " + ui.Dim.Render(pairs[i+1])
		if clickableKey(pairs[i]) {
			// The same key can appear in several places; keep each area distinct.
			part = m.mark(fmt.Sprintf("key:%s#%d", pairs[i], len(m.clickables)), part)
		}
		pw := lipgloss.Width(part)
		if curW > 0 && width > 0 && curW+3+pw > width {
			lines = append(lines, cur)
			cur, curW = "", 0
		}
		if curW > 0 {
			cur += "   "
			curW += 3
		}
		cur += part
		curW += pw
	}
	return strings.Join(append(lines, cur), "\n")
}

func (m *Model) depLines(deps []string) []string {
	var out []string
	for _, d := range deps {
		full, min := ts.ParseDep(d)
		_, name := ts.SplitFullName(full)
		mark, extra := ui.Bad.Render(ui.GFail), ui.Bad.Render(" missing")
		if strings.EqualFold(name, "BepInExPack") {
			if m.app.Inv.BepInEx.Installed {
				mark, extra = ui.OK.Render(ui.GOK), ui.Dim.Render(" loader")
			}
		} else if e := m.app.Inv.Find(full); e != nil {
			mark, extra = ui.OK.Render(ui.GOK), ui.Dim.Render(" have "+e.Version)
			if e.Version != "" && min != "" && compareLess(e.Version, min) {
				mark, extra = ui.Warn.Render(ui.GUpdate), ui.Warn.Render(" have "+e.Version+", needs "+min)
			} else if !e.Enabled {
				mark, extra = ui.Warn.Render(ui.GOff), ui.Warn.Render(" disabled")
			}
		}
		out = append(out, fmt.Sprintf("  %s %s %s%s", mark, name, ui.Fainter.Render("≥"+min), extra))
	}
	return out
}

// iconLines returns the drawn icon, blank space while it loads, or nil when
// images are off or the mod has no icon.
func (m *Model) iconLines(p *ts.Package) []string {
	iconCols, iconRows := m.iconCols, m.iconRows
	if m.imgs == nil || m.imgs.Mode == termimg.Off || p.Icon == "" || iconCols == 0 {
		return nil
	}
	if lines := m.imgs.Render(p.Icon, iconCols, iconRows); lines != nil {
		return lines
	}
	if m.imgs.Has(p.Icon) {
		return nil // failed to load
	}
	blank := make([]string, iconRows)
	for i := range blank {
		blank[i] = strings.Repeat(" ", iconCols)
	}
	blank[iconRows/2] = lipgloss.PlaceHorizontal(iconCols, lipgloss.Center, m.spin.View())
	return blank
}

func (m *Model) packageBlock(p *ts.Package, w int) []string {
	l := p.Latest()
	icon := m.iconLines(p)
	infoW := w
	if icon != nil && w >= 28+m.iconCols {
		infoW = w - m.iconCols - 2
	} else {
		icon = nil
	}
	var info []string
	info = append(info, ui.Accent.Render(m.scr(p.Name)), ui.Dim.Render("v"+l.Number+" · by "+p.Owner))
	info = append(info, ui.Dim.Render("updated "+ui.Ago(p.Updated)))
	info = append(info, "")
	info = append(info, ui.Dim.Render(fmt.Sprintf("%s %s  %s %d  %s", ui.GDownload, ui.Count(p.Downloads), ui.GStar, p.Rating, ui.Bytes(l.Size))))
	if len(p.Categories) > 0 {
		var chips []string
		for _, c := range p.Categories {
			chips = append(chips, "#"+strings.ReplaceAll(c, " ", ""))
		}
		info = append(info, ui.Sel.Render(wrap(strings.Join(chips, " "), infoW)))
	}
	var out []string
	if icon != nil {
		joined := lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(icon, "\n"), "  ", strings.Join(info, "\n"))
		out = append(out, strings.Split(joined, "\n")...)
	} else {
		out = append(out, info...)
	}
	out = append(out, "")
	if p.Description != "" {
		out = append(out, wrap(p.Description, w))
	}
	if chart := versionChart(p, w); chart != "" {
		out = append(out, "", chart)
	}
	if p.Deprecated {
		out = append(out, "", ui.Bad.Render(ui.GWarn+" Deprecated by its author"))
	}
	return out
}

func (m *Model) browseDetail(w int) string {
	p := m.selPackage()
	if p == nil {
		return ui.Dim.Render("Nothing selected.")
	}
	out := m.packageBlock(p, w)
	out = append(out, "")
	hints := []string{"enter", "readme"}
	if e := m.app.Inv.Find(p.FullName); e != nil {
		switch {
		case !e.Managed:
			out = append(out, ui.Info.Render(ui.GLocal+" Installed by hand ("+orQ(e.Version)+")"))
			hints = append(hints, "i", "adopt")
		case e.HasUpdate():
			out = append(out, ui.Warn.Render(ui.GUpdate+" Installed "+e.Version+" · "+e.Latest()+" available"))
			hints = append(hints, "u", "update")
		case !e.Enabled:
			out = append(out, ui.Dim.Render(ui.GOff+" Installed "+e.Version+" · disabled"))
		default:
			out = append(out, ui.OK.Render(ui.GOK+" Installed "+e.Version))
		}
		hints = append(hints, "x", "remove")
	} else {
		hints = append(hints, "i", "install")
	}
	if deps := p.Latest().Deps; len(deps) > 0 {
		out = append(out, "", ui.Rule(fmt.Sprintf("Needs %d", len(deps)), w))
		out = append(out, m.depLines(deps)...)
	}
	out = append(out, "", m.hintsW(w, hints...))
	return strings.Join(out, "\n")
}

func orQ(s string) string {
	if s == "" {
		return "version unknown"
	}
	return s
}

func hasLibraryCat(p *ts.Package) bool {
	for _, c := range p.Categories {
		if c == "Libraries" {
			return true
		}
	}
	return false
}

func (m *Model) installedRows(w, h int) (string, []string) {
	mods, libs := 0, 0
	for _, g := range m.app.Inv.Groups() {
		if g.Library {
			libs += len(g.Entries)
		} else {
			mods += len(g.Entries)
		}
	}
	title := fmt.Sprintf("%d mods · %d libraries & dependencies", mods, libs)
	shown := 0
	groups, _ := m.installedGroups()
	for _, g := range groups {
		shown += len(g.Entries)
	}
	if shown < mods+libs {
		title = fmt.Sprintf("%d of %d shown · %s", shown, mods+libs, m.sortSummary(tabInstalled))
	} else if len(m.sorts(tabInstalled)) > 1 || m.sorts(tabInstalled)[0].field != "name" {
		title += " · " + m.sortSummary(tabInstalled)
	}
	if len(m.irows) == 0 {
		if mods+libs > 0 {
			return title, emptyState(w, h, "Everything is filtered out.", "Press f to change what Installed shows.")
		}
		return title, emptyState(w, h, "No mods yet.", "Press 1 to browse Thunderstore.")
	}
	var rows []string
	off := m.off[tabInstalled]
	for i := off; i < len(m.irows) && i < off+h; i++ {
		r := m.irows[i]
		sel := i == m.cur[tabInstalled]
		if g := r.group; g != nil {
			arrow := "▾ "
			if m.collapsed[g.Name] {
				arrow = "▸ "
			}
			st := lipgloss.NewStyle().Foreground(ui.Equipment).Bold(true)
			if g.Library {
				st = lipgloss.NewStyle().Foreground(ui.Muted).Bold(true)
			}
			right := []seg{{fmt.Sprint(len(g.Entries)), stDim}}
			if n := groupUpdates(g); n > 0 {
				right = []seg{{fmt.Sprintf("%s%d  ", ui.GUpdate, n), stWarn}, {fmt.Sprint(len(g.Entries)), stDim}}
			}
			rows = append(rows, m.mark(fmt.Sprintf("row:%d", i), rowLead(w, sel, []seg{{arrow, stDim}, {g.Name, st}}, right, "┈")))
			continue
		}
		e := r.entry
		g := seg{ui.GOn + " ", stOK}
		switch {
		case !e.Managed:
			g = seg{ui.GLocal + " ", stInfo}
		case !e.Enabled:
			g = seg{ui.GOff + " ", stDim}
		}
		nameSt := stName
		if sel {
			nameSt = stBold
		}
		if !e.Enabled {
			nameSt = nameSt.Foreground(ui.Muted)
		}
		indent := "  "
		if m.instFlat {
			indent = " "
		}
		left := []seg{{indent, stName}, g, {e.Name, nameSt}}
		if m.showDesc && e.Pkg != nil && e.Pkg.Description != "" {
			left = append(left, seg{"  " + strings.ReplaceAll(e.Pkg.Description, "\n", " "), stFaint})
		}
		var right []seg
		if e.HasUpdate() {
			right = []seg{{e.Version + " ", stDim}, {ui.GUpdate + " " + e.Latest(), stWarn}}
		} else {
			right = []seg{{orDash(e.Version), stDim}}
		}
		rows = append(rows, m.mark(fmt.Sprintf("row:%d", i), row(w, sel, left, right)))
	}
	return title, rows
}

func groupUpdates(g *core.Group) int {
	n := 0
	for _, e := range g.Entries {
		if e.HasUpdate() {
			n++
		}
	}
	return n
}

func (m *Model) groupDetail(g *core.Group, w int) string {
	out := []string{ui.Accent.Render(m.scr(g.Name)), ui.Dim.Render(fmt.Sprintf("%d installed", len(g.Entries))), ""}
	if g.About != "" {
		out = append(out, ui.Dim.Render(wrap(g.About, w)), "")
	}
	for _, e := range g.Entries {
		line := "  " + e.Name
		if g.Library {
			var by []string
			for _, d := range m.app.Inv.Dependents(e.FullName) {
				if d != e {
					by = append(by, d.Name)
				}
			}
			if len(by) > 0 {
				line += ui.Fainter.Render("  ← " + strings.Join(by, ", "))
			}
		}
		out = append(out, ansi.Truncate(line, w, "…"))
	}
	verb := "collapse"
	if m.collapsed[g.Name] {
		verb = "expand"
	}
	out = append(out, "", m.hintsW(w, "space", verb, "z", "all sections"))
	return strings.Join(out, "\n")
}

func orDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func (m *Model) installedDetail(w int) string {
	if g := m.selGroup(); g != nil {
		return m.groupDetail(g, w)
	}
	e := m.selEntry()
	if e == nil {
		return ui.Dim.Render("Nothing selected.")
	}
	var out []string
	var hints []string
	if e.Pkg != nil {
		out = append(out, m.packageBlock(e.Pkg, w)...)
		hints = append(hints, "enter", "readme")
	} else {
		out = append(out, ui.Accent.Render(e.Name), ui.Dim.Render("Not on Thunderstore"))
	}
	out = append(out, "")
	switch {
	case !e.Managed:
		out = append(out, ui.Info.Render(ui.GLocal+" Installed by hand · "+orQ(e.Version)))
		out = append(out, ui.Fainter.Render(wrap(strings.Join(e.Local.Paths, ", "), w)))
		if e.Pkg != nil {
			conf := "name and version match"
			if e.Match != core.MatchStrong {
				conf = "name matches, version unconfirmed"
			}
			out = append(out, ui.Dim.Render(wrap("Identified as "+e.FullName+" ("+conf+"). Adopt to track and update it.", w)))
			hints = append(hints, "a", "adopt")
		} else {
			out = append(out, ui.Dim.Render(wrap("ror2 leaves this mod alone. Remove it with x if you no longer want it.", w)))
		}
	default:
		st := ui.OK.Render(ui.GOn + " Enabled")
		if !e.Enabled {
			st = ui.Dim.Render(ui.GOff + " Disabled")
		}
		v := "Version " + e.Version
		if e.HasUpdate() {
			v += ui.Warn.Render("  " + ui.GUpdate + " " + e.Latest() + " available")
			hints = append(hints, "u", "update")
		}
		out = append(out, st+ui.Dim.Render("  ·  ")+v)
		if e.AsDependency {
			out = append(out, ui.Dim.Render("Installed as a dependency"))
		}
		hints = append(hints, "space", map[bool]string{true: "disable", false: "enable"}[e.Enabled])
	}
	if deps := e.Deps(); len(deps) > 0 {
		out = append(out, "", ui.Rule(fmt.Sprintf("Needs %d", len(deps)), w))
		out = append(out, m.depLines(deps)...)
	}
	if e.FullName != "" {
		if needers := m.app.Inv.Dependents(e.FullName); len(needers) > 0 {
			var n []string
			for _, d := range needers {
				if d != e {
					n = append(n, d.Name)
				}
			}
			if len(n) > 0 {
				out = append(out, "", ui.Rule("Needed by", w), ui.Dim.Render(wrap("  "+strings.Join(n, ", "), w)))
			}
		}
	}
	hints = append(hints, "x", "remove")
	out = append(out, "", m.hintsW(w, hints...))
	return strings.Join(out, "\n")
}

func sevSeg(s core.Severity) seg {
	switch s {
	case core.SevError:
		return seg{ui.GError + " ", stBad}
	case core.SevWarn:
		return seg{ui.GWarn + " ", stWarn}
	}
	return seg{ui.GInfo + " ", stInfo}
}

func (m *Model) doctorRows(w, h int) (string, []string) {
	e, wa, n := m.countSev(core.SevError), m.countSev(core.SevWarn), m.countSev(core.SevInfo)
	title := fmt.Sprintf("%d problem(s) · %d warning(s) · %d note(s)", e, wa, n)
	if len(m.issues) == 0 {
		return "All clear", emptyState(w, h, ui.OK.Render(ui.GOK+" Everything looks good."),
			"Launch the game (L); when it closes, the log is checked again automatically.")
	}
	var rows []string
	off := m.off[tabDoctor]
	for i := off; i < len(m.issues) && i < off+h; i++ {
		is := m.issues[i]
		sel := i == m.cur[tabDoctor]
		nameSt := stName
		if sel {
			nameSt = stBold
		}
		var right []seg
		if is.Fix != nil {
			right = []seg{{"fixable", stVoid}}
		}
		rows = append(rows, m.mark(fmt.Sprintf("row:%d", i), row(w, sel, []seg{sevSeg(is.Sev), {is.Title, nameSt}}, right)))
	}
	return title, rows
}

func (m *Model) doctorDetail(w int) string {
	is := m.selIssue()
	if is == nil {
		return strings.Join([]string{
			ui.Accent.Render("Doctor"),
			"",
			ui.Dim.Render(wrap("Checks that BepInEx is set up, that every mod has its dependencies at the right versions, and reads BepInEx's log from your last launch for plugins that refused to load.", w)),
		}, "\n")
	}
	label := map[core.Severity]string{core.SevError: "PROBLEM", core.SevWarn: "WARNING", core.SevInfo: "NOTE"}[is.Sev]
	col := map[core.Severity]lipgloss.TerminalColor{core.SevError: ui.Legendary, core.SevWarn: ui.Boss, core.SevInfo: ui.Lunar}[is.Sev]
	out := []string{ui.Pill(label, col), "", ui.Bold.Render(wrap(m.scr(is.Title), w)), ""}
	if is.Detail != "" {
		out = append(out, ui.Dim.Render(wrap(is.Detail, w)), "")
	}
	hints := []string{}
	if is.Fix != nil {
		out = append(out, ui.Sel.Render(ui.GArrow+" "+is.Fix.Label))
		hints = append(hints, "f", "fix this")
	} else {
		out = append(out, ui.Fainter.Render("No automatic fix."))
	}
	hints = append(hints, "F", "fix problems+warnings", "A", "fix all", "r", "re-read log")
	out = append(out, "", m.hintsW(w, hints...))
	return strings.Join(out, "\n")
}

// ---------- modals ----------

func (m *Model) modal(title string, body string, accent lipgloss.TerminalColor, w int) string {
	head := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(title)
	box := lipgloss.NewStyle().Border(m.border).BorderForeground(accent).
		Padding(1, 2).Width(w).Render(head + "\n\n" + body)
	if c, ok := accent.(lipgloss.Color); ok {
		return m.frame(box, c)
	}
	return box
}

func (m *Model) modalWidth() int { return max(40, min(m.w-6, 96)) }

func (m *Model) confirmView() string {
	c := m.confirm
	if c == nil {
		return ""
	}
	_, h := m.bodySize()
	body := c.body
	maxLines := h - 10
	if len(body) > maxLines && maxLines > 3 {
		hidden := len(body) - maxLines + 1
		body = append(body[:maxLines-1], ui.Fainter.Render(fmt.Sprintf("… and %d more", hidden)))
	}
	w := m.modalWidth()
	for i := range body {
		body[i] = ansi.Truncate(body[i], w-6, "…")
	}
	no := c.noLabel
	if no == "" {
		no = "cancel"
	}
	keys := m.hints("y", c.yesLabel, "n", no)
	return m.modal(c.title, strings.Join(body, "\n")+"\n\n"+keys, ui.Void, w)
}

func (m *Model) busyView() string {
	b := m.busy
	if b == nil {
		return ""
	}
	w := m.modalWidth()
	var lines []string
	done := b.done
	if len(done) > 8 {
		lines = append(lines, ui.Fainter.Render(fmt.Sprintf("… %d earlier", len(done)-8)))
		done = done[len(done)-8:]
	}
	lines = append(lines, done...)
	if b.current != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.spin.View()+" "+b.current)
		if b.hasFrac {
			lines = append(lines, "  "+ui.Bar(b.frac, w-14)+ui.Dim.Render(fmt.Sprintf(" %3d%%", int(b.frac*100))))
		}
	} else if len(lines) == 0 {
		lines = append(lines, m.spin.View()+" Backing up BepInEx…")
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], w-6, "…")
	}
	return m.modal(b.title, strings.Join(lines, "\n"), ui.Equipment, w)
}

func (m *Model) helpView() string {
	sec := func(t string) string { return ui.Sel.Bold(true).Render(t) }
	k := lipgloss.NewStyle().Foreground(ui.Equipment).Width(14)
	line := func(key, d string) string { return "  " + k.Render(key) + ui.Dim.Render(d) }
	body := strings.Join([]string{
		sec("Everywhere"),
		line("1-5 / tab", "switch tabs   , settings"),
		line("↑↓ j k", "move   pgup/pgdn  g/G  top/bottom"),
		line("enter", "README"),
		line("L", "launch Risk of Rain 2"),
		line("R", "refresh the Thunderstore catalog"),
		line("` / F5 / ⟳", "reload from disk (restarts after an update)"),
		line("q", "quit"),
		"",
		sec("Browse"),
		line("/", "search  (esc clears)"),
		line("f", "sort & filter: several sort levels, show/hide anything"),
		line("s  S", "change the first sort · flip its direction"),
		line("c  b  D", "categories · libraries · deprecated"),
		line("i  u  x", "install · update · remove"),
		"",
		sec("Installed"),
		line("space", "enable / disable · open/close a section"),
		line("z", "open or close all sections"),
		line("f", "sort & filter: hide disabled, sections, flat list…"),
		line("u  U", "update this · update all"),
		line("a  x", "adopt hand-installed · remove"),
		"",
		sec("Create"),
		line("n / enter", "new mod · open project in your AI agent"),
		line("c  x", "open in Claude Code · Codex (continues the last chat)"),
		line("C  X", "start a new Claude Code · Codex chat"),
		line("b d u p", "build · deploy to game · remove from game · package"),
		"",
		sec("Doctor"),
		line("f  F  A", "fix this · fix problems+warnings · fix all"),
		line("r", "re-read the BepInEx log"),
		"",
		sec("Mouse"),
		line("click", "tabs, rows, section headers, and any key hint"),
		line("double-click", "open a mod's README (Doctor: fix the issue)"),
		line("wheel", "scroll lists, READMEs and the filter panel"),
		"",
		ui.Dim.Render("Every change backs up BepInEx first (ror2 backups / restore)."),
		"",
		ui.Dim.Render("Legend:  ") + ui.OK.Render(ui.GOK+" installed  ") + ui.Warn.Render(ui.GUpdate+" update  ") + ui.Info.Render(ui.GLocal+" by hand  ") + ui.Dim.Render(ui.GOff+" disabled"),
	}, "\n")
	return m.modal("Keys", body, ui.Void, min(m.modalWidth(), 72))
}

func (m *Model) readmeView() string {
	w, h := m.bodySize()
	vw, _ := m.readmeSize()
	title := ui.Accent.Render(m.readmeTitle) + ui.Dim.Render("  README")
	content := m.vp.View()
	if m.readmeLoading {
		content = "\n  " + m.spin.View() + ui.Dim.Render(" Loading README…")
	}
	pct := ""
	if !m.readmeLoading {
		pct = ui.Fainter.Render(fmt.Sprintf("%3.0f%%", m.vp.ScrollPercent()*100))
	}
	foot := m.hints("↑↓", "scroll", "i", "install", "esc", "back") + "   " + pct
	box := m.frame(lipgloss.NewStyle().Border(m.border).BorderForeground(ui.BorderFocus).
		Padding(0, 1).Width(vw+2).Render(title+"\n"+content+"\n"+foot), ui.BorderFocus)
	box = m.unfold(box, ui.BorderFocus)
	bw := lipgloss.Width(box)
	m.mask = [4]int{max(0, (w-bw)/2), 0, bw, lipgloss.Height(box)}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Top, box)
}

// ---------- footer ----------

func (m *Model) footerView() string {
	var left string
	if m.toast != "" {
		g, st := ui.GOK, ui.OK
		switch m.toastKind {
		case toastWarn:
			g, st = ui.GWarn, ui.Warn
		case toastErr:
			g, st = ui.GError, ui.Bad
		case toastInfo:
			g, st = ui.GInfo, ui.Info
		}
		left = " " + st.Render(g+" "+m.typed(m.toast))
	} else {
		switch m.mode {
		case mSearch:
			left = " " + m.hints("type", "to filter", "enter", "done", "esc", "clear")
		case mNormal:
			switch m.tab {
			case tabBrowse:
				left = " " + m.hints("/", "search", "i", "install", "f", "sort & filter", "s", "sort", "c", "categories", "L", "launch", "?", "help", "q", "quit")
			case tabInstalled:
				left = " " + m.hints("space", "on/off", "u", "update", "U", "update all", "a", "adopt", "x", "remove", "f", "sort & filter", "?", "help")
			case tabDoctor:
				left = " " + m.hints("f", "fix", "F", "fix all", "r", "re-read log", "L", "launch", "?", "help")
			case tabCreate:
				left = " " + m.createFooter()
			case tabSettings:
				left = " " + m.hints("←", "previous", "→", "next", "enter", "change", "?", "help", "q", "quit")
			}
		}
	}
	right := ""
	if m.app.Cat != nil && !m.catLoading {
		right = ui.Fainter.Render("catalog " + ui.Ago(m.app.Cat.Fetched) + " ")
	}
	left = ansi.Truncate(left, max(0, m.w-lipgloss.Width(right)-1), "…")
	return left + strings.Repeat(" ", max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))) + right
}

func compareLess(a, b string) bool { return semver.Less(a, b) }

// clickableKey reports whether a hint stands for a single action a click can trigger.
func clickableKey(k string) bool {
	return k != "type" && !strings.ContainsAny(k, "↑↓")
}

// browseMetric is the right-hand column of a Browse row. It follows the first
// sort level, with a small dotted meter for downloads and rating.
func (m *Model) browseMetric(p *ts.Package, sel bool) []seg {
	meter := func(frac float64) []seg {
		if !ui.Decor {
			return nil
		}
		f, e := ui.Meter(frac, 4)
		return []seg{{f, stVoid}, {e + " ", stFaint}}
	}
	switch m.primarySort().field {
	case "updated":
		return []seg{{fmt.Sprintf("%8s", ui.Ago(p.Updated)), stDim}}
	case "created":
		return []seg{{fmt.Sprintf("new %8s", ui.Ago(p.Created)), stDim}}
	case "size":
		var sz int64
		if l := p.Latest(); l != nil {
			sz = l.Size
		}
		return []seg{{fmt.Sprintf("%8s", ui.Bytes(sz)), stDim}}
	case "rating":
		return append(meter(ui.LogFrac(float64(p.Rating), 1, 2000)), seg{fmt.Sprintf("%s %5d", ui.GStar, p.Rating), stDim})
	}
	return append(meter(ui.LogFrac(float64(p.Downloads), 1000, 5_000_000)), seg{fmt.Sprintf("%s %6s", ui.GDownload, ui.Count(p.Downloads)), stDim})
}

// versionChart is a dotted chart of downloads per version, oldest first.
func versionChart(p *ts.Package, w int) string {
	if !ui.Decor || len(p.Versions) < 2 || w < 20 {
		return ""
	}
	cw := min(w, 48)
	n := min(len(p.Versions), cw*2)
	vals := make([]float64, n)
	for i := 0; i < n; i++ {
		vals[n-1-i] = float64(p.Versions[i].Downloads)
	}
	head := ui.Rule(fmt.Sprintf("Downloads by version · %d", len(p.Versions)), w)
	first, last := p.Versions[n-1].Number, p.Versions[0].Number
	axis := ui.Fainter.Render("v" + first + strings.Repeat(" ", max(1, cw-len(first)-len(last)-2)) + "v" + last)
	return head + "\n" + ui.Spark(vals, cw, 2) + "\n" + axis
}

// emptyState centers a small planet picture over a message.
func emptyState(w, h int, title, sub string) []string {
	out := []string{""}
	if ui.Decor && h >= 14 && w >= 30 {
		pw := min(w-4, 34)
		ph := min(9, h-7)
		for _, l := range strings.Split(ui.Planet(pw, ph), "\n") {
			out = append(out, "  "+l)
		}
		out = append(out, "")
	}
	out = append(out, "  "+ui.Bold.Render(title))
	if sub != "" {
		out = append(out, ui.Dim.Render(indentLines(wrap(sub, w-4), "  ")))
	}
	return out
}
