package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"ror2mm/internal/config"
	"ror2mm/internal/core"
	"ror2mm/internal/devkit"
	"ror2mm/internal/ui"
)

func devUsage() {
	header("dev · make your own mods")
	cmd := lipgloss.NewStyle().Foreground(ui.Equipment).Width(34)
	line := func(c, d string) { out("    %s %s", cmd.Render(c), ui.Dim.Render(d)) }
	line("ror2 dev new [name]", "create a project  (--template blank|tweak|item, --author, --desc)")
	line("ror2 dev list", "your projects")
	line("ror2 dev build [project]", "compile")
	line("ror2 dev deploy [project]", "compile and install into the game")
	line("ror2 dev undeploy [project]", "remove the dev copy from the game")
	line("ror2 dev log [project]", "this mod's log lines and errors  (--all, -n 200)")
	line("ror2 dev package [project]", "Thunderstore zip in build/")
	line("ror2 dev types <word>", "find game types  (--dll RoR2BepInExPack)")
	line("ror2 dev decompile <Type>", "read a game type's code  (--dll …)")
	line("ror2 dev ai [project]", "open Claude Code in the project  (--codex, --new for a fresh chat)")
	fmt.Println()
	note("Inside a project folder the project name can be left out.")
	note("Projects live in " + config.ShortPath(devkit.Workspace(mustCfg())) + " (change: ror2 config set workspace <path>).")
	fmt.Println()
}

func mustCfg() *config.Config {
	c, _, _ := config.Load()
	return c
}

func cmdDev(ctx context.Context, a *core.App, f flags) int {
	if len(f.args) == 0 {
		devUsage()
		return 0
	}
	sub, rest := f.args[0], f.args[1:]
	f.args = rest
	switch sub {
	case "new":
		return devNew(ctx, a, f)
	case "list", "ls":
		return devList(a)
	case "build":
		return devBuild(ctx, a, f, false)
	case "deploy":
		return devBuild(ctx, a, f, true)
	case "undeploy":
		p, ok := devProject(a, f)
		if !ok {
			return 1
		}
		if err := p.Undeploy(a.Game.Dir); err != nil {
			errorf(err.Error())
			return 1
		}
		success("Removed " + p.DevFolder() + " from the game.")
		return 0
	case "package", "pack":
		return devPackage(ctx, a, f)
	case "log", "logs":
		return devLog(a, f)
	case "types":
		return devTypes(a, f)
	case "decompile", "source":
		return devDecompile(a, f)
	case "ai", "open", "claude", "codex":
		if sub == "codex" {
			f.codex = true
		}
		return devAI(a, f)
	case "path":
		p, ok := devProject(a, f)
		if !ok {
			return 1
		}
		fmt.Println(p.Dir)
		return 0
	default:
		errorf("unknown dev command " + ui.Bold.Render(sub))
		devUsage()
		return 2
	}
}

// devProject resolves the project from an argument or the current folder.
func devProject(a *core.App, f flags) (*devkit.Project, bool) {
	if len(f.args) > 0 {
		name := f.args[0]
		dir := name
		if !strings.Contains(name, "/") {
			dir = filepath.Join(devkit.Workspace(a.Cfg), name)
		}
		p, err := devkit.Load(dir)
		if err != nil {
			errorf("No project " + ui.Bold.Render(name) + " (see: ror2 dev list)")
			return nil, false
		}
		return p, true
	}
	wd, _ := os.Getwd()
	p, err := devkit.Find(wd)
	if err != nil {
		errorf(err.Error())
		return nil, false
	}
	return p, true
}

func ask(label, def string) string {
	if !isTTY {
		return def
	}
	hint := ""
	if def != "" {
		hint = ui.Dim.Render(" [" + def + "]")
	}
	fmt.Printf("  %s %s%s: ", ui.Accent.Render("?"), label, hint)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if v := strings.TrimSpace(line); v != "" {
		return v
	}
	return def
}

// DefaultAuthor guesses an author name from git or the macOS account.
func DefaultAuthor(cfg *config.Config) string {
	if a := cfg.Get("author", ""); a != "" {
		return a
	}
	if out, err := exec.Command("git", "config", "--global", "user.name").Output(); err == nil {
		if n := cleanName(string(out)); n != "" {
			return n
		}
	}
	if u := os.Getenv("USER"); u != "" {
		return cleanName(u)
	}
	return "Me"
}

var nonName = regexp.MustCompile(`[^A-Za-z0-9_]`)

func cleanName(s string) string {
	s = nonName.ReplaceAllString(strings.TrimSpace(s), "")
	if s == "" || !regexp.MustCompile(`^[A-Za-z]`).MatchString(s) {
		return ""
	}
	return s
}

func devNew(ctx context.Context, a *core.App, f flags) int {
	header("New mod")
	_ = loadCatalog(ctx, a, f)
	name := ""
	if len(f.args) > 0 {
		name = f.args[0]
	}
	if name == "" {
		name = ask("Mod name (letters, digits, _)", "")
	}
	author := f.author
	if author == "" {
		author = ask("Author", DefaultAuthor(a.Cfg))
	}
	desc := f.desc
	if desc == "" {
		desc = ask("What should it do?", "")
	}
	tid := f.template
	if tid == "" {
		for i, t := range devkit.Templates {
			out("    %s %s  %s", ui.Accent.Render(fmt.Sprint(i+1)), ui.Bold.Render(t.Label), ui.Dim.Render(t.About))
		}
		pick := ask("Template", "1")
		for i, t := range devkit.Templates {
			if pick == fmt.Sprint(i+1) || strings.EqualFold(pick, t.ID) {
				tid = t.ID
			}
		}
	}
	t := devkit.TemplateByID(tid)
	if err := devkit.ValidName("Mod name", name); err != nil {
		errorf(err.Error())
		return 1
	}
	if err := devkit.ValidName("Author", author); err != nil {
		errorf(err.Error())
		return 1
	}
	if code := ensureTemplateDeps(ctx, a, t, f.yes); code != 0 {
		return code
	}
	p, err := devkit.New(devkit.Workspace(a.Cfg), devkit.NewOptions{
		Name: name, Author: author, Description: desc, Template: t.ID, GameDir: a.Game.Dir, Deps: a.TemplateDeps(t),
	})
	if err != nil {
		errorf(err.Error())
		return 1
	}
	a.Cfg.Set("author", author)
	_ = a.Cfg.Save()
	success("Created " + ui.Bold.Render(p.Name) + " in " + config.ShortPath(p.Dir))
	update, stop := spinner("First build…")
	_ = update
	err = p.Build(ctx, a.Game.Dir, new(strings.Builder))
	stop()
	if err != nil {
		warnf("The first build failed; run `ror2 dev build` in the project to see why.")
	} else {
		success("It builds.")
	}
	fmt.Println()
	note("Next: cd " + config.ShortPath(p.Dir) + " && ror2 dev ai   (or open it from the Create tab in ror2)")
	fmt.Println()
	return 0
}

func ensureTemplateDeps(ctx context.Context, a *core.App, t devkit.Template, yes bool) int {
	if len(t.Deps) == 0 || a.Cat == nil {
		return 0
	}
	plan, err := a.TemplatePlan(t)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	if plan.Empty() {
		return 0
	}
	note("This template needs a few libraries in the game first:")
	return applyPlan(ctx, a, plan, "before-dev-template", yes)
}

func devList(a *core.App) int {
	header("Your mods")
	ws := devkit.Workspace(a.Cfg)
	list := devkit.List(ws)
	if len(list) == 0 {
		note("No projects yet. Create one: ror2 dev new")
		fmt.Println()
		return 0
	}
	for _, p := range list {
		state := ui.Dim.Render("not built")
		if t, ok := p.Built(); ok {
			state = ui.OK.Render(ui.GOK+" built") + ui.Dim.Render(" "+ui.Ago(t))
		}
		if _, ok := p.Deployed(a.Game.Dir); ok {
			state += "  " + ui.Sel.Render(ui.GOn+" in game")
		}
		out("  %s %s  %s  %s", ui.Accent.Render(ui.GLocal), ui.Bold.Render(p.Name), ui.Dim.Render(p.Version+" · "+devkit.TemplateByID(p.Template).Label), state)
		if p.Description != "" {
			out("    %s", ui.Fainter.Render(ansiTrunc(p.Description, termWidth()-6)))
		}
	}
	fmt.Println()
	note("In " + config.ShortPath(ws))
	fmt.Println()
	return 0
}

func devBuild(ctx context.Context, a *core.App, f flags, deploy bool) int {
	p, ok := devProject(a, f)
	if !ok {
		return 1
	}
	var buf strings.Builder
	update, stop := spinner("Building " + p.Name + "…")
	_ = update
	err := p.Build(ctx, a.Game.Dir, &buf)
	stop()
	output := strings.TrimSpace(buf.String())
	if err != nil {
		errorf("Build failed")
		fmt.Println(indent(output, 4))
		return 1
	}
	for _, l := range strings.Split(output, "\n") {
		if strings.Contains(l, "warning CS") {
			out("    %s", ui.Warn.Render(strings.TrimSpace(l)))
		}
	}
	success("Built " + config.ShortPath(p.DLL()))
	if !deploy {
		return 0
	}
	dest, err := p.Deploy(a.Game.Dir)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	success("Deployed to " + config.ShortPath(dest))
	if core.GameRunning() {
		warnf("The game is running; restart it to load the new build.")
	} else {
		note("Start the game with: ror2 launch")
	}
	return 0
}

func devPackage(ctx context.Context, a *core.App, f flags) int {
	p, ok := devProject(a, f)
	if !ok {
		return 1
	}
	var buf strings.Builder
	if err := p.Build(ctx, a.Game.Dir, &buf); err != nil {
		errorf("Build failed")
		fmt.Println(indent(strings.TrimSpace(buf.String()), 4))
		return 1
	}
	zip, err := p.Package()
	if err != nil {
		errorf("Not ready to package: " + err.Error())
		return 1
	}
	success("Package: " + config.ShortPath(zip))
	note("Upload it at thunderstore.io → Upload (community: Risk of Rain 2).")
	return 0
}

var logLevel = regexp.MustCompile(`^\[(\w+)\s*:\s*([^\]]+)\]`)

func devLog(a *core.App, f flags) int {
	path := filepath.Join(a.Game.Dir, "BepInEx", "LogOutput.log")
	data, err := os.ReadFile(path)
	if err != nil {
		errorf("No BepInEx log yet. Launch the game once (ror2 launch).")
		return 1
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	var name string
	if !f.all {
		if p, ok := devProjectQuiet(a, f); ok {
			name = p.Name
		}
	}
	var keep []string
	inTrace := false
	for _, l := range lines {
		if f.all {
			keep = append(keep, l)
			continue
		}
		m := logLevel.FindStringSubmatch(l)
		switch {
		case m != nil:
			src := strings.TrimSpace(m[2])
			lvl := strings.ToLower(m[1])
			mine := name != "" && strings.EqualFold(src, name)
			bad := lvl == "error" || lvl == "fatal" || strings.Contains(l, "Exception") || strings.Contains(l, "Could not load")
			mentions := name != "" && strings.Contains(l, name)
			inTrace = mine || bad || mentions
			if inTrace {
				keep = append(keep, l)
			}
		case inTrace && strings.TrimSpace(l) != "":
			keep = append(keep, l) // stack trace continuation
		}
	}
	n := f.limit
	if n == 20 {
		n = 80
	}
	if len(keep) > n {
		keep = keep[len(keep)-n:]
	}
	fi, _ := os.Stat(path)
	what := "errors"
	if name != "" {
		what = name + " and errors"
	}
	if f.all {
		what = "everything"
	}
	note(fmt.Sprintf("BepInEx log from %s, %s (last %d lines)", ui.Ago(fi.ModTime()), what, len(keep)))
	for _, l := range keep {
		switch {
		case strings.Contains(l, "Error") || strings.Contains(l, "Exception") || strings.Contains(l, "Fatal"):
			fmt.Println(ui.Bad.Render(l))
		case strings.HasPrefix(l, "[Warning"):
			fmt.Println(ui.Warn.Render(l))
		case name != "" && strings.Contains(l, ":"+name+"]"):
			fmt.Println(ui.OK.Render(l))
		default:
			fmt.Println(l)
		}
	}
	return 0
}

func devProjectQuiet(a *core.App, f flags) (*devkit.Project, bool) {
	if len(f.args) > 0 {
		p, err := devkit.Load(filepath.Join(devkit.Workspace(a.Cfg), f.args[0]))
		return p, err == nil
	}
	wd, _ := os.Getwd()
	p, err := devkit.Find(wd)
	return p, err == nil
}

func devTypes(a *core.App, f flags) int {
	if len(f.args) == 0 {
		errorf("Give part of a type name, e.g. ror2 dev types CharacterBody")
		return 2
	}
	update, stop := spinner("Reading types…")
	types, err := devkit.ListTypes(a.Game.Dir, f.dll, f.args[0], update)
	stop()
	if err != nil {
		errorf(err.Error())
		return 1
	}
	if len(types) == 0 {
		note("No matching types.")
		return 0
	}
	if len(types) > 300 {
		note(fmt.Sprintf("%d matches, showing 300; be more specific.", len(types)))
		types = types[:300]
	}
	fmt.Println(strings.Join(types, "\n"))
	return 0
}

func devDecompile(a *core.App, f flags) int {
	if len(f.args) == 0 {
		errorf("Give a type, e.g. ror2 dev decompile RoR2.CharacterBody")
		return 2
	}
	update, stop := spinner("Decompiling…")
	src, err := devkit.Decompile(a.Game.Dir, f.dll, f.args[0], update)
	stop()
	if err != nil {
		errorf(err.Error())
		return 1
	}
	fmt.Print(src)
	return 0
}

func devAI(a *core.App, f flags) int {
	p, ok := devProject(a, f)
	if !ok {
		return 1
	}
	agent := devkit.Agents[0]
	if f.codex {
		agent = devkit.Agents[1]
	}
	bin := devkit.AgentPath(agent)
	if bin == "" {
		errorf(agent.Name + " is not installed.")
		return 1
	}
	if agent.ID == "claude" && !devkit.ClaudeTrusted(p.Dir) {
		warnf(devkit.TrustHint)
	}
	note("Opening " + agent.Name + " in " + config.ShortPath(p.Dir) + "…")
	cmd := devkit.AgentCommand(agent, bin, p, a.Game.Dir, "", !f.fresh)
	p.MarkSession(agent)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		errorf(err.Error())
		return 1
	}
	return 0
}
