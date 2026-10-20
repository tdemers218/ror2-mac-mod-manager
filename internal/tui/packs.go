package tui

// The Packs tab: saved mod arrangements, plus the built-in Vanilla pack.

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ror2mm/internal/core"
	"ror2mm/internal/ui"
)

// packRows is the list: every pack, then a "save as new" row.
func (m *Model) packList() []*core.Pack { return m.app.Packs() }

func (m *Model) packsLen() int { return len(m.packList()) + 1 }

// selPack is the pack under the cursor, or nil on the "new pack" row.
func (m *Model) selPack() *core.Pack {
	list := m.packList()
	if c := m.cur[tabPacks]; c < len(list) {
		return list[c]
	}
	return nil
}

func (m *Model) packsRows(w, h int) (string, []string) {
	list := m.packList()
	active, matches := m.app.ActivePack()
	var rows []string
	off := m.off[tabPacks]
	for i := off; i < len(list)+1 && i < off+h; i++ {
		sel := i == m.cur[tabPacks]
		nameSt := stName
		if sel {
			nameSt = stBold
		}
		if i == len(list) {
			rows = append(rows, m.mark(fmt.Sprintf("row:%d", i), row(w, sel, []seg{{" +  ", stVoid}, {"Save the mods that are on now as a new pack", stFaint}}, nil)))
			continue
		}
		p := list[i]
		mark := seg{" ○  ", stFaint}
		if active != nil && active.Name == p.Name {
			mark = seg{" ●  ", stOK}
			if !matches {
				mark = seg{" ●  ", stWarn}
			}
		}
		left := []seg{mark, {p.Name, nameSt}}
		var right []seg
		switch {
		case p.Vanilla:
			left = append(left, seg{"  built in", stFaint})
			right = []seg{{"no mods", stDim}}
		default:
			if p.Settings {
				left = append(left, seg{"  + settings", stFaint})
			}
			right = []seg{{fmt.Sprintf("%d mods", len(p.Mods)), stDim}}
		}
		rows = append(rows, m.mark(fmt.Sprintf("row:%d", i), rowLead(w, sel, left, right, "·")))
	}
	title := fmt.Sprintf("%d mod packs", len(list))
	if active != nil {
		title += " · using " + active.Name
		if !matches {
			title += " (changed)"
		}
	}
	return title, rows
}

func (m *Model) packsDetail(w int) string {
	p := m.selPack()
	if p == nil {
		return strings.Join([]string{
			ui.Accent.Render(m.scr("New mod pack")), "",
			ui.Dim.Render(wrap("Saves which mods are switched on right now, including hand-installed mods and your own dev builds, under a name. Load it later to get exactly this setup back: mods are switched on and off to match, and any that were removed since are downloaded again.", w)), "",
			ui.Dim.Render(wrap("You can also keep a copy of every mod's settings with the pack; they are put back when you load it.", w)), "",
			m.hintsW(w, "enter", "save as new pack"),
		}, "\n")
	}
	active, matches := m.app.ActivePack()
	isActive := active != nil && active.Name == p.Name
	out := []string{ui.Accent.Render(m.scr(p.Name))}
	switch {
	case isActive && matches:
		out = append(out, ui.OK.Render(ui.GOn+" In use"))
	case isActive:
		out = append(out, ui.Warn.Render(ui.GOn+" In use, but mods changed since it was loaded"))
	default:
		out = append(out, ui.Dim.Render(ui.GOff+" Not in use"))
	}
	out = append(out, "")
	if p.Vanilla {
		out = append(out, ui.Dim.Render(wrap("The plain game with no mods. Loading it switches the mod loader (BepInEx) off for the next launch. Your mods are not moved or removed, so loading any other pack brings them back instantly.", w)), "")
		hints := []string{"enter", "load"}
		if isActive {
			hints = []string{}
		}
		out = append(out, m.hintsW(w, append(hints, "n", "new pack")...))
		return strings.Join(out, "\n")
	}
	out = append(out, ui.Dim.Render(fmt.Sprintf("Saved %s", ui.Ago(p.Updated))))
	if p.Settings {
		out = append(out, ui.Dim.Render("Keeps its own copy of mod settings"))
	}
	// What loading would do.
	if pp, err := m.app.PlanPack(p); err == nil && !pp.Empty() && !(isActive && matches) {
		var parts []string
		if n := len(pp.Enable); n > 0 {
			parts = append(parts, ui.OK.Render(fmt.Sprintf("%s %d on", ui.GOn, n)))
		}
		if n := len(pp.Disable); n > 0 {
			parts = append(parts, ui.Dim.Render(fmt.Sprintf("%s %d off", ui.GOff, n)))
		}
		if pp.Install != nil && !pp.Install.Empty() {
			parts = append(parts, ui.Info.Render(fmt.Sprintf("%s %d to download", ui.GDownload, len(pp.Install.Steps))))
		}
		if len(pp.Missing) > 0 {
			parts = append(parts, ui.Bad.Render(fmt.Sprintf("%s %d gone", ui.GFail, len(pp.Missing))))
		}
		if len(parts) > 0 {
			out = append(out, "", ui.Bold.Render("Loading it: ")+strings.Join(parts, ui.Dim.Render("  ·  ")))
		}
	}
	out = append(out, "", ui.Rule(fmt.Sprintf("Mods · %d", len(p.Mods)), w))
	have := map[string]*core.Entry{}
	for _, e := range m.app.Inv.Entries {
		if e.Managed {
			have["ts:"+strings.ToLower(e.FullName)] = e
		} else if e.Local != nil {
			have["local:"+strings.ToLower(e.Local.Name)] = e
		}
	}
	for _, pm := range p.Mods {
		e := have[pm.Key()]
		mark := ui.OK.Render(ui.GOK)
		note := ""
		switch {
		case e == nil && pm.FullName != "":
			mark, note = ui.Info.Render(ui.GDownload), ui.Fainter.Render("  will download")
		case e == nil:
			mark, note = ui.Bad.Render(ui.GFail), ui.Fainter.Render("  gone (was installed by hand)")
		case !e.Enabled:
			mark = ui.Dim.Render(ui.GOff)
		}
		out = append(out, "  "+mark+" "+pm.Name+ui.Fainter.Render(" "+pm.Version)+note)
	}
	hints := []string{"enter", "load", "s", "save current mods into it", "r", "rename", "x", "delete"}
	if isActive && matches {
		hints = hints[2:]
	}
	out = append(out, "", m.hintsW(w, hints...))
	return strings.Join(out, "\n")
}

// packsKey handles keys on the Packs tab.
func (m *Model) packsKey(key string) (tea.Cmd, bool) {
	p := m.selPack()
	switch key {
	case "enter", " ":
		if p == nil {
			return m.askPackName("New mod pack", "", true, func(m *Model, name string, settings bool) tea.Cmd {
				return m.savePack(name, settings, false)
			}), true
		}
		return m.loadPack(p), true
	case "n":
		return m.askPackName("New mod pack", "", true, func(m *Model, name string, settings bool) tea.Cmd {
			return m.savePack(name, settings, false)
		}), true
	case "s":
		if p == nil || p.Vanilla {
			return nil, true
		}
		name, settings := p.Name, p.Settings
		m.confirm = &confirmBox{title: "Update " + name + "?", yesLabel: "replace",
			body: []string{"The pack will list the mods that are on right now.", ui.Dim.Render(fmt.Sprintf("It has %d mods now.", len(p.Mods)))},
			yes:  func(m *Model) tea.Cmd { return m.savePack(name, settings, true) }}
		m.mode = mConfirm
		return nil, true
	case "r":
		if p == nil || p.Vanilla {
			return nil, true
		}
		old := p.Name
		return m.askPackName("Rename "+old, old, false, func(m *Model, name string, _ bool) tea.Cmd {
			if err := m.app.RenamePack(old, name); err != nil {
				return m.showToast(toastErr, err.Error())
			}
			m.diskStamp = m.app.DiskStamp()
			return m.showToast(toastOK, "Renamed to "+name)
		}), true
	case "x", "delete", "backspace":
		if p == nil {
			return nil, true
		}
		if p.Vanilla {
			return m.showToast(toastInfo, "Vanilla is built in and can't be deleted."), true
		}
		name := p.Name
		m.confirm = &confirmBox{title: "Delete the pack " + name + "?", yesLabel: "delete",
			body: []string{"Only the saved list is deleted. Your mods stay installed as they are."},
			yes: func(m *Model) tea.Cmd {
				if err := m.app.DeletePack(name); err != nil {
					return m.showToast(toastErr, err.Error())
				}
				m.diskStamp = m.app.DiskStamp()
				m.clampAll()
				return m.showToast(toastOK, "Deleted "+name)
			}}
		m.mode = mConfirm
		return nil, true
	}
	return nil, false
}

func (m *Model) savePack(name string, settings, replacing bool) tea.Cmd {
	if !replacing {
		if old := m.app.FindPack(name); old != nil {
			m.confirm = &confirmBox{title: "Replace " + old.Name + "?", yesLabel: "replace",
				body: []string{"A pack with that name exists. Replace it with the mods that are on now?"},
				yes:  func(m *Model) tea.Cmd { return m.savePack(name, settings, true) }}
			m.mode = mConfirm
			return nil
		}
	}
	p, err := m.app.SavePack(name, settings)
	if err != nil {
		return m.showToast(toastErr, err.Error())
	}
	// Saving the current setup makes it the pack in use.
	m.app.Cfg.Set("active_pack", p.Name)
	_ = m.app.Cfg.Save()
	m.diskStamp = m.app.DiskStamp()
	for i, q := range m.packList() {
		if q.Name == p.Name {
			m.cur[tabPacks] = i
		}
	}
	extra := ""
	if p.Settings {
		extra = " and their settings"
	}
	return m.showToast(toastOK, fmt.Sprintf("Saved %s: %d mods%s", p.Name, len(p.Mods), extra))
}

func (m *Model) loadPack(p *core.Pack) tea.Cmd {
	if m.gameRunning {
		return m.showToast(toastWarn, "Close the game first; mods load when it starts.")
	}
	pp, err := m.app.PlanPack(p)
	if err != nil {
		return m.showToast(toastErr, err.Error())
	}
	if pp.Empty() {
		m.app.Cfg.Set("active_pack", p.Name)
		_ = m.app.Cfg.Save()
		return m.showToast(toastOK, "Already set up as "+p.Name)
	}
	run := func(m *Model) tea.Cmd {
		return m.runOp("Loading "+p.Name, func(progress func(core.Event)) (string, error) {
			if err := m.app.ApplyPack(context.Background(), pp, progress); err != nil {
				return "", err
			}
			if p.Vanilla {
				return "Vanilla is on: the next launch has no mods. Press L to play.", nil
			}
			return "Loaded " + p.Name + ". Press L to play.", nil
		})
	}
	if p.Vanilla || (m.confirmSkip && (pp.Install == nil || pp.Install.Empty())) {
		return run(m)
	}
	var body []string
	line := func(title string, es []*core.Entry, glyph string) {
		if len(es) == 0 {
			return
		}
		var names []string
		for _, e := range es {
			names = append(names, e.Name)
		}
		body = append(body, ui.Bold.Render(fmt.Sprintf("%s %s (%d)", glyph, title, len(es))), ui.Dim.Render(wrap(strings.Join(names, ", "), m.modalWidth()-8)), "")
	}
	line("Switch on", pp.Enable, ui.OK.Render(ui.GOn))
	line("Switch off", pp.Disable, ui.Dim.Render(ui.GOff))
	if pp.Install != nil && !pp.Install.Empty() {
		var names []string
		for _, s := range pp.Install.Steps {
			names = append(names, s.Pkg.Name+" "+s.Version)
		}
		body = append(body, ui.Bold.Render(fmt.Sprintf("%s Download again (%d)", ui.Info.Render(ui.GDownload), len(names))), ui.Dim.Render(wrap(strings.Join(names, ", "), m.modalWidth()-8)), "")
	}
	for _, g := range pp.Missing {
		body = append(body, ui.Warn.Render(ui.GWarn+" "+g+" was installed by hand and is gone"))
	}
	if pp.Settings {
		body = append(body, ui.Dim.Render("Its saved mod settings will be put back."))
	}
	if pp.LoaderChange {
		body = append(body, ui.Dim.Render("Mods will load again (the loader was off)."))
	}
	body = append(body, ui.Fainter.Render("A backup of BepInEx is taken first."))
	m.confirm = &confirmBox{title: "Load " + p.Name + "?", yesLabel: "load", body: body, yes: run}
	m.mode = mConfirm
	return nil
}

// ---------- name prompt ----------

type promptBox struct {
	title        string
	input        textinput.Model
	withSettings bool
	askSettings  bool
	err          string
	done         func(m *Model, name string, settings bool) tea.Cmd
}

func (m *Model) askPackName(title, value string, askSettings bool, done func(m *Model, name string, settings bool) tea.Cmd) tea.Cmd {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "e.g. Chill run, Starstorm, Testing"
	ti.CharLimit = 40
	ti.Width = 40
	ti.SetValue(value)
	ti.TextStyle, ti.PlaceholderStyle, ti.Cursor.Style = ui.Bold, ui.Fainter, ui.Sel
	m.prompt = &promptBox{title: title, input: ti, askSettings: askSettings, done: done}
	m.mode = mPrompt
	return m.prompt.input.Focus()
}

func (m *Model) promptKey(msg tea.KeyMsg) tea.Cmd {
	pb := m.prompt
	switch msg.String() {
	case "esc":
		m.prompt, m.mode = nil, mNormal
		return nil
	case "tab", "shift+tab", "ctrl+s":
		if pb.askSettings {
			pb.withSettings = !pb.withSettings
		}
		return nil
	case "enter":
		name := strings.TrimSpace(pb.input.Value())
		if msg := core.ValidPackName(name); msg != "" {
			pb.err = msg
			return nil
		}
		m.prompt, m.mode = nil, mNormal
		return pb.done(m, name, pb.withSettings)
	}
	var cmd tea.Cmd
	pb.input, cmd = pb.input.Update(msg)
	pb.err = ""
	return cmd
}

func (m *Model) promptView() string {
	pb := m.prompt
	w := min(m.modalWidth(), 60)
	field := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.BorderFocus).Padding(0, 1).Width(w - 8).Render(pb.input.View())
	lines := []string{ui.Dim.Render("Name"), field}
	if pb.err != "" {
		lines = append(lines, ui.Warn.Render(ui.GWarn+" "+pb.err))
	}
	if pb.askSettings {
		box := "○"
		if pb.withSettings {
			box = ui.OK.Render("◉")
		}
		lines = append(lines, "", m.mark("key:tab#settings", box+" Also keep a copy of every mod's settings"), ui.Fainter.Render("  put back when the pack is loaded"))
	}
	hints := []string{"enter", "save", "esc", "cancel"}
	if pb.askSettings {
		hints = []string{"enter", "save", "tab", "settings on/off", "esc", "cancel"}
	}
	lines = append(lines, "", m.hints(hints...))
	return m.modal(pb.title, strings.Join(lines, "\n"), ui.Void, w)
}
