package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ror2mm/internal/config"
	"ror2mm/internal/devkit"
	"ror2mm/internal/game"
	"ror2mm/internal/termimg"
	"ror2mm/internal/ui"
)

// setting is one row of the Settings tab: a choice, an action or plain info.
type setting struct {
	section string
	key     string // config key for choices
	label   string
	help    string
	values  []string
	labels  []string // display names, same order as values (optional)
	def     string
	action  func(m *Model) tea.Cmd
	info    func(m *Model) string
}

func (s setting) isChoice() bool { return len(s.values) > 0 }

func (s setting) display(v string) string {
	for i, x := range s.values {
		if x == v && i < len(s.labels) {
			return s.labels[i]
		}
	}
	return v
}

func (m *Model) settings() []setting {
	imgAuto := "Auto (" + map[termimg.Mode]string{termimg.Kitty: "sharp", termimg.Blocks: "half-blocks", termimg.Off: "off"}[termimg.Detect()] + ")"
	return []setting{
		{section: "Appearance", key: "theme", label: "Theme", values: ui.ThemeNames(), def: ui.ThemeByName("", m.dark).Name,
			help: "Colors for the whole app, READMEs included. Use ← → to flip through them live."},
		{section: "Appearance", key: "background", label: "Background", values: []string{"theme", "terminal"},
			labels: []string{"Theme color", "Terminal's own"}, def: "theme",
			help: "Paint the theme's background behind everything so it looks the same in any terminal, or keep your terminal's background (and transparency)."},
		{section: "Appearance", key: "borders", label: "Borders", values: []string{"extra-round", "rounded", "thick", "double", "square", "dotted", "braille"},
			labels: []string{"Extra round (dots)", "Rounded", "Thick", "Double", "Square", "Dotted", "Braille dots"}, def: "rounded", help: "Frame style for panels and dialogs. Extra round draws a fine dotted frame with wide, smooth corners (terminals can only draw a bigger curve with braille dots). Dotted uses dashed lines; Braille dots draws a fine frame out of dots."},
		{section: "Appearance", key: "backdrop", label: "Background art", values: ui.BackdropKinds,
			labels: []string{"None", "Starfield", "Rain", "Dot grid", "Grid lines", "Waves", "Planet", "Picture (dots)", "Picture (soft blocks)", "Text art"}, def: "off",
			help: "A faint pattern, picture or text art behind everything. It only fills empty space, so text stays readable. Pick a picture or a text art file below. With animations on, stars twinkle, rain falls, waves roll and the planet turns."},
		{section: "Appearance", key: "backdrop_strength", label: "Background strength", values: []string{"faint", "soft", "bold"},
			labels: []string{"Faint", "Soft", "Bold"}, def: "soft", help: "How visible the background art is."},
		{section: "Appearance", label: "Background picture", help: "Any PNG, JPEG, GIF or WebP. Enter opens a file picker. It is cropped to fill the screen and drawn as tinted dots (or soft blocks). Also: ror2 config set backdrop-image <file>",
			info:   func(m *Model) string { return orNone(m.app.Cfg.Get("backdrop_image", "")) },
			action: func(m *Model) tea.Cmd { return chooseFile(m, "backdrop_image", "Choose a background picture", true) }},
		{section: "Appearance", label: "Background text art", help: "A plain text file with ASCII or Unicode art, centered on screen and colored with the theme. Enter opens a file picker. Also: ror2 config set backdrop-text <file>",
			info:   func(m *Model) string { return orNone(m.app.Cfg.Get("backdrop_text", "")) },
			action: func(m *Model) tea.Cmd { return chooseFile(m, "backdrop_text", "Choose a text art file", false) }},
		{section: "Appearance", key: "animations", label: "Animations", values: []string{"off", "slow", "normal", "fast"},
			labels: []string{"Off", "Slow", "Normal", "Fast"}, def: "off",
			help: "Motion all over: the cursor leaves a comet trail and the new row flashes, tabs slide in, dialogs open like an old CRT, names unscramble out of braille noise, messages type themselves out, installs and fixes end in a burst of sparkles, and progress bars glint. The background art moves too, and the logo and cursor gently pulse. Off keeps everything still."},
		{section: "Appearance", key: "decor", label: "Dotted accents", values: []string{"on", "off"},
			labels: []string{"On", "Off"}, def: "on",
			help: "Braille progress bars and meters, download charts, dotted dividers and leaders, a faint starfield in the top bar, and the planet pictures. Off gives a plainer look."},
		{section: "Appearance", key: "icons", label: "Mod icons", values: []string{"large", "medium", "small", "off"},
			labels: []string{"Large", "Medium", "Small", "Off"}, def: "medium", help: "Size of the icon next to a selected mod."},
		{section: "Appearance", key: "images", label: "Images", values: []string{"auto", "sharp", "blocks", "off"},
			labels: []string{imgAuto, "Sharp (Ghostty, kitty)", "Half-blocks", "Off"}, def: "auto",
			help: "How icons and README pictures are drawn. Sharp needs a terminal with the kitty graphics protocol, like Ghostty. Half-blocks work in any terminal with full color."},
		{section: "Appearance", key: "descriptions", label: "Descriptions in lists", values: []string{"on", "off"},
			labels: []string{"Shown", "Hidden"}, def: "on", help: "Show each mod's one-line description in the Installed list."},

		{section: "Browsing", label: "Browse sort & filters", help: "Sort Browse by up to four things in order (for example downloads, then last updated), and show or hide libraries, deprecated mods, mods you have, old mods, and any mix of categories. Enter opens it; in Browse press f (or c to jump to categories).",
			info: func(m *Model) string {
				f, _ := m.filterSummary(tabBrowse)
				return m.sortSummary(tabBrowse) + " · " + f
			},
			action: func(m *Model) tea.Cmd { return m.openView(tabBrowse, false) }},
		{section: "Browsing", label: "Installed sort & filters", help: "Sort Installed (updates first, name, last updated…), hide disabled, hand-installed or up-to-date mods, hide whole sections, or show one flat list. Enter opens it; in Installed press f.",
			info: func(m *Model) string {
				f, _ := m.filterSummary(tabInstalled)
				return m.sortSummary(tabInstalled) + " · " + f
			},
			action: func(m *Model) tea.Cmd { return m.openView(tabInstalled, false) }},
		{section: "Browsing", key: "deprecated", label: "Deprecated mods", values: []string{"hide", "show"},
			labels: []string{"Hidden", "Shown"}, def: "hide", help: "Mods their authors abandoned. Usually best left hidden."},
		{section: "Browsing", key: "browse_libraries", label: "Libraries in Browse", values: []string{"hide", "show"},
			labels: []string{"Hidden", "Shown"}, def: "hide",
			help: "Libraries are support code other mods need (R2API modules, patchers). They install automatically as dependencies, so Browse hides them by default. Press b in Browse to flip it."},
		{section: "Browsing", key: "libraries", label: "Libraries section", values: []string{"collapsed", "expanded"},
			labels: []string{"Collapsed", "Expanded"}, def: "collapsed", help: "Whether Installed starts with Libraries & dependencies folded away."},

		{section: "Behavior", key: "confirm", label: "Confirm installs & updates", values: []string{"ask", "skip"},
			labels: []string{"Ask first", "Just do it"}, def: "ask",
			help: "Show the plan before installing or updating. Removing a mod always asks. A backup is taken either way."},
		{section: "Behavior", key: "mouse", label: "Mouse", values: []string{"on", "off"}, labels: []string{"On", "Off"}, def: "on",
			help: "Clicks and scrolling. Turn off to select text in the terminal (or hold ⌥ while dragging)."},
		{section: "Behavior", key: "refresh", label: "Refresh catalog after", values: []string{"15", "60", "360", "1440"},
			labels: []string{"15 minutes", "1 hour", "6 hours", "1 day"}, def: "60",
			help: "How old the cached Thunderstore catalog may get before it is downloaded again on start."},
		{section: "Behavior", key: "backups", label: "Backups to keep", values: []string{"3", "5", "8", "15"}, def: "8",
			help: "Snapshots of BepInEx taken before every change. They are APFS clones, so they cost little disk space."},

		{section: "Create", key: "agent", label: "AI agent", values: []string{"claude", "codex"},
			labels: []string{"Claude Code", "Codex"}, def: "claude",
			help: "Which coding agent Enter opens in the Create tab. c and x always open Claude Code and Codex."},
		{section: "Create", label: "Projects folder", help: "Where new mod projects are created. Enter opens it. Change it with: ror2 config set workspace <path>",
			info:   func(m *Model) string { return config.ShortPath(devkit.Workspace(m.app.Cfg)) },
			action: func(m *Model) tea.Cmd { return openPath(m, devkit.Workspace(m.app.Cfg)) }},
		{section: "Create", label: "Author name", help: "Used for new projects and Thunderstore uploads. It is remembered from the last mod you created.",
			info: func(m *Model) string { return defaultAuthor(m.app.Cfg) }},

		{section: "Game", label: "Game folder", help: "Where Risk of Rain 2 is installed. Enter opens it in Finder. Change it with: ror2 config set game-dir <path>",
			info:   func(m *Model) string { return config.ShortPath(m.app.Game.Dir) },
			action: func(m *Model) tea.Cmd { return openPath(m, m.app.Game.Dir) }},
		{section: "Game", label: "Wrapper", help: "The Sikarugir app that runs Windows Steam. Enter shows it in Finder.",
			info:   func(m *Model) string { return strings.TrimSuffix(filepath.Base(m.app.Cfg.WrapperApp), ".app") },
			action: func(m *Model) tea.Cmd { return revealPath(m, m.app.Cfg.WrapperApp) }},
		{section: "Game", label: "Backups", help: "Enter opens the backups folder. Restore one with: ror2 restore <name>",
			info: func(m *Model) string {
				list, _ := game.ListBackups(m.app.Game.BackupRoot())
				return fmt.Sprintf("%d saved", len(list))
			},
			action: func(m *Model) tea.Cmd { return openPath(m, m.app.Game.BackupRoot()) }},
		{section: "Game", label: "Download cache", help: "Downloaded mod zips and images. Enter clears it; anything needed is downloaded again.",
			info: func(m *Model) string { return ui.Bytes(dirSize(config.CacheDir())) },
			action: func(m *Model) tea.Cmd {
				os.RemoveAll(filepath.Join(config.CacheDir(), "packages"))
				os.RemoveAll(filepath.Join(config.CacheDir(), "images"))
				return m.showToast(toastOK, "Cleared downloaded zips and images.")
			}},
		{section: "Game", label: "Thunderstore catalog", help: "Enter downloads the latest list of mods now.",
			info: func(m *Model) string {
				if m.app.Cat == nil {
					return "not loaded"
				}
				return fmt.Sprintf("%s mods · %s", ui.Count(len(m.app.Cat.Packages)), ui.Ago(m.app.Cat.Fetched))
			},
			action: func(m *Model) tea.Cmd {
				if m.catLoading {
					return nil
				}
				return m.fetchCatalog()
			}},

		{section: "About", label: "ror2", help: "A Thunderstore mod manager for Risk of Rain 2 on Mac.",
			info: func(m *Model) string { return "v" + ui.Version }},
	}
}

func dirSize(p string) int64 {
	var n int64
	_ = filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n
}

func openPath(m *Model, p string) tea.Cmd {
	_ = os.MkdirAll(p, 0o755)
	if err := exec.Command("open", p).Start(); err != nil {
		return m.showToast(toastErr, err.Error())
	}
	return nil
}

func revealPath(m *Model, p string) tea.Cmd {
	if err := exec.Command("open", "-R", p).Start(); err != nil {
		return m.showToast(toastErr, err.Error())
	}
	return nil
}

func (m *Model) settingValue(s setting) string {
	switch s.key {
	case "refresh":
		return strconv.Itoa(m.app.Cfg.CatalogMaxAgeMinutes)
	case "backups":
		return strconv.Itoa(m.app.Cfg.KeepBackups)
	}
	return m.app.Cfg.Get(s.key, s.def)
}

// changeSetting moves a choice by dir (+1/-1) or runs an action.
func (m *Model) changeSetting(i, dir int) tea.Cmd {
	list := m.settings()
	if i < 0 || i >= len(list) {
		return nil
	}
	s := list[i]
	if !s.isChoice() {
		if s.action != nil && dir >= 0 {
			return s.action(m)
		}
		return nil
	}
	cur := m.settingValue(s)
	idx := 0
	for j, v := range s.values {
		if v == cur {
			idx = j
		}
	}
	idx = (idx + dir + len(s.values)) % len(s.values)
	return m.setSetting(s, s.values[idx])
}

func (m *Model) setSetting(s setting, v string) tea.Cmd {
	cfg := m.app.Cfg
	switch s.key {
	case "refresh":
		cfg.CatalogMaxAgeMinutes, _ = strconv.Atoi(v)
	case "backups":
		cfg.KeepBackups, _ = strconv.Atoi(v)
	default:
		cfg.Set(s.key, v)
	}
	cmd := m.applySettings(s.key)
	defer func() { m.diskStamp = m.app.DiskStamp() }()
	if err := cfg.Save(); err != nil {
		return tea.Batch(cmd, m.showToast(toastErr, "Could not save settings: "+err.Error()))
	}
	return cmd
}

// applySettings puts the saved settings into effect. changed names the setting
// that just changed ("" on startup) so one-off effects only happen then.
func (m *Model) applySettings(changed string) tea.Cmd {
	cfg := m.app.Cfg
	ui.Apply(ui.ThemeByName(cfg.Get("theme", ""), m.dark))
	m.border = map[string]lipgloss.Border{
		"thick": lipgloss.ThickBorder(), "double": lipgloss.DoubleBorder(), "square": lipgloss.NormalBorder(),
		"dotted": ui.DottedBorder(), "braille": ui.BrailleBorder(),
	}[cfg.Get("borders", "rounded")]
	if m.border == (lipgloss.Border{}) {
		m.border = lipgloss.RoundedBorder()
	}
	m.roundMore = cfg.Get("borders", "rounded") == "extra-round"
	ui.Animate = cfg.Get("animations", "off") != "off"
	ui.AnimSpeed = map[string]float64{"slow": 0.5, "normal": 1, "fast": 2}[cfg.Get("animations", "off")]
	if kind := cfg.Get("backdrop", "off"); kind == "off" {
		m.backdrop = nil
	} else {
		if m.backdrop == nil {
			m.backdrop = &ui.Backdrop{}
		}
		m.backdrop.Kind = kind
		m.backdrop.Strength = map[string]float64{"faint": 0.12, "soft": 0.2, "bold": 0.32}[cfg.Get("backdrop_strength", "soft")]
		m.backdrop.Image = cfg.Get("backdrop_image", "")
		m.backdrop.Text = cfg.Get("backdrop_text", "")
	}
	m.fillBg = cfg.Get("background", "theme") == "theme" && ui.TrueColor()
	size := map[string][2]int{"large": {20, 10}, "medium": {16, 8}, "small": {12, 6}, "off": {0, 0}}[cfg.Get("icons", "medium")]
	if size == ([2]int{}) && cfg.Get("icons", "medium") != "off" {
		size = [2]int{16, 8}
	}
	m.iconCols, m.iconRows = size[0], size[1]
	m.showDesc = cfg.Get("descriptions", "on") == "on"
	ui.Decor = cfg.Get("decor", "on") == "on"
	m.confirmSkip = cfg.Get("confirm", "ask") == "skip"
	if m.imgs != nil {
		mode := map[string]termimg.Mode{"sharp": termimg.Kitty, "blocks": termimg.Blocks, "off": termimg.Off}[cfg.Get("images", "auto")]
		if cfg.Get("images", "auto") == "auto" {
			mode = termimg.Detect()
		}
		m.imgs.SetMode(mode)
		m.imgs.SetBackground(ui.Current.Bg, !ui.Current.Light)
	}

	var cmds []tea.Cmd
	if changed == "" || changed == "libraries" {
		m.collapsed["Libraries & dependencies"] = cfg.Get("libraries", "collapsed") == "collapsed"
	}
	if changed == "mouse" {
		if cfg.Get("mouse", "on") == "on" {
			cmds = append(cmds, tea.EnableMouseCellMotion)
		} else {
			cmds = append(cmds, tea.DisableMouse)
		}
	}
	if changed == "theme" && m.mode == mNormal {
		// README text was colored with the old theme.
		m.readmeBody = ""
	}
	if changed != "" {
		m.recompute()
	}
	return tea.Batch(cmds...)
}

// ---------- view ----------

func (m *Model) settingsRows(w, h int) (string, []string) {
	list := m.settings()
	type vline struct {
		text string
		item int
	}
	var lines []vline
	section := ""
	for i, s := range list {
		if s.section != section {
			if section != "" {
				lines = append(lines, vline{"", -1})
			}
			section = s.section
			lines = append(lines, vline{" " + ui.Rule(strings.ToUpper(section), w-2), -1})
		}
		sel := i == m.cur[tabSettings]
		nameSt := stName
		if sel {
			nameSt = stBold
		}
		var right []seg
		switch {
		case s.isChoice():
			v := s.display(m.settingValue(s))
			if sel {
				right = []seg{{"‹ ", stVoid}, {v, lipgloss.NewStyle().Foreground(ui.Equipment).Bold(true)}, {" ›", stVoid}}
			} else {
				right = []seg{{v, stDim}}
			}
		case s.info != nil:
			right = []seg{{keepEnd(s.info(m), max(8, w-lipgloss.Width(s.label)-8)), stDim}}
			if s.action != nil && sel {
				right = append(right, seg{"  ↵", stVoid})
			}
		}
		lines = append(lines, vline{m.mark(fmt.Sprintf("set:%d", i), rowLead(w, sel, []seg{{" " + s.label, nameSt}}, right, "·")), i})
	}
	// Scroll so the selected item is visible.
	selLine := 0
	for j, l := range lines {
		if l.item == m.cur[tabSettings] {
			selLine = j
		}
	}
	start := 0
	if selLine >= h {
		start = selLine - h + 1
	}
	var out []string
	for j := start; j < len(lines) && j < start+h; j++ {
		out = append(out, lines[j].text)
	}
	return "Settings · saved automatically", out
}

func (m *Model) settingsDetail(w int) string {
	list := m.settings()
	c := m.cur[tabSettings]
	if c >= len(list) {
		return ""
	}
	s := list[c]
	var out []string
	if s.section == "About" {
		return m.aboutView(w)
	}
	out = append(out, ui.Fainter.Render(strings.ToUpper(s.section)), ui.Accent.Render(m.scr(s.label)), "")
	out = append(out, ui.Dim.Render(wrap(s.help, w)), "")
	if s.isChoice() {
		cur := m.settingValue(s)
		for j, v := range s.values {
			mark, st := "  ○ ", stName
			if v == cur {
				mark, st = "  ● ", lipgloss.NewStyle().Foreground(ui.Equipment).Bold(true)
			}
			line := lipgloss.NewStyle().Foreground(ui.Void).Render(mark) + st.Render(s.display(v))
			if s.key == "theme" {
				line += "  " + swatches(ui.ThemeByName(v, true))
			}
			out = append(out, m.mark(fmt.Sprintf("opt:%d", j), ansi.Truncate(line, w, "…")))
		}
		out = append(out, "", m.hintsW(w, "←", "previous", "→", "next"))
		if s.key == "backdrop" || s.key == "backdrop_strength" {
			if bd := m.backdrop; bd != nil {
				if err := bd.Err(); err != nil {
					out = append(out, "", ui.Warn.Render(wrap(ui.GWarn+" "+err.Error(), w)))
				}
			}
		}
		if s.key == "theme" || s.key == "borders" || s.key == "background" {
			out = append(out, "", m.themePreview(w))
		}
	} else if s.info != nil {
		out = append(out, ui.Bold.Render(s.info(m)))
		if s.action != nil {
			out = append(out, "", m.hintsW(w, "enter", "open"))
		}
	}
	return strings.Join(out, "\n")
}

// swatches shows a theme's main colors as little blocks.
func swatches(t ui.Theme) string {
	var b strings.Builder
	for _, c := range []string{t.Accent, t.Sel, t.OK, t.Warn, t.Bad, t.Info} {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("■"))
	}
	return b.String()
}

// themePreview is a small mock of the app in the current theme.
func (m *Model) themePreview(w int) string {
	pw := min(w, 46)
	rows := []string{
		row(pw-4, true, []seg{{ui.GOK + " ", stOK}, {"LookingGlass", stBold}, {"  DropPod", stFaint}}, []seg{{"↓ 2.8M", stDim}}),
		row(pw-4, false, []seg{{ui.GUpdate + " ", stWarn}, {"Starstorm2", stName}, {"  TeamMoonstorm", stFaint}}, []seg{{"↓ 4M", stDim}}),
		row(pw-4, false, []seg{{ui.GLocal + " ", stInfo}, {"MetalSafeBars", stName}}, []seg{{"1.0.0", stDim}}),
		"",
		" " + ui.Bar(0.62, pw-12) + ui.Dim.Render(" 62%"),
		"",
		" " + ui.Pill("PROBLEM", ui.Legendary) + " " + ui.Pill("WARNING", ui.Boss) + " " + ui.Pill("NOTE", ui.Lunar),
	}
	return m.frame(lipgloss.NewStyle().Border(m.border).BorderForeground(ui.BorderFocus).Width(pw-2).Padding(0, 1).
		Render(ui.Fainter.Render("preview")+"\n"+strings.Join(rows, "\n")), ui.BorderFocus)
}

func (m *Model) aboutView(w int) string {
	out := []string{""}
	if ui.Decor && w >= 36 {
		out = append(out, ui.Planet(min(w, 34), 8), "")
	}
	out = append(out, ui.BigLogo(), "",
		ui.Gradient("Risk of Rain 2 mod manager", ui.Void, ui.Equipment, true)+ui.Dim.Render("  v"+ui.Version), "",
		ui.Dim.Render(wrap("Browse and install Thunderstore mods, keep them updated, fix missing dependencies, and build your own, on a Mac running the game through Sikarugir.", w)), "",
		ui.Fainter.Render("Theme    ")+ui.Bold.Render(ui.Current.Name),
		ui.Fainter.Render("Images   ")+ui.Bold.Render(map[termimg.Mode]string{termimg.Kitty: "sharp (kitty protocol)", termimg.Blocks: "half-blocks", termimg.Off: "off"}[m.imgMode()]),
		ui.Fainter.Render("Config   ")+ui.Dim.Render(config.ShortPath(filepath.Join(config.Dir(), "config.json"))),
		ui.Fainter.Render("Cache    ")+ui.Dim.Render(config.ShortPath(config.CacheDir())),
	)
	return strings.Join(out, "\n")
}

func (m *Model) imgMode() termimg.Mode {
	if m.imgs == nil {
		return termimg.Off
	}
	return m.imgs.Mode
}

// keepEnd shortens s to n cells by cutting from the start, so the useful end
// of a path stays visible.
func keepEnd(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return config.ShortPath(s)
}

type fileChosenMsg struct {
	key, path string
	image     bool
	err       error
}

// chooseFile opens the macOS file picker and saves the chosen path.
func chooseFile(m *Model, key, prompt string, image bool) tea.Cmd {
	return func() tea.Msg {
		script := fmt.Sprintf(`POSIX path of (choose file with prompt %q)`, prompt)
		if image {
			script = fmt.Sprintf(`POSIX path of (choose file with prompt %q of type {"public.image"})`, prompt)
		}
		out, err := exec.Command("osascript", "-e", script).Output()
		if err != nil {
			return fileChosenMsg{key: key, err: err} // cancelled, usually
		}
		return fileChosenMsg{key: key, path: strings.TrimSpace(string(out)), image: image}
	}
}

// onFileChosen stores a picked background file and switches the background to use it.
func (m *Model) onFileChosen(msg fileChosenMsg) tea.Cmd {
	if msg.err != nil || msg.path == "" {
		return nil
	}
	cfg := m.app.Cfg
	cfg.Set(msg.key, msg.path)
	kind := cfg.Get("backdrop", "off")
	if msg.image && kind != "image" && kind != "image-blocks" {
		cfg.Set("backdrop", "image")
	} else if !msg.image {
		cfg.Set("backdrop", "text")
	}
	cmd := m.applySettings("backdrop")
	m.saveView()
	return tea.Batch(cmd, m.showToast(toastOK, "Background set to "+filepath.Base(msg.path)))
}
