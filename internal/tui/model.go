// Package tui is the full-screen interactive mod browser.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"ror2mm/internal/config"
	"ror2mm/internal/core"
	"ror2mm/internal/devkit"
	"ror2mm/internal/termimg"
	ts "ror2mm/internal/thunderstore"
	"ror2mm/internal/ui"
)

type tab int

const (
	tabBrowse tab = iota
	tabInstalled
	tabPacks
	tabDoctor
	tabCreate
	tabSettings
	numTabs
)

var tabNames = []string{"Browse", "Installed", "Packs", "Doctor", "Create", "Settings"}
var tabIcons = []string{"⌕", "▤", "❖", "✚", "✦", "⚙"}

type mode int

const (
	mNormal mode = iota
	mSearch
	mConfirm
	mBusy
	mReadme
	mHelp
	mView
	mNewMod
	mPrompt
)

type toastKind int

const (
	toastOK toastKind = iota
	toastWarn
	toastErr
	toastInfo
)

// shared lets background goroutines send messages to the running program.
type shared struct{ p *tea.Program }

func (s *shared) send(msg tea.Msg) {
	if s.p != nil {
		s.p.Send(msg)
	}
}

type confirmBox struct {
	title    string
	body     []string
	yesLabel string
	noLabel  string // defaults to "cancel"
	yes      func(m *Model) tea.Cmd
}

type busyBox struct {
	title   string
	current string
	frac    float64
	hasFrac bool
	done    []string
}

// irow is one line of the grouped Installed list: a section header or a mod.
type irow struct {
	group *core.Group
	entry *core.Entry
}

// Model is the Bubble Tea model.
type Model struct {
	app  *core.App
	sh   *shared
	dark bool
	w, h int
	tab  tab
	mode mode

	search   textinput.Model
	results  []*ts.Package
	cur, off [numTabs]int

	issues []*core.Issue

	irows     []irow
	collapsed map[string]bool

	imgs   *termimg.Store
	imgReq map[string]bool

	confirm      *confirmBox
	busy         *busyBox
	frozenHeader string
	spin         spinner.Model

	vp            viewport.Model
	readmeTitle   string
	readmeLoading bool
	readmeRefs    []termimg.Ref
	readmeBody    string

	vw     *viewPanel
	prompt *promptBox

	toast     string
	toastKind toastKind
	toastID   int

	catLoading bool
	catFrac    float64

	gameRunning bool
	launching   string

	// Create tab.
	projects  []*devkit.Project
	wiz       *wizard
	toolCache *toolStatus
	afterOp   func(*Model) tea.Cmd
	lastErr   error
	instFlat  bool // Installed shows one list without section headers

	// Settings in effect.
	border      lipgloss.Border
	fillBg      bool
	iconCols    int
	iconRows    int
	showDesc    bool
	confirmSkip bool

	// Watching for outside changes.
	diskStamp  string
	exePath    string
	exeTime    time.Time
	binChanged bool
	restart    bool

	fx        effects      // running animations
	roundMore bool         // smooth dotted "extra round" corners
	backdrop  *ui.Backdrop // background art, nil when off
	mask      [4]int       // x, y, w, h of a dialog the background stays out of

	clickables   []string
	lastClick    time.Time
	lastClickRow int
	lastClickTab tab
}

// ---------- messages ----------

type catProgressMsg float64
type catDoneMsg struct {
	cat *ts.Catalog
	err error
}
type opEventMsg core.Event
type opDoneMsg struct {
	ok  string
	err error
}
type readmeMsg struct {
	md  string
	err error
}
type toastClearMsg int
type pollMsg struct {
	game       bool
	stamp      string // when watched files last changed
	binChanged bool   // a newer ror2 was installed since this one started
}
type launchStatusMsg string
type launchDoneMsg struct{ err error }
type imgMsg struct{ url string }

// Run starts the interactive UI.
func Run(a *core.App, query, startTab string) error {
	dark := lipgloss.HasDarkBackground()
	if a.Cat == nil {
		_ = a.LoadCachedCatalog()
	}
	// Image data and screen redraws share one locked stream so they never interleave.
	zone.NewGlobal()
	defer zone.Close()
	out := &termimg.LockedWriter{F: os.Stdout}
	store := termimg.NewStore(filepath.Join(config.CacheDir(), "images"), termimg.Detect(), dark, out)
	m := newModel(a, dark)
	m.imgs = store
	m.applySettings("")
	for i, n := range tabNames {
		if strings.EqualFold(n, startTab) {
			m.tab = tab(i)
		}
	}
	if query != "" {
		m.search.SetValue(query)
		m.tab = tabBrowse
	}
	m.recompute()
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithOutput(out)}
	if a.Cfg.Get("mouse", "on") == "on" {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(m, opts...)
	m.sh.p = p
	_, err := p.Run()
	store.Cleanup()
	if m.restart && err == nil {
		// A newer ror2 was installed: replace this process with it, same tab.
		zone.Close()
		_ = syscall.Exec(m.exePath, []string{m.exePath, "--tab", tabNames[m.tab]}, os.Environ())
	}
	return err
}

func init() {
	ui.OnThemeChange(rebuildStyles)
}

func newModel(a *core.App, dark bool) *Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "search mods…"
	ti.PlaceholderStyle = ui.Fainter
	ti.Cursor.Style = ui.Sel
	ti.TextStyle = ui.Bold
	ti.CharLimit = 80

	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(ui.Sel))
	m := &Model{app: a, sh: &shared{}, dark: dark, search: ti, spin: sp,
		collapsed: map[string]bool{core.LibrariesGroup: true}, imgReq: map[string]bool{},
		border: lipgloss.RoundedBorder(), iconCols: 16, iconRows: 8, showDesc: true}
	m.gameRunning = core.GameRunning()
	m.diskStamp = a.DiskStamp()
	if exe, err := os.Executable(); err == nil {
		m.exePath = exe
		if fi, err := os.Stat(exe); err == nil {
			m.exeTime = fi.ModTime()
		}
	}
	m.reloadProjects()
	m.recompute()
	if len(m.issues) > 0 && m.countSev(core.SevError) > 0 {
		m.tab = tabDoctor
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.poll(), m.ensureIcon()}
	if m.app.CatalogStale() {
		cmds = append(cmds, m.fetchCatalog())
	}
	return tea.Batch(cmds...)
}

// ---------- background work ----------

func (m *Model) fetchCatalog() tea.Cmd {
	m.catLoading = true
	m.catFrac = 0
	prev := m.app.Cat
	sh := m.sh
	return func() tea.Msg {
		last := -1.0
		c, err := ts.Fetch(context.Background(), config.CacheDir(), prev, func(read, total int64) {
			if total <= 0 {
				return
			}
			f := float64(read) / float64(total)
			if f-last >= 0.02 || f >= 1 {
				last = f
				sh.send(catProgressMsg(f))
			}
		})
		return catDoneMsg{c, err}
	}
}

// poll checks every few seconds whether the game runs, whether files changed
// outside the app, and whether a newer ror2 was installed.
func (m *Model) poll() tea.Cmd {
	a, exe, exeTime := m.app, m.exePath, m.exeTime
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		msg := pollMsg{game: core.GameRunning(), stamp: a.DiskStamp()}
		if fi, err := os.Stat(exe); err == nil && exe != "" && fi.ModTime().After(exeTime) {
			msg.binChanged = true
		}
		return msg
	})
}

// reloadData rereads everything from disk and refreshes every tab.
func (m *Model) reloadData() error {
	if err := m.app.Reload(); err != nil {
		return err
	}
	m.diskStamp = m.app.DiskStamp()
	m.reloadProjects()
	m.recompute()
	return nil
}

// reloadKey reloads data, or restarts into a newly installed ror2.
func (m *Model) reloadKey() tea.Cmd {
	if m.binChanged {
		m.restart = true
		return tea.Quit
	}
	if err := m.reloadData(); err != nil {
		return m.showToast(toastErr, "Reload failed: "+err.Error())
	}
	return m.showToast(toastOK, "Reloaded mods, game log and settings.")
}

func (m *Model) showToast(kind toastKind, msg string) tea.Cmd {
	m.toastID++
	m.toast, m.toastKind = msg, kind
	id := m.toastID
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return toastClearMsg(id) })
}

// runOp shows the busy box and runs fn in the background.
func (m *Model) runOp(title string, fn func(progress func(core.Event)) (string, error)) tea.Cmd {
	m.frozenHeader = m.headerView()
	m.mode = mBusy
	m.busy = &busyBox{title: title}
	sh := m.sh
	return func() tea.Msg {
		var lastSent time.Time
		var lastPhase string
		progress := func(ev core.Event) {
			// Downloads report often; only pass on a few updates per second.
			if ev.Phase == "download" && ev.Phase == lastPhase && ev.Read != ev.Size && time.Since(lastSent) < 80*time.Millisecond {
				return
			}
			lastSent, lastPhase = time.Now(), ev.Phase
			sh.send(opEventMsg(ev))
		}
		ok, err := fn(progress)
		return opDoneMsg{ok, err}
	}
}

// recompute refreshes search results and doctor findings after any change.
func (m *Model) recompute() {
	if m.app.Cat != nil {
		m.results = m.app.Cat.Search(m.search.Value(), m.searchOptions())
	}
	m.issues = m.app.Doctor()
	m.buildRows()
	m.clampAll()
}

// buildRows lays out the grouped Installed list, honoring collapsed sections.
func (m *Model) buildRows() {
	var keep *core.Entry
	var keepGroup string
	if c := m.cur[tabInstalled]; c < len(m.irows) {
		keep = m.irows[c].entry
		if g := m.irows[c].group; g != nil {
			keepGroup = g.Name
		}
	}
	m.irows = m.irows[:0]
	groups, flat := m.installedGroups()
	m.instFlat = flat
	for _, g := range groups {
		if !flat {
			m.irows = append(m.irows, irow{group: g})
		}
		if !flat && m.collapsed[g.Name] {
			continue
		}
		for _, e := range g.Entries {
			m.irows = append(m.irows, irow{entry: e})
		}
	}
	// Keep the cursor on the same mod or header after a change.
	for i, r := range m.irows {
		if (keep != nil && r.entry != nil && r.entry.FullName == keep.FullName && r.entry.Name == keep.Name) ||
			(keep == nil && r.group != nil && r.group.Name == keepGroup && keepGroup != "") {
			m.cur[tabInstalled] = i
			break
		}
	}
}

// selGroup returns the section header under the cursor on the Installed tab.
func (m *Model) selGroup() *core.Group {
	if c := m.cur[tabInstalled]; m.tab == tabInstalled && c < len(m.irows) {
		return m.irows[c].group
	}
	return nil
}

func (m *Model) toggleGroup(g *core.Group) {
	m.collapsed[g.Name] = !m.collapsed[g.Name]
	m.buildRows()
	m.clampAll()
}

// ensureIcon starts loading the selected mod's icon if needed.
func (m *Model) ensureIcon() tea.Cmd {
	if m.imgs == nil || m.imgs.Mode == termimg.Off {
		return nil
	}
	p := m.selPackage()
	if p == nil || p.Icon == "" {
		return nil
	}
	return m.loadImage(p.Icon)
}

func (m *Model) loadImage(url string) tea.Cmd {
	if m.imgReq[url] || m.imgs.Has(url) {
		return nil
	}
	m.imgReq[url] = true
	store := m.imgs
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = store.Load(ctx, url)
		return imgMsg{url}
	}
}

// composeReadme redraws the README with whatever images have arrived.
func (m *Model) composeReadme() {
	vw, vh := m.readmeSize()
	off := m.vp.YOffset
	m.vp.SetContent(termimg.Compose(m.readmeBody, m.readmeRefs, m.imgs, vw-6, max(4, vh-2), "◌"))
	m.vp.SetYOffset(off)
}

func (m *Model) countSev(s core.Severity) int {
	n := 0
	for _, is := range m.issues {
		if is.Sev == s {
			n++
		}
	}
	return n
}

func (m *Model) listLen(t tab) int {
	switch t {
	case tabBrowse:
		return len(m.results)
	case tabInstalled:
		return len(m.irows)
	case tabSettings:
		return len(m.settings())
	case tabCreate:
		return m.createLen()
	case tabPacks:
		return m.packsLen()
	default:
		return len(m.issues)
	}
}

func (m *Model) clampAll() {
	for t := tab(0); t < numTabs; t++ {
		n := m.listLen(t)
		if m.cur[t] >= n {
			m.cur[t] = n - 1
		}
		if m.cur[t] < 0 {
			m.cur[t] = 0
		}
	}
}

func (m *Model) move(delta int) {
	n := m.listLen(m.tab)
	if n == 0 {
		return
	}
	c := m.cur[m.tab] + delta
	if c < 0 {
		c = 0
	}
	if c >= n {
		c = n - 1
	}
	m.cur[m.tab] = c
}

func (m *Model) selPackage() *ts.Package {
	switch m.tab {
	case tabBrowse:
		if c := m.cur[tabBrowse]; c < len(m.results) {
			return m.results[c]
		}
	case tabInstalled:
		if e := m.selEntry(); e != nil {
			return e.Pkg
		}
	}
	return nil
}

func (m *Model) selEntry() *core.Entry {
	switch m.tab {
	case tabInstalled:
		if c := m.cur[tabInstalled]; c < len(m.irows) {
			return m.irows[c].entry
		}
	case tabBrowse:
		if p := m.selPackage(); p != nil {
			return m.app.Inv.Find(p.FullName)
		}
	}
	return nil
}

func (m *Model) selIssue() *core.Issue {
	if c := m.cur[tabDoctor]; m.tab == tabDoctor && c < len(m.issues) {
		return m.issues[c]
	}
	return nil
}

// ---------- update ----------

// Update handles a message, then starts any animation the change calls for.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(animTickMsg); ok {
		if m.fx.any() {
			return m, animTick()
		}
		m.fx.ticking = false
		return m, nil
	}
	before := m.snap()
	model, cmd := m.update(msg)
	if m.startEffects(before, msg) && !m.fx.ticking {
		m.fx.ticking = true
		cmd = tea.Batch(cmd, animTick())
	}
	return model, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.search.Width = max(10, m.w/3)
		if m.mode == mReadme {
			m.vp.Width, m.vp.Height = m.readmeSize()
		}
		return m, nil

	case spinner.TickMsg:
		m.spin.Style = ui.Sel
		m.search.PlaceholderStyle, m.search.Cursor.Style, m.search.TextStyle = ui.Fainter, ui.Sel, ui.Bold
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case toastClearMsg:
		if int(msg) == m.toastID {
			m.toast = ""
		}
		return m, nil

	case pollMsg:
		var cmd tea.Cmd
		if m.gameRunning && !msg.game && m.mode != mBusy {
			// The game just closed: its log now tells us what loaded.
			m.app.ReloadLog()
			m.recompute()
			if n := m.countSev(core.SevError); n > 0 {
				cmd = m.showToast(toastWarn, fmt.Sprintf("Game closed. Doctor found %d problem(s) in the log.", n))
			} else {
				cmd = m.showToast(toastOK, "Game closed. The BepInEx log looks clean.")
			}
		}
		if msg.game {
			m.launching = ""
		}
		m.gameRunning = msg.game
		m.binChanged = msg.binChanged
		// Something outside the app changed our files (a ror2 command in a
		// terminal, the game writing its log): refresh quietly.
		if msg.stamp != m.diskStamp && (m.mode == mNormal || m.mode == mSearch || m.mode == mHelp) {
			if err := m.reloadData(); err == nil && cmd == nil {
				cmd = m.showToast(toastInfo, "Refreshed: files changed outside the app.")
			}
		}
		return m, tea.Batch(cmd, m.poll())

	case catProgressMsg:
		m.catFrac = float64(msg)
		return m, nil

	case catDoneMsg:
		m.catLoading = false
		if msg.cat != nil {
			m.app.Cat = msg.cat
			m.app.Refresh()
			m.recompute()
		}
		iconCmd := m.ensureIcon()
		if msg.err != nil {
			return m, tea.Batch(iconCmd, m.showToast(toastErr, "Catalog: "+msg.err.Error()))
		}
		if n := len(m.app.Inv.Updates()); n > 0 {
			return m, tea.Batch(iconCmd, m.showToast(toastInfo, fmt.Sprintf("Catalog updated · %d of your mods have updates", n)))
		}
		return m, iconCmd

	case opEventMsg:
		m.onEvent(core.Event(msg))
		return m, nil

	case opDoneMsg:
		m.mode = mNormal
		m.busy = nil
		m.lastErr = msg.err
		defer func() { m.diskStamp = m.app.DiskStamp() }()
		m.app.Refresh()
		m.recompute()
		var after tea.Cmd
		if m.afterOp != nil {
			f := m.afterOp
			m.afterOp = nil
			after = f(m)
		}
		if msg.err != nil {
			text := "Stopped: " + msg.err.Error()
			if m.tab != tabCreate {
				text += " (a backup was taken first)"
			}
			return m, tea.Batch(after, m.showToast(toastErr, text))
		}
		return m, tea.Batch(after, m.showToast(toastOK, msg.ok))

	case agentDoneMsg:
		defer func() { m.diskStamp = m.app.DiskStamp() }()
		m.reloadProjects()
		m.app.ReloadLog()
		m.recompute()
		if msg.err != nil && msg.quick && msg.retry != nil {
			body := []string{ui.Dim.Render(wrap(msg.name+" closed right away ("+msg.err.Error()+").", m.modalWidth()-8)), ""}
			if msg.name == "Claude Code" {
				body = append(body, ui.Dim.Render(wrap(devkit.TrustHint, m.modalWidth()-8)))
			}
			m.confirm = &confirmBox{title: msg.name + " did not start", body: body,
				yesLabel: "try again", noLabel: "close", yes: msg.retry}
			m.mode = mConfirm
			return m, nil
		}
		if msg.err != nil {
			return m, m.showToast(toastWarn, msg.name+" exited: "+msg.err.Error())
		}
		return m, m.showToast(toastOK, "Back from "+msg.name+".")

	case readmeMsg:
		m.readmeLoading = false
		if msg.err != nil {
			m.vp.SetContent(ui.Dim.Render("\n  README unavailable: " + msg.err.Error()))
			return m, nil
		}
		w, _ := m.readmeSize()
		md, refs := termimg.ExtractImages(msg.md)
		m.readmeRefs = refs
		m.readmeBody = ui.RenderMarkdown(md, w-2, m.dark)
		m.composeReadme()
		var cmds []tea.Cmd
		if m.imgs.Mode != termimg.Off {
			for _, r := range refs {
				cmds = append(cmds, m.loadImage(r.URL))
			}
		}
		return m, tea.Batch(cmds...)

	case imgMsg:
		if m.mode == mReadme {
			for _, r := range m.readmeRefs {
				if r.URL == msg.url {
					m.composeReadme()
					break
				}
			}
		}
		return m, nil

	case launchStatusMsg:
		m.launching = string(msg)
		return m, nil

	case fileChosenMsg:
		return m, m.onFileChosen(msg)

	case launchDoneMsg:
		if msg.err != nil {
			m.launching = ""
			return m, m.showToast(toastErr, msg.err.Error())
		}
		m.launching = "Steam is starting Risk of Rain 2…"
		return m, m.showToast(toastOK, "Launch sent to Steam. Doctor re-checks the log when the game closes.")

	case tea.MouseMsg:
		return m, tea.Batch(m.onMouse(msg), m.ensureIcon())

	case tea.KeyMsg:
		// Fast typing or a held key can arrive as several runes at once.
		// Outside the search box, treat each one as its own key press.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste && m.mode != mSearch && m.mode != mNewMod && m.mode != mPrompt {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				cmds = append(cmds, m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
			}
			return m, tea.Batch(append(cmds, m.ensureIcon())...)
		}
		return m, tea.Batch(m.onKey(msg), m.ensureIcon())
	}
	return m, nil
}

func (m *Model) onEvent(ev core.Event) {
	b := m.busy
	if b == nil {
		return
	}
	label := fmt.Sprintf("%s %s", ev.Step.Pkg.Name, ev.Step.Version)
	count := fmt.Sprintf("[%d/%d] ", ev.Index+1, ev.Total)
	switch {
	case ev.Err != nil:
		b.done = append(b.done, ui.Bad.Render(ui.GError+" "+label+": "+ev.Err.Error()))
	case ev.Phase == "download":
		b.current = count + "Downloading " + label
		b.hasFrac = ev.Size > 0
		if b.hasFrac {
			b.frac = float64(ev.Read) / float64(ev.Size)
		}
	case ev.Phase == "install":
		b.current = count + "Installing " + label
		b.hasFrac = false
	case ev.Phase == "done":
		verb := map[core.StepKind]string{core.StepInstall: "Installed", core.StepUpdate: "Updated", core.StepAdopt: "Adopted", core.StepReinstall: "Reinstalled"}[ev.Step.Kind]
		b.done = append(b.done, ui.OK.Render(ui.GOK)+" "+verb+" "+ui.Bold.Render(ev.Step.Pkg.Name)+" "+ui.Dim.Render(ev.Step.Version))
	}
}

func (m *Model) onMouse(msg tea.MouseMsg) tea.Cmd {
	if m.mode == mReadme {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			return m.onClick(msg)
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return cmd
	}
	if m.mode == mView && msg.Action == tea.MouseActionPress {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.viewKey("up")
		case tea.MouseButtonWheelDown:
			return m.viewKey("down")
		}
	}
	if m.mode != mNormal && m.mode != mSearch {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && m.mode != mBusy {
			return m.onClick(msg)
		}
		return nil
	}
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		m.move(-3)
	case msg.Button == tea.MouseButtonWheelDown:
		m.move(3)
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		return m.onClick(msg)
	}
	return nil
}

// clicked returns the id of the clickable area under the mouse, if any.
// Later-drawn areas win, so a button inside a row beats the row.
func (m *Model) clicked(msg tea.MouseMsg) string {
	for i := len(m.clickables) - 1; i >= 0; i-- {
		if z := zone.Get(m.clickables[i]); z != nil && z.InBounds(msg) {
			return m.clickables[i]
		}
	}
	return ""
}

// keyFor builds the key press a clickable hint stands for.
func keyFor(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "f5":
		return tea.KeyMsg{Type: tea.KeyF5}
	case "←":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "→":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

const doubleClick = 450 * time.Millisecond

func (m *Model) onClick(msg tea.MouseMsg) tea.Cmd {
	id := m.clicked(msg)
	var n int
	if m.mode == mSearch {
		switch id {
		case "key:/", "key:type":
			return nil // clicking the search box keeps typing in it
		case "key:enter", "key:esc":
			// handled by the search keys below
		default:
			// Any other click leaves the search box (keeping the query),
			// then acts like a normal click.
			m.search.Blur()
			m.mode = mNormal
		}
	}
	switch {
	case strings.HasPrefix(id, "key:"):
		k := strings.TrimPrefix(id, "key:")
		if i := strings.LastIndex(k, "#"); i > 0 {
			k = k[:i]
		}
		return m.onKey(keyFor(k))
	case strings.HasPrefix(id, "tab:"):
		fmt.Sscanf(id, "tab:%d", &n)
		switch m.mode {
		case mConfirm, mView, mBusy, mNewMod, mPrompt:
			return nil // finish the dialog first
		case mSearch:
			m.search.Blur()
		}
		m.mode = mNormal
		m.tab = tab(n)
	case strings.HasPrefix(id, "set:"):
		fmt.Sscanf(id, "set:%d", &n)
		if m.cur[tabSettings] == n {
			return m.changeSetting(n, 1)
		}
		m.cur[tabSettings] = n
	case strings.HasPrefix(id, "opt:"):
		fmt.Sscanf(id, "opt:%d", &n)
		if list := m.settings(); m.cur[tabSettings] < len(list) {
			s := list[m.cur[tabSettings]]
			if n < len(s.values) {
				return m.setSetting(s, s.values[n])
			}
		}
	case strings.HasPrefix(id, "cr:"):
		return m.createClick(id)
	case strings.HasPrefix(id, "vw:"):
		fmt.Sscanf(id, "vw:%d", &n)
		return m.viewClick(n)
	case strings.HasPrefix(id, "row:"):
		fmt.Sscanf(id, "row:%d", &n)
		if m.mode == mSearch {
			m.search.Blur()
			m.mode = mNormal
		}
		again := m.cur[m.tab] == n && m.lastClickRow == n && m.lastClickTab == m.tab && time.Since(m.lastClick) < doubleClick
		m.cur[m.tab] = n
		m.lastClick, m.lastClickRow, m.lastClickTab = time.Now(), n, m.tab
		// Section headers open and close on a single click.
		if g := m.selGroup(); g != nil {
			m.toggleGroup(g)
			return nil
		}
		if again {
			m.lastClick = time.Time{}
			switch m.tab {
			case tabDoctor:
				return m.onKey(keyFor("f"))
			case tabCreate, tabPacks:
				return m.onKey(keyFor("enter"))
			}
			return m.actReadme()
		}
	default:
		// A click outside any button closes the help screen.
		if m.mode == mHelp {
			m.mode = mNormal
		}
	}
	return nil
}

func (m *Model) onKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	// Reload is ` or F5 (no modifier keys needed). Not while typing.
	if (key == "`" || key == "f5") && m.mode != mBusy && m.mode != mNewMod && m.mode != mSearch {
		m.mode = mNormal
		return m.reloadKey()
	}
	switch m.mode {
	case mBusy:
		return nil
	case mNewMod:
		return m.wizardKey(msg)
	case mPrompt:
		return m.promptKey(msg)
	case mHelp:
		m.mode = mNormal
		return nil
	case mConfirm:
		switch key {
		case "y", "Y", "enter":
			c := m.confirm
			m.confirm = nil
			m.mode = mNormal
			return c.yes(m)
		case "n", "N", "esc", "q":
			m.confirm = nil
			m.mode = mNormal
		}
		return nil
	case mReadme:
		switch key {
		case "esc", "q", "enter", "backspace":
			m.mode = mNormal
			return nil
		case "i":
			m.mode = mNormal
			return m.actInstall()
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return cmd
	case mView:
		return m.viewKey(key)
	case mSearch:
		switch key {
		case "esc":
			m.search.SetValue("")
			m.search.Blur()
			m.mode = mNormal
			m.cur[tabBrowse] = 0
			m.recompute()
			return nil
		case "enter", "tab":
			m.search.Blur()
			m.mode = mNormal
			return nil
		case "up", "down", "pgup", "pgdown":
			m.navKey(key)
			return nil
		}
		before := m.search.Value()
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		if m.search.Value() != before {
			m.cur[tabBrowse], m.off[tabBrowse] = 0, 0
			m.recompute()
		}
		return cmd
	}

	// Normal mode.
	if m.navKey(key) {
		return nil
	}
	if m.tab == tabSettings {
		switch key {
		case "right", "l", "enter", " ":
			return m.changeSetting(m.cur[tabSettings], 1)
		case "left", "h":
			return m.changeSetting(m.cur[tabSettings], -1)
		}
	}
	if m.tab == tabCreate {
		if cmd, handled := m.createKey(key); handled {
			return cmd
		}
	}
	if m.tab == tabPacks {
		if cmd, handled := m.packsKey(key); handled {
			return cmd
		}
	}
	switch key {
	case "q":
		return tea.Quit
	case "?":
		m.mode = mHelp
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % numTabs
	case "shift+tab", "left", "h":
		m.tab = (m.tab + numTabs - 1) % numTabs
	case "1", "2", "3", "4", "5", "6":
		m.tab = tab(key[0] - '1')
		if m.tab == tabCreate {
			m.reloadProjects()
		}
	case ",":
		m.tab = tabSettings
	case "/":
		m.tab = tabBrowse
		m.mode = mSearch
		return m.search.Focus()
	case "esc":
		if m.tab == tabBrowse && m.search.Value() != "" {
			m.search.SetValue("")
			m.cur[tabBrowse] = 0
			m.recompute()
		}
	case "R":
		if !m.catLoading {
			return m.fetchCatalog()
		}
	case "L":
		return m.actLaunch()
	case "enter":
		if g := m.selGroup(); g != nil {
			m.toggleGroup(g)
			return nil
		}
		return m.actReadme()
	}

	switch m.tab {
	case tabBrowse:
		switch key {
		case "s", "S":
			// s: next field for the first sort level; S: flip its direction.
			keys := m.sorts(tabBrowse)
			if key == "s" {
				keys[0].field = m.otherField(tabBrowse, keys, 0, 1)
			} else {
				keys[0].reverse = !keys[0].reverse
			}
			m.setSorts(tabBrowse, keys)
			return m.showToast(toastInfo, "Sorted by "+m.sortSummary(tabBrowse))
		case "f", "o":
			return m.openView(tabBrowse, false)
		case "c":
			return m.openView(tabBrowse, true)
		case "b":
			return m.flipToggle(browseToggles, "browse_libraries")
		case "D":
			return m.flipToggle(browseToggles, "deprecated")
		case "i":
			return m.actInstall()
		case "u":
			return m.actUpdate()
		case "x":
			return m.actRemove()
		}
	case tabInstalled:
		switch key {
		case " ", "e":
			if g := m.selGroup(); g != nil {
				m.toggleGroup(g)
				return nil
			}
			return m.actToggle()
		case "z":
			// Collapse or expand every section at once.
			allOpen := true
			for _, g := range m.app.Inv.Groups() {
				if m.collapsed[g.Name] {
					allOpen = false
				}
			}
			for _, g := range m.app.Inv.Groups() {
				m.collapsed[g.Name] = allOpen
			}
			m.buildRows()
			m.clampAll()
		case "u":
			return m.actUpdate()
		case "U":
			return m.actUpdateAll()
		case "x":
			return m.actRemove()
		case "a":
			return m.actAdopt()
		case "f", "o":
			return m.openView(tabInstalled, false)
		}
	case tabDoctor:
		switch key {
		case "f":
			if is := m.selIssue(); is != nil && is.Fix != nil {
				return m.actFix([]*core.Fix{is.Fix}, is.Fix.Label)
			}
			return m.showToast(toastInfo, "No automatic fix for this one.")
		case "F":
			var fixes []*core.Fix
			for _, is := range m.issues {
				if is.Fix != nil && is.Sev > core.SevInfo {
					fixes = append(fixes, is.Fix)
				}
			}
			if len(fixes) == 0 {
				return m.showToast(toastInfo, "No problems or warnings to fix. Use f on a note to apply it.")
			}
			return m.actFix(fixes, "Fix all problems and warnings")
		case "A":
			var fixes []*core.Fix
			for _, is := range m.issues {
				if is.Fix != nil {
					fixes = append(fixes, is.Fix)
				}
			}
			if len(fixes) == 0 {
				return m.showToast(toastInfo, "Nothing to fix.")
			}
			return m.actFix(fixes, "Fix everything, including notes")
		case "r":
			m.app.ReloadLog()
			m.recompute()
			return m.showToast(toastOK, "Re-read the BepInEx log.")
		}
	}
	return nil
}

func (m *Model) navKey(key string) bool {
	_, bodyH := m.bodySize()
	page := max(1, bodyH-4)
	switch key {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-page)
	case "pgdown", "ctrl+d":
		m.move(page)
	case "home", "g":
		m.move(-1 << 30)
	case "end", "G":
		m.move(1 << 30)
	default:
		return false
	}
	return true
}

// ---------- actions ----------

func (m *Model) needCatalog() tea.Cmd {
	if m.app.Cat == nil {
		return m.showToast(toastWarn, "The Thunderstore catalog is still loading…")
	}
	return nil
}

func (m *Model) askPlan(title string, plan *core.Plan, extra []string, yesLabel string, run func(m *Model) tea.Cmd) tea.Cmd {
	if plan.Empty() && len(extra) == 0 {
		msg := "Nothing to do."
		if len(plan.Skipped) > 0 {
			msg = plan.Skipped[0]
		}
		return m.showToast(toastInfo, msg)
	}
	if m.confirmSkip {
		return run(m)
	}
	body := planLines(plan)
	body = append(body, extra...)
	if core.GameRunning() {
		body = append(body, "", ui.Warn.Render(ui.GWarn+" The game is running; changes apply after a restart."))
	}
	m.confirm = &confirmBox{title: title, body: body, yesLabel: yesLabel, yes: run}
	m.mode = mConfirm
	return nil
}

func planLines(plan *core.Plan) []string {
	var out []string
	nameW, verW := 0, 0
	vers := make([]string, len(plan.Steps))
	for i, s := range plan.Steps {
		nameW = max(nameW, lipgloss.Width(s.Pkg.Name))
		vers[i] = s.Version
		if s.From != "" && s.From != s.Version {
			vers[i] = ui.Dim.Render(s.From+" "+ui.GArrow+" ") + s.Version
		}
		verW = max(verW, lipgloss.Width(vers[i]))
	}
	var size int64
	for i, s := range plan.Steps {
		g := ui.OK.Render("+")
		switch s.Kind {
		case core.StepUpdate:
			g = ui.Warn.Render(ui.GUpdate)
		case core.StepAdopt:
			g = ui.Info.Render(ui.GLocal)
		case core.StepReinstall:
			g = ui.Dim.Render("↻")
		}
		out = append(out, fmt.Sprintf("%s %s  %s  %s", g,
			lipgloss.NewStyle().Width(nameW).Render(ui.Bold.Render(s.Pkg.Name)),
			lipgloss.NewStyle().Width(verW).Render(vers[i]),
			ui.Fainter.Render(s.Reason)))
		if v := s.Pkg.Version(s.Version); v != nil {
			size += v.Size
		}
	}
	if len(plan.Steps) > 0 {
		out = append(out, "", ui.Dim.Render(fmt.Sprintf("%d package(s) · %s to download · BepInEx is backed up first", len(plan.Steps), ui.Bytes(size))))
	}
	for _, w := range plan.Warnings {
		out = append(out, ui.Warn.Render(ui.GWarn+" "+w))
	}
	return out
}

func applyCmd(plan *core.Plan, label, title string) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		a := m.app
		return m.runOp(title, func(progress func(core.Event)) (string, error) {
			err := a.Apply(context.Background(), plan, label, progress)
			return fmt.Sprintf("Done · %d package(s) changed", len(plan.Steps)), err
		})
	}
}

func (m *Model) actInstall() tea.Cmd {
	if c := m.needCatalog(); c != nil {
		return c
	}
	p := m.selPackage()
	if p == nil {
		return nil
	}
	plan, err := m.app.PlanInstall([]core.Request{{FullName: p.FullName}})
	if err != nil {
		return m.showToast(toastErr, err.Error())
	}
	return m.askPlan("Install "+p.Name, plan, nil, "install", applyCmd(plan, "before-install", "Installing "+p.Name))
}

func (m *Model) actUpdate() tea.Cmd {
	if c := m.needCatalog(); c != nil {
		return c
	}
	e := m.selEntry()
	if e == nil {
		return m.showToast(toastInfo, "Not installed.")
	}
	if e.FullName == "" {
		return m.showToast(toastInfo, e.Name+" is not on Thunderstore.")
	}
	if !e.HasUpdate() {
		return m.showToast(toastInfo, e.Name+" is up to date.")
	}
	plan, err := m.app.PlanUpdate([]string{e.FullName})
	if err != nil {
		return m.showToast(toastErr, err.Error())
	}
	return m.askPlan("Update "+e.Name, plan, nil, "update", applyCmd(plan, "before-update", "Updating "+e.Name))
}

func (m *Model) actUpdateAll() tea.Cmd {
	if c := m.needCatalog(); c != nil {
		return c
	}
	plan, err := m.app.PlanUpdate(nil)
	if err != nil {
		return m.showToast(toastErr, err.Error())
	}
	var extra []string
	var local []string
	for _, e := range m.app.Inv.Entries {
		if !e.Managed && e.HasUpdate() {
			local = append(local, e.Name)
		}
	}
	if len(local) > 0 {
		extra = append(extra, "", ui.Info.Render(ui.GLocal+" Hand-installed mods with updates (adopt them first with a): "+strings.Join(local, ", ")))
	}
	if plan.Empty() {
		if len(local) > 0 {
			return m.showToast(toastInfo, "Tracked mods are up to date. Hand-installed ones need adopting first (a).")
		}
		return m.showToast(toastOK, "Everything is up to date.")
	}
	return m.askPlan("Update all", plan, extra, "update", applyCmd(plan, "before-update", "Updating mods"))
}

func (m *Model) actAdopt() tea.Cmd {
	if c := m.needCatalog(); c != nil {
		return c
	}
	e := m.selEntry()
	if e == nil || e.Managed {
		return m.showToast(toastInfo, "Pick a hand-installed mod (◆) to adopt.")
	}
	if e.Pkg == nil {
		return m.showToast(toastInfo, e.Name+" is not on Thunderstore, so it stays as it is.")
	}
	plan, err := m.app.PlanAdopt([]*core.Entry{e})
	if err != nil {
		return m.showToast(toastErr, err.Error())
	}
	return m.askPlan("Adopt "+e.Name, plan, nil, "adopt", applyCmd(plan, "before-adopt", "Adopting "+e.Name))
}

func (m *Model) actRemove() tea.Cmd {
	e := m.selEntry()
	if e == nil {
		return m.showToast(toastInfo, "Not installed.")
	}
	body := []string{ui.Bad.Render("−") + " " + ui.Bold.Render(e.Name) + " " + ui.Dim.Render(e.Version)}
	if !e.Managed {
		body = append(body, ui.Info.Render("  hand-installed: "+strings.Join(e.Local.Paths, ", ")))
	}
	var needers []string
	for _, d := range m.app.Inv.Dependents(e.FullName) {
		if d != e {
			needers = append(needers, d.Name)
		}
	}
	if len(needers) > 0 {
		body = append(body, "", ui.Warn.Render(ui.GWarn+" Still needed by "+strings.Join(needers, ", ")))
	}
	body = append(body, "", ui.Dim.Render("Config files are kept. BepInEx is backed up first."))
	m.confirm = &confirmBox{title: "Remove " + e.Name, body: body, yesLabel: "remove", yes: func(m *Model) tea.Cmd {
		a := m.app
		return m.runOp("Removing "+e.Name, func(func(core.Event)) (string, error) {
			var err error
			if e.Managed {
				err = a.Remove([]string{e.FullName})
			} else {
				err = a.RemoveLocalEntries([]*core.Entry{e})
			}
			return "Removed " + e.Name, err
		})
	}}
	m.mode = mConfirm
	return nil
}

func (m *Model) actToggle() tea.Cmd {
	e := m.selEntry()
	if e == nil {
		return nil
	}
	on := !e.Enabled
	var err error
	if e.Managed {
		err = m.app.SetEnabled([]string{e.FullName}, on)
	} else {
		err = m.app.SetLocalEnabled([]*core.Entry{e}, on)
	}
	if err != nil {
		return m.showToast(toastErr, err.Error())
	}
	m.diskStamp = m.app.DiskStamp()
	m.recompute()
	if on {
		return m.showToast(toastOK, e.Name+" is on")
	}
	var needers []string
	for _, d := range m.app.Inv.Dependents(e.FullName) {
		needers = append(needers, d.Name)
	}
	if len(needers) > 0 {
		return m.showToast(toastWarn, e.Name+" is off, but "+strings.Join(needers, ", ")+" need it")
	}
	return m.showToast(toastOK, e.Name+" is off")
}

func (m *Model) actFix(fixes []*core.Fix, title string) tea.Cmd {
	if c := m.needCatalog(); c != nil {
		return c
	}
	batch, err := m.app.PlanFixes(fixes)
	if err != nil {
		return m.showToast(toastErr, err.Error())
	}
	var extra []string
	for _, n := range batch.Enable {
		_, name := ts.SplitFullName(n)
		extra = append(extra, ui.OK.Render(ui.GOn)+" enable "+ui.Bold.Render(name))
	}
	for _, n := range batch.Disable {
		_, name := ts.SplitFullName(n)
		extra = append(extra, ui.Dim.Render(ui.GOff)+" turn off "+ui.Bold.Render(name))
	}
	for _, n := range batch.Remove {
		_, name := ts.SplitFullName(n)
		extra = append(extra, ui.Bad.Render("−")+" remove "+ui.Bold.Render(name))
	}
	for _, e := range batch.RemoveLocal {
		extra = append(extra, ui.Bad.Render("−")+" remove hand-installed "+strings.Join(e.Local.Paths, ", "))
	}
	if batch.Empty() {
		return m.showToast(toastInfo, "Nothing to do.")
	}
	return m.askPlan(title, batch.Plan, extra, "fix", func(m *Model) tea.Cmd {
		a := m.app
		return m.runOp("Fixing", func(progress func(core.Event)) (string, error) {
			err := a.ApplyFixes(context.Background(), batch, progress)
			left := 0
			for _, is := range a.Doctor() {
				if is.Sev == core.SevError {
					left++
				}
			}
			if left > 0 {
				return fmt.Sprintf("Fixes applied · %d problem(s) remain", left), err
			}
			return "Fixed · launch the game (L) to confirm everything loads", err
		})
	})
}

func (m *Model) actReadme() tea.Cmd {
	p := m.selPackage()
	if p == nil {
		if m.tab == tabDoctor {
			return nil
		}
		return m.showToast(toastInfo, "No README: this mod is not on Thunderstore.")
	}
	ver := p.Latest().Number
	m.mode = mReadme
	m.readmeLoading = true
	m.readmeTitle = p.Name + " " + ver
	m.readmeRefs, m.readmeBody = nil, ""
	w, h := m.readmeSize()
	m.vp = viewport.New(w, h)
	m.vp.SetContent("")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		md, err := ts.Readme(ctx, p, ver)
		return readmeMsg{md, err}
	}
}

func (m *Model) actLaunch() tea.Cmd {
	if m.gameRunning {
		return m.showToast(toastInfo, "Risk of Rain 2 is already running.")
	}
	if m.launching != "" {
		return nil
	}
	m.launching = "Starting…"
	a, sh := m.app, m.sh
	return func() tea.Msg {
		err := a.Launch(func(s string) { sh.send(launchStatusMsg(s)) })
		return launchDoneMsg{err}
	}
}
