package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ror2mm/internal/config"
	"ror2mm/internal/core"
	"ror2mm/internal/devkit"
	"ror2mm/internal/ui"
)

// wizard is the "New mod" form.
type wizard struct {
	inputs   [3]textinput.Model // name, author, description
	template int
	agent    int // index into devkit.Agents, or len(Agents) for "don't open"
	focus    int // 0-2 inputs, 3 template, 4 agent, 5 create button
	err      string
}

const wizardFields = 6

type agentDoneMsg struct {
	name  string
	err   error
	quick bool // exited within seconds, probably at the trust question
	retry func(m *Model) tea.Cmd
}

// toolStatus caches which tools are installed (checked once).
type toolStatus struct {
	agents map[string]string // agent id -> path ("" if missing)
	dotnet string            // version, "" if missing
}

func (m *Model) tools() *toolStatus {
	if m.toolCache != nil {
		return m.toolCache
	}
	ts := &toolStatus{agents: map[string]string{}}
	for _, a := range devkit.Agents {
		ts.agents[a.ID] = devkit.AgentPath(a)
	}
	if dn, err := devkit.Dotnet(); err == nil {
		if out, err := exec.Command(dn, "--version").Output(); err == nil {
			ts.dotnet = strings.TrimSpace(string(out))
		}
	}
	m.toolCache = ts
	return ts
}

func (m *Model) reloadProjects() {
	m.projects = devkit.List(devkit.Workspace(m.app.Cfg))
}

func (m *Model) createLen() int { return 1 + len(m.projects) }

func (m *Model) selProject() *devkit.Project {
	c := m.cur[tabCreate]
	if m.tab != tabCreate || c == 0 || c-1 >= len(m.projects) {
		return nil
	}
	return m.projects[c-1]
}

func (m *Model) preferredAgent() devkit.Agent {
	if m.app.Cfg.Get("agent", "claude") == "codex" {
		return devkit.Agents[1]
	}
	return devkit.Agents[0]
}

// ---------- keys ----------

func (m *Model) createKey(key string) (tea.Cmd, bool) {
	p := m.selProject()
	switch key {
	case "n":
		return m.openWizard(), true
	case "enter":
		if p == nil {
			return m.openWizard(), true
		}
		return m.openAgent(p, m.preferredAgent(), ""), true
	case "C", "X":
		if p != nil {
			a := devkit.Agents[0]
			if key == "X" {
				a = devkit.Agents[1]
			}
			return m.openAgentFresh(p, a), true
		}
	}
	if p == nil {
		return nil, false
	}
	switch key {
	case "c":
		return m.openAgent(p, devkit.Agents[0], ""), true
	case "x":
		return m.openAgent(p, devkit.Agents[1], ""), true
	case "b":
		return m.devBuild(p, false), true
	case "d":
		return m.devBuild(p, true), true
	case "u":
		if err := p.Undeploy(m.app.Game.Dir); err != nil {
			return m.showToast(toastErr, err.Error()), true
		}
		m.app.Refresh()
		m.recompute()
		return m.showToast(toastOK, "Removed the dev copy of "+p.Name+" from the game."), true
	case "p":
		return m.devPackage(p), true
	case "f":
		return openPath(m, p.Dir), true
	}
	return nil, false
}

func (m *Model) createClick(id string) tea.Cmd {
	var n int
	switch {
	case strings.HasPrefix(id, "cr:field:"):
		fmt.Sscanf(id, "cr:field:%d", &n)
		if m.wiz != nil {
			m.wizFocus(n)
		}
	case strings.HasPrefix(id, "cr:tpl:"):
		fmt.Sscanf(id, "cr:tpl:%d", &n)
		if m.wiz != nil {
			m.wiz.template = n
			m.wizFocus(3)
		}
	case strings.HasPrefix(id, "cr:agent:"):
		fmt.Sscanf(id, "cr:agent:%d", &n)
		if m.wiz != nil {
			m.wiz.agent = n
			m.wizFocus(4)
		}
	case id == "cr:create":
		return m.wizSubmit()
	case id == "cr:cancel":
		m.wiz = nil
		m.mode = mNormal
	}
	return nil
}

// ---------- wizard ----------

func (m *Model) openWizard() tea.Cmd {
	if m.tools().dotnet == "" {
		return m.showToast(toastErr, "The .NET SDK is needed to build mods: brew install --cask dotnet-sdk")
	}
	w := &wizard{}
	place := []string{"MyFirstMod", "Your name", "e.g. Gives players a double jump that recharges on kills"}
	for i := range w.inputs {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = place[i]
		ti.PlaceholderStyle = ui.Fainter
		ti.TextStyle = ui.Bold
		ti.Cursor.Style = ui.Sel
		ti.CharLimit = 64
		ti.Width = 40
		w.inputs[i] = ti
	}
	w.inputs[2].CharLimit = 400
	w.inputs[2].Width = 60
	w.inputs[1].SetValue(defaultAuthor(m.app.Cfg))
	if m.app.Cfg.Get("agent", "claude") == "codex" {
		w.agent = 1
	}
	m.wiz = w
	m.mode = mNewMod
	return m.wizFocus(0)
}

func defaultAuthor(cfg *config.Config) string {
	if a := cfg.Get("author", ""); a != "" {
		return a
	}
	if out, err := exec.Command("git", "config", "--global", "user.name").Output(); err == nil {
		if n := strings.Map(nameRune, strings.TrimSpace(string(out))); len(n) > 1 {
			return n
		}
	}
	return strings.Map(nameRune, os.Getenv("USER"))
}

func nameRune(r rune) rune {
	if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return r
	}
	return -1
}

func (m *Model) wizFocus(i int) tea.Cmd {
	w := m.wiz
	w.focus = (i + wizardFields) % wizardFields
	var cmd tea.Cmd
	for j := range w.inputs {
		if j == w.focus {
			cmd = w.inputs[j].Focus()
		} else {
			w.inputs[j].Blur()
		}
	}
	return cmd
}

func (m *Model) wizardKey(msg tea.KeyMsg) tea.Cmd {
	w := m.wiz
	key := msg.String()
	switch key {
	case "esc":
		m.wiz = nil
		m.mode = mNormal
		return nil
	case "tab", "down":
		return m.wizFocus(w.focus + 1)
	case "shift+tab", "up":
		return m.wizFocus(w.focus - 1)
	case "ctrl+s":
		return m.wizSubmit()
	case "enter":
		if w.focus == wizardFields-1 || w.focus == 2 {
			return m.wizSubmit()
		}
		return m.wizFocus(w.focus + 1)
	}
	switch w.focus {
	case 3, 4:
		n := len(devkit.Templates)
		ptr := &w.template
		if w.focus == 4 {
			n, ptr = len(devkit.Agents)+1, &w.agent
		}
		switch key {
		case "left", "h":
			*ptr = (*ptr + n - 1) % n
		case "right", "l", " ":
			*ptr = (*ptr + 1) % n
		}
		return nil
	case 5:
		if key == " " {
			return m.wizSubmit()
		}
		return nil
	}
	var cmd tea.Cmd
	w.inputs[w.focus], cmd = w.inputs[w.focus].Update(msg)
	w.err = ""
	return cmd
}

func (m *Model) wizSubmit() tea.Cmd {
	w := m.wiz
	name := strings.TrimSpace(w.inputs[0].Value())
	author := strings.TrimSpace(w.inputs[1].Value())
	desc := strings.TrimSpace(w.inputs[2].Value())
	if err := devkit.ValidName("Mod name", name); err != nil {
		w.err = err.Error()
		return m.wizFocus(0)
	}
	if err := devkit.ValidName("Author", author); err != nil {
		w.err = err.Error()
		return m.wizFocus(1)
	}
	if desc == "" {
		w.err = "Describe what the mod should do; the AI starts from this."
		return m.wizFocus(2)
	}
	if _, err := os.Stat(filepath.Join(devkit.Workspace(m.app.Cfg), name)); err == nil {
		w.err = "A project with that name already exists."
		return m.wizFocus(0)
	}
	t := devkit.Templates[w.template]
	var agent *devkit.Agent
	if w.agent < len(devkit.Agents) {
		a := devkit.Agents[w.agent]
		agent = &a
		m.app.Cfg.Set("agent", a.ID)
	}
	m.app.Cfg.Set("author", author)
	_ = m.app.Cfg.Save()
	m.wiz = nil
	m.mode = mNormal

	a := m.app
	var plan *core.Plan
	if a.Cat != nil {
		plan, _ = a.TemplatePlan(t)
	}
	deps := a.TemplateDeps(t)
	opts := devkit.NewOptions{Name: name, Author: author, Description: desc, Template: t.ID, GameDir: a.Game.Dir, Deps: deps}
	var created *devkit.Project
	cmd := m.runOp("Creating "+name, func(progress func(core.Event)) (string, error) {
		if plan != nil && !plan.Empty() {
			if err := a.Apply(context.Background(), plan, "before-dev-template", progress); err != nil {
				return "", err
			}
		}
		p, err := devkit.New(devkit.Workspace(a.Cfg), opts)
		if err != nil {
			return "", err
		}
		created = p
		var out strings.Builder
		if err := p.Build(context.Background(), a.Game.Dir, &out); err != nil {
			return "Created " + name + ", but the first build failed (press b to see why)", nil
		}
		return "Created " + name + " · it builds", nil
	})
	m.afterOp = func(m *Model) tea.Cmd {
		m.reloadProjects()
		m.tab = tabCreate
		for i, p := range m.projects {
			if created != nil && p.Dir == created.Dir {
				m.cur[tabCreate] = i + 1
			}
		}
		if created != nil && agent != nil {
			return m.openAgent(created, *agent, devkit.KickoffPrompt(created))
		}
		return nil
	}
	return cmd
}

// ---------- actions ----------

// openAgent hands the terminal to Claude Code or Codex inside the project,
// continuing the last conversation there when there is one.
func (m *Model) openAgent(p *devkit.Project, a devkit.Agent, prompt string) tea.Cmd {
	return m.runAgent(p, a, prompt, true)
}

// openAgentFresh starts a new conversation.
func (m *Model) openAgentFresh(p *devkit.Project, a devkit.Agent) tea.Cmd {
	return m.runAgent(p, a, "", false)
}

func (m *Model) runAgent(p *devkit.Project, a devkit.Agent, prompt string, resume bool) tea.Cmd {
	bin := m.tools().agents[a.ID]
	if bin == "" {
		return m.showToast(toastErr, a.Name+" is not installed.")
	}
	// A brand-new project's kickoff starts a new conversation.
	if prompt != "" && !devkit.HasSession(a, p) {
		resume = false
	}
	launch := func(m *Model) tea.Cmd {
		cmd := devkit.AgentCommand(a, bin, p, m.app.Game.Dir, prompt, resume)
		// Give the agent the real terminal. Bubble Tea would otherwise hand it
		// our locked output writer, which is not a TTY, and Claude Code then
		// runs non-interactively (no approval prompts, exits when done).
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		p.MarkSession(a)
		name, started := a.Name, time.Now()
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			return agentDoneMsg{name: name, err: err, quick: time.Since(started) < 20*time.Second,
				retry: func(m *Model) tea.Cmd { return m.runAgent(p, a, prompt, resume) }}
		})
	}
	// First time in this folder: explain Claude Code's trust question, whose
	// default answer quits.
	if a.ID == "claude" && !devkit.ClaudeTrusted(p.Dir) {
		m.confirm = &confirmBox{title: "Opening Claude Code",
			body:     []string{ui.Dim.Render(wrap(devkit.TrustHint, m.modalWidth()-8)), "", ui.Fainter.Render("You only have to do this once per project.")},
			yesLabel: "open Claude Code", yes: launch}
		m.mode = mConfirm
		return nil
	}
	return launch(m)
}

func (m *Model) devBuild(p *devkit.Project, deploy bool) tea.Cmd {
	a := m.app
	title := "Building " + p.Name
	if deploy {
		title = "Building and deploying " + p.Name
	}
	var output strings.Builder
	cmd := m.runOp(title, func(func(core.Event)) (string, error) {
		if err := p.Build(context.Background(), a.Game.Dir, &output); err != nil {
			return "", err
		}
		if !deploy {
			return p.Name + " builds", nil
		}
		dest, err := p.Deploy(a.Game.Dir)
		if err != nil {
			return "", err
		}
		msg := "Deployed to " + config.ShortPath(dest) + " · launch the game (L) to try it"
		if core.GameRunning() {
			msg = "Deployed · restart the game to load the new build"
		}
		return msg, nil
	})
	m.afterOp = func(m *Model) tea.Cmd {
		m.reloadProjects()
		out := strings.TrimSpace(output.String())
		if m.lastErr != nil && out != "" {
			lines := strings.Split(out, "\n")
			if len(lines) > 30 {
				lines = lines[len(lines)-30:]
			}
			var errs []string
			for i, l := range lines {
				// Paths make lines long; keep the useful part.
				l = strings.Replace(l, p.Dir+"/", "", 1)
				if j := strings.LastIndex(l, " ["); j > 0 {
					l = l[:j]
				}
				if strings.Contains(l, "error") {
					errs = append(errs, l)
					lines[i] = ui.Bad.Render(l)
				} else {
					lines[i] = ui.Dim.Render(l)
				}
			}
			agent := m.preferredAgent()
			prompt := "The mod does not build. `ror2 dev build` reports:\n\n" + strings.Join(errs, "\n") +
				"\n\nFix the errors (check game APIs with ror2 dev decompile if a member is missing), then run `ror2 dev build` to confirm."
			m.confirm = &confirmBox{title: "Build failed: " + p.Name, body: lines,
				yesLabel: "ask " + agent.Name + " to fix it", noLabel: "close",
				yes: func(m *Model) tea.Cmd { return m.openAgent(p, agent, prompt) }}
			m.mode = mConfirm
		}
		return nil
	}
	return cmd
}

func (m *Model) devPackage(p *devkit.Project) tea.Cmd {
	a := m.app
	var zip string
	cmd := m.runOp("Packaging "+p.Name, func(func(core.Event)) (string, error) {
		var out strings.Builder
		if err := p.Build(context.Background(), a.Game.Dir, &out); err != nil {
			return "", err
		}
		z, err := p.Package()
		zip = z
		return "Package ready: " + config.ShortPath(z), err
	})
	m.afterOp = func(m *Model) tea.Cmd {
		if zip != "" {
			_ = exec.Command("open", "-R", zip).Start()
		}
		return nil
	}
	return cmd
}

// ---------- view ----------

func (m *Model) createSubheader() string {
	t := m.tools()
	mark := func(ok bool, name string) string {
		if ok {
			return ui.OK.Render(ui.GOK) + " " + ui.Dim.Render(name)
		}
		return ui.Bad.Render(ui.GFail) + " " + ui.Dim.Render(name)
	}
	parts := []string{ui.Dim.Render("Projects in " + keepEnd(config.ShortPath(devkit.Workspace(m.app.Cfg)), 40))}
	for _, a := range devkit.Agents {
		parts = append(parts, mark(t.agents[a.ID] != "", a.Name))
	}
	parts = append(parts, mark(t.dotnet != "", ".NET "+t.dotnet))
	return strings.Join(parts, ui.Fainter.Render("  ·  "))
}

func (m *Model) createFooter() string {
	if m.selProject() == nil {
		return m.hints("enter", "new mod", "?", "help", "q", "quit")
	}
	return m.hints("enter", "open in "+m.preferredAgent().Name, "c", "Claude", "x", "Codex", "b", "build", "d", "deploy", "p", "package", "?", "help")
}

func (m *Model) createRows(w, h int) (string, []string) {
	title := fmt.Sprintf("Your mods · %d", len(m.projects))
	var rows []string
	newSt := lipgloss.NewStyle().Foreground(ui.Equipment).Bold(true)
	rows = append(rows, m.mark("row:0", row(w, m.cur[tabCreate] == 0, []seg{{"✦ ", newSt}, {"New mod", newSt}}, []seg{{"n", stFaint}})))
	off := m.off[tabCreate]
	for i := max(1, off); i < m.createLen() && len(rows) < h; i++ {
		p := m.projects[i-1]
		sel := i == m.cur[tabCreate]
		g := seg{ui.GOff + " ", stDim}
		if _, ok := p.Built(); ok {
			g = seg{ui.GOK + " ", stOK}
		}
		if _, ok := p.Deployed(m.app.Game.Dir); ok {
			g = seg{ui.GOn + " ", stVoid}
		}
		nameSt := stName
		if sel {
			nameSt = stBold
		}
		left := []seg{g, {p.Name, nameSt}, {"  " + devkit.TemplateByID(p.Template).Label, stFaint}}
		rows = append(rows, m.mark(fmt.Sprintf("row:%d", i), row(w, sel, left, []seg{{p.Version, stDim}})))
	}
	if len(m.projects) == 0 {
		rows = append(rows, "", ui.Dim.Render("  No projects yet."))
	}
	return title, rows
}

func (m *Model) createDetail(w int) string {
	p := m.selProject()
	if p == nil {
		return m.createIntro(w)
	}
	var out []string
	out = append(out, ui.Accent.Render(p.Name)+ui.Dim.Render("  v"+p.Version+" · by "+p.Author), "")
	if p.Description != "" {
		out = append(out, wrap(p.Description, w), "")
	}
	status := ui.Dim.Render(ui.GOff + " not built yet")
	if t, ok := p.Built(); ok {
		status = ui.OK.Render(ui.GOK+" built") + ui.Dim.Render(" "+ui.Ago(t))
	}
	if t, ok := p.Deployed(m.app.Game.Dir); ok {
		status += ui.Dim.Render("   ") + ui.Sel.Render(ui.GOn+" in the game") + ui.Dim.Render(" since "+ui.Ago(t))
	} else {
		status += ui.Dim.Render("   not in the game")
	}
	out = append(out, status)
	out = append(out, ui.Fainter.Render(keepEnd(config.ShortPath(p.Dir), w)), "")

	out = append(out, ui.Bold.Render("Build with AI"))
	t := m.tools()
	for i, a := range devkit.Agents {
		key := []string{"c", "x"}[i]
		state := ui.OK.Render("ready")
		if t.agents[a.ID] == "" {
			state = ui.Bad.Render("not installed")
		}
		pref := ""
		if a.ID == m.preferredAgent().ID {
			pref = ui.Fainter.Render("  (enter)")
		}
		verb := "open in "
		if devkit.HasSession(a, p) {
			verb = "continue in "
		}
		out = append(out, "  "+m.hints(key, verb+a.Name)+"  "+state+pref)
	}
	out = append(out, "  "+m.hints("C", "new Claude chat", "X", "new Codex chat"))
	out = append(out, ui.Fainter.Render(indentLines(wrap("The agent starts in this folder with skills that teach it RoR2 modding, how to build and test here, and how to read game code.", w-2), "  ")), "")

	out = append(out, ui.Bold.Render("Develop"))
	out = append(out, indentLines(m.hintsW(w-2, "b", "build", "d", "deploy to game", "u", "remove from game", "p", "package", "f", "Finder", "L", "launch game"), "  "))
	if e := m.devEntry(p); e != nil && m.app.Log != nil {
		out = append(out, "", ui.Bold.Render("Last launch"))
		out = append(out, "  "+m.devLogStatus(p))
	}
	return strings.Join(out, "\n")
}

// devEntry finds the deployed copy in the inventory.
func (m *Model) devEntry(p *devkit.Project) *core.Entry {
	for _, e := range m.app.Inv.Entries {
		if e.Dev && e.Local != nil && e.Local.Name == p.DevFolder() {
			return e
		}
	}
	return nil
}

func (m *Model) devLogStatus(p *devkit.Project) string {
	l := m.app.Log
	for _, pl := range l.Loaded {
		if strings.EqualFold(pl.Name, p.Name) {
			return ui.OK.Render(ui.GOK+" loaded "+pl.Version) + ui.Dim.Render(" · "+ui.Ago(l.Time))
		}
	}
	for _, f := range l.Failures {
		if strings.EqualFold(f.Plugin.Name, p.Name) {
			return ui.Bad.Render(ui.GError + " failed: " + f.Reason)
		}
	}
	return ui.Dim.Render("not in the log yet · launch the game after deploying")
}

func (m *Model) createIntro(w int) string {
	t := m.tools()
	out := []string{
		ui.Gradient("Make your own mod", ui.Void, ui.Equipment, true), "",
		ui.Dim.Render(wrap("Describe what you want and let an AI coding agent build it with you. ror2 sets up a ready-to-build C# project wired to your game, with skills that teach the agent RoR2 modding, how to test here, and how to read the game's code.", w)), "",
		ui.Bold.Render("How it works"),
		ui.Dim.Render("  1  ") + "Pick a name, describe the idea, choose a starting template",
		ui.Dim.Render("  2  ") + "Claude Code or Codex opens in the project and proposes a plan",
		ui.Dim.Render("  3  ") + "It builds and deploys into your game; you launch and play",
		ui.Dim.Render("  4  ") + "Tell it what happened; it reads the BepInEx log and iterates",
		ui.Dim.Render("  5  ") + "When happy, package it for Thunderstore", "",
		ui.Bold.Render("Templates"),
	}
	for _, tp := range devkit.Templates {
		out = append(out, "  "+ui.Accent.Render(tp.Label), ui.Fainter.Render(indentLines(wrap(tp.About, w-2), "  ")))
	}
	out = append(out, "", ui.Bold.Render("Tools"))
	for _, a := range devkit.Agents {
		st := ui.OK.Render(ui.GOK + " found")
		if t.agents[a.ID] == "" {
			st = ui.Bad.Render(ui.GFail + " not installed")
		}
		out = append(out, "  "+a.Name+"  "+st)
	}
	dn := ui.OK.Render(ui.GOK + " " + t.dotnet)
	if t.dotnet == "" {
		dn = ui.Bad.Render(ui.GFail + " missing · brew install --cask dotnet-sdk")
	}
	out = append(out, "  .NET SDK  "+dn, "", m.hints("enter", "new mod"))
	return strings.Join(out, "\n")
}

func (m *Model) wizardView() string {
	w := m.wiz
	mw := min(m.modalWidth(), 84)
	label := func(i int, s string) string {
		st := ui.Dim
		if w.focus == i {
			st = lipgloss.NewStyle().Foreground(ui.Void).Bold(true)
		}
		return m.mark(fmt.Sprintf("cr:field:%d", i), st.Render(s))
	}
	field := func(i int, ti textinput.Model) string {
		bc := ui.Border
		if w.focus == i {
			bc = ui.BorderFocus
		}
		return m.mark(fmt.Sprintf("cr:field:%d", i), lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(bc).Width(mw-8).Render(ti.View()))
	}
	choice := func(i, sel int, opts []string, prefix string) string {
		var parts []string
		for j, o := range opts {
			st := lipgloss.NewStyle().Foreground(ui.Muted).Padding(0, 1)
			if j == sel {
				st = lipgloss.NewStyle().Background(ui.Void).Foreground(ui.PillFg).Bold(true).Padding(0, 1)
				if w.focus != i {
					st = lipgloss.NewStyle().Foreground(ui.Equipment).Bold(true).Padding(0, 1)
				}
			}
			parts = append(parts, m.mark(fmt.Sprintf("%s%d", prefix, j), st.Render(o)))
		}
		return strings.Join(parts, " ")
	}
	var tpls []string
	for _, t := range devkit.Templates {
		tpls = append(tpls, t.Label)
	}
	var agents []string
	for _, a := range devkit.Agents {
		agents = append(agents, a.Name)
	}
	agents = append(agents, "Don't open yet")
	t := devkit.Templates[w.template]

	lines := []string{
		label(0, "Mod name"), field(0, w.inputs[0]), "",
		label(1, "Author (your Thunderstore team name)"), field(1, w.inputs[1]), "",
		label(2, "What should it do?"), field(2, w.inputs[2]), "",
		label(3, "Start from"), choice(3, w.template, tpls, "cr:tpl:"),
		ui.Fainter.Render(ansi.Truncate(t.About, mw-8, "…")), "",
		label(4, "Then open in"), choice(4, w.agent, agents, "cr:agent:"), "",
	}
	if len(t.Deps) > 0 {
		var need []string
		for _, d := range t.Deps {
			if m.app.Inv.Find(d) == nil {
				_, n := splitFull(d)
				need = append(need, n)
			}
		}
		if len(need) > 0 {
			lines = append(lines, ui.Info.Render(ui.GInfo+" Also installs into your game: "+strings.Join(need, ", ")), "")
		}
	}
	if w.err != "" {
		lines = append(lines, ui.Bad.Render(ui.GError+" "+w.err), "")
	}
	create := lipgloss.NewStyle().Background(ui.Equipment).Foreground(ui.PillFg).Bold(true).Padding(0, 2).Render("Create mod")
	if w.focus == 5 {
		create = lipgloss.NewStyle().Background(ui.Void).Foreground(ui.PillFg).Bold(true).Padding(0, 2).Render("Create mod")
	}
	lines = append(lines, m.mark("cr:create", create)+"   "+m.mark("cr:cancel", ui.Dim.Render("cancel"))+"   "+
		ui.Fainter.Render("tab next · ←→ choose · enter create · esc cancel"))
	return m.modal("New mod", strings.Join(lines, "\n"), ui.Void, mw)
}

func splitFull(full string) (string, string) {
	if i := strings.Index(full, "-"); i > 0 {
		return full[:i], full[i+1:]
	}
	return "", full
}

func indentLines(s, pad string) string {
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}
