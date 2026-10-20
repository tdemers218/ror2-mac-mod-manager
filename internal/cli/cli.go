// Package cli implements the ror2 commands.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ror2mm/internal/config"
	"ror2mm/internal/core"
	"ror2mm/internal/game"
	"ror2mm/internal/termimg"
	ts "ror2mm/internal/thunderstore"
	"ror2mm/internal/ui"
)

type flags struct {
	yes, offline, all, configs, reinstall, fix, codex, libs, fresh bool
	sort, dll, template, author, desc, tab                         string
	limit                                                          int
	args                                                           []string
}

func parse(argv []string) (flags, error) {
	f := flags{limit: 20}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		val := func() (string, error) {
			if j := strings.Index(a, "="); j >= 0 {
				return a[j+1:], nil
			}
			if i+1 >= len(argv) {
				return "", fmt.Errorf("%s needs a value", a)
			}
			i++
			return argv[i], nil
		}
		switch {
		case a == "-y" || a == "--yes":
			f.yes = true
		case a == "--offline":
			f.offline = true
		case a == "--all" || a == "-a":
			f.all = true
		case a == "--configs" || a == "--settings":
			f.configs = true
		case a == "--reinstall":
			f.reinstall = true
		case a == "--fix":
			f.fix = true
		case a == "--codex":
			f.codex = true
		case a == "--new" || a == "--fresh":
			f.fresh = true
		case a == "--libs" || a == "--libraries":
			f.libs = true
		case a == "--dll" || strings.HasPrefix(a, "--dll="),
			a == "--template" || strings.HasPrefix(a, "--template="),
			a == "--author" || strings.HasPrefix(a, "--author="),
			a == "--desc" || strings.HasPrefix(a, "--desc="),
			a == "--tab" || strings.HasPrefix(a, "--tab="):
			v, err := val()
			if err != nil {
				return f, err
			}
			switch {
			case strings.HasPrefix(a, "--dll"):
				f.dll = v
			case strings.HasPrefix(a, "--template"):
				f.template = v
			case strings.HasPrefix(a, "--author"):
				f.author = v
			case strings.HasPrefix(a, "--tab"):
				f.tab = v
			default:
				f.desc = v
			}
		case a == "--sort" || strings.HasPrefix(a, "--sort="):
			v, err := val()
			if err != nil {
				return f, err
			}
			f.sort = v
		case a == "-n" || a == "--limit" || strings.HasPrefix(a, "--limit="):
			v, err := val()
			if err != nil {
				return f, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return f, fmt.Errorf("bad number %q", v)
			}
			f.limit = n
		case strings.HasPrefix(a, "-") && len(a) > 1:
			return f, fmt.Errorf("unknown option %s", a)
		default:
			f.args = append(f.args, a)
		}
	}
	return f, nil
}

// Run executes a command and returns the exit code. tuiFn starts the interactive UI.
func Run(argv []string, tuiFn func(a *core.App, search, tab string) error) int {
	cmd := ""
	if len(argv) > 0 {
		cmd, argv = argv[0], argv[1:]
		// Options without a command (ror2 --tab create) open the app.
		if strings.HasPrefix(cmd, "-") && cmd != "-h" && cmd != "--help" && cmd != "-v" && cmd != "--version" {
			argv = append([]string{cmd}, argv...)
			cmd = ""
		}
	}
	f, err := parse(argv)
	if err != nil {
		errorf(err.Error())
		return 2
	}
	// Use the theme picked in the Settings tab for plain output too.
	if cfg, _, err := config.Load(); err == nil {
		name, dark := cfg.Get("theme", ""), true
		if name == "" {
			dark = lipgloss.HasDarkBackground()
		}
		ui.Apply(ui.ThemeByName(name, dark))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	switch cmd {
	case "help", "-h", "--help":
		usage()
		return 0
	case "version", "--version", "-v":
		out("ror2 %s", ui.Version)
		return 0
	case "config":
		return cmdConfig(f)
	}

	a, err := core.New()
	if err != nil {
		header("")
		errorf(err.Error())
		return 1
	}

	switch cmd {
	case "", "ui", "browse":
		if !isTTY {
			usage()
			return 0
		}
		if err := tuiFn(a, strings.Join(f.args, " "), f.tab); err != nil {
			errorf(err.Error())
			return 1
		}
		return 0
	case "search", "s":
		return cmdSearch(ctx, a, f)
	case "info", "show":
		return cmdInfo(ctx, a, f)
	case "install", "i", "add":
		return cmdInstall(ctx, a, f)
	case "update", "up", "upgrade":
		return cmdUpdate(ctx, a, f)
	case "remove", "rm", "uninstall":
		return cmdRemove(ctx, a, f)
	case "enable", "disable":
		return cmdEnable(ctx, a, f, cmd == "enable")
	case "list", "ls":
		return cmdList(ctx, a, f)
	case "doctor", "dr", "check":
		return cmdDoctor(ctx, a, f)
	case "adopt":
		return cmdAdopt(ctx, a, f)
	case "import":
		return cmdImport(ctx, a, f)
	case "launch", "play", "run":
		return cmdLaunch(a)
	case "refresh":
		return cmdRefresh(ctx, a)
	case "backups":
		return cmdBackups(a)
	case "restore":
		return cmdRestore(a, f)
	case "dev", "create", "make":
		return cmdDev(ctx, a, f)
	case "pack", "packs", "modpack":
		return cmdPack(ctx, a, f)
	default:
		errorf("unknown command " + ui.Bold.Render(cmd))
		note("Run `ror2 help` to see commands.")
		return 2
	}
}

func usage() {
	header("")
	cmd := lipgloss.NewStyle().Foreground(ui.Equipment).Width(26)
	section := func(t string) { out("  %s", ui.Sel.Bold(true).Render(t)) }
	line := func(c, d string) { out("    %s %s", cmd.Render(c), ui.Dim.Render(d)) }
	section("Interactive")
	line("ror2 [search words]", "open the full-screen app (optionally searching Browse)")
	line("ror2 --tab <name>", "open on browse, installed, doctor, create or settings")
	fmt.Println()
	section("Mods")
	line("ror2 search <words>", "search Thunderstore  (--sort downloads|updated|rating, -n 30, --libs)")
	line("ror2 info <mod>", "details and README")
	line("ror2 install <mod>…", "install with dependencies  (--reinstall)")
	line("ror2 update [mod…]", "update one, some or all tracked mods")
	line("ror2 remove <mod>…", "uninstall (configs are kept)")
	line("ror2 enable|disable <mod>…", "turn mods on or off without deleting")
	line("ror2 pack [list]", "mod packs: saved sets of mods, plus the built-in Vanilla")
	line("ror2 pack save <name>", "save the mods that are on now  (--configs keeps their settings)")
	line("ror2 pack load <name>", "switch to a pack (Vanilla = play with no mods)")
	line("ror2 list", "what is installed")
	fmt.Println()
	section("Health")
	line("ror2 doctor", "find missing dependencies and load errors  (--fix, --all)")
	line("ror2 adopt", "track mods you installed by hand")
	line("ror2 import <code|file.r2z>", "install an r2modman profile  (--configs)")
	fmt.Println()
	section("Make your own")
	line("ror2 dev", "create, build and test mods with Claude Code or Codex")
	fmt.Println()
	section("Game & housekeeping")
	line("ror2 launch", "start Risk of Rain 2 through your Sikarugir wrapper")
	line("ror2 refresh", "re-download the Thunderstore catalog")
	line("ror2 backups / restore <name>", "BepInEx snapshots taken before each change")
	line("ror2 config [set <key> <value>]", "game-dir, wrapper, workspace, catalog-max-age, keep-backups")
	fmt.Println()
	note("Mods can be named loosely: LookingGlass, DropPod-LookingGlass, or a Thunderstore URL.")
	note("-y answers yes to prompts. --offline uses the cached catalog.")
	fmt.Println()
}

// loadCatalog makes sure the catalog is there, with a spinner when downloading.
func loadCatalog(ctx context.Context, a *core.App, f flags) bool {
	if a.Cat == nil {
		_ = a.LoadCachedCatalog()
	}
	if !f.offline && a.CatalogStale() {
		update, stop := spinner("Fetching the Thunderstore catalog…")
		w, err := a.EnsureCatalog(ctx, false, func(read, total int64) {
			if total > 0 {
				update(fmt.Sprintf("Fetching the Thunderstore catalog…  %s  %s", ui.Bar(float64(read)/float64(total), 20), ui.Dim.Render(ui.Bytes(read))))
			}
		})
		stop()
		if err != nil {
			errorf(err.Error())
			return false
		}
		if w != "" {
			warnf(w)
		}
	}
	if a.Cat == nil {
		errorf("No catalog available. Check your connection and try again.")
		return false
	}
	return true
}

// resolve turns names typed by the player into packages.
func resolve(a *core.App, names []string) ([]core.Request, bool) {
	var reqs []core.Request
	ok := true
	for _, n := range names {
		p, ver, alts := a.Cat.Lookup(n)
		if p == nil {
			errorf(fmt.Sprintf("No mod called %s on Thunderstore.", ui.Bold.Render(n)))
			if hits := a.Cat.Search(n, ts.SearchOptions{}); len(hits) > 0 {
				var s []string
				for i, h := range hits {
					if i == 4 {
						break
					}
					s = append(s, h.FullName)
				}
				note("Did you mean: " + strings.Join(s, ", ") + "?")
			}
			ok = false
			continue
		}
		if len(alts) > 1 && alts[1].Downloads*10 > p.Downloads {
			note(fmt.Sprintf("%q matched %s (also: %s). Use the full name to pick another.", n, p.FullName, alts[1].FullName))
		}
		reqs = append(reqs, core.Request{FullName: p.FullName, Version: ver})
	}
	return reqs, ok
}

// findInstalled matches typed names against installed entries.
func findInstalled(a *core.App, names []string) ([]*core.Entry, bool) {
	var outE []*core.Entry
	ok := true
	for _, n := range names {
		var hit *core.Entry
		nn := ts.Normalize(n)
		for _, e := range a.Inv.Entries {
			if strings.EqualFold(e.FullName, n) || (e.FullName != "" && strings.EqualFold(strings.ReplaceAll(e.FullName, "-", "/"), n)) {
				hit = e
				break
			}
		}
		if hit == nil {
			for _, e := range a.Inv.Entries {
				if ts.Normalize(e.Name) == nn || (e.Local != nil && ts.Normalize(e.Local.Name) == nn) {
					hit = e
					break
				}
			}
		}
		if hit == nil {
			errorf(fmt.Sprintf("%s is not installed.", ui.Bold.Render(n)))
			ok = false
			continue
		}
		outE = append(outE, hit)
	}
	return outE, ok
}

func warnIfRunning() {
	if core.GameRunning() {
		warnf("Risk of Rain 2 is running. Changes take effect after you restart it.")
	}
}

func applyPlan(ctx context.Context, a *core.App, plan *core.Plan, label string, yes bool) int {
	printPlan(plan)
	if plan.Empty() {
		fmt.Println()
		success("Nothing to do.")
		return 0
	}
	if !confirm("Go ahead?", yes) {
		note("Cancelled.")
		return 1
	}
	fmt.Println()
	warnIfRunning()
	pp := &progressPrinter{}
	if err := a.Apply(ctx, plan, label, pp.event); err != nil {
		fmt.Println()
		errorf("Stopped: " + err.Error())
		note("A backup was taken first; see `ror2 backups`.")
		return 1
	}
	fmt.Println()
	return 0
}

// ---------- commands ----------

func cmdSearch(ctx context.Context, a *core.App, f flags) int {
	if !loadCatalog(ctx, a, f) {
		return 1
	}
	opt := ts.SearchOptions{Sort: ts.SortRelevance, HideLibraries: !f.libs && a.Cfg.Get("browse_libraries", "hide") == "hide"}
	switch f.sort {
	case "downloads", "dl":
		opt.Sort = ts.SortDownloads
	case "updated", "new", "recent":
		opt.Sort = ts.SortUpdated
	case "rating", "likes":
		opt.Sort = ts.SortRating
	case "name":
		opt.Sort = ts.SortName
	}
	q := strings.Join(f.args, " ")
	hits := a.Cat.Search(q, opt)
	title := "Search"
	if q != "" {
		title = "Search · " + q
	}
	header(title)
	if len(hits) == 0 {
		note("Nothing found.")
		return 0
	}
	t := newTable("", "Mod", "Author", ui.GDownload, "Updated", "About")
	descW := termWidth() - 70
	if descW < 20 {
		descW = 20
	}
	for i, p := range hits {
		if i == f.limit {
			break
		}
		st := " "
		if e := a.Inv.Find(p.FullName); e != nil {
			st = ui.OK.Render(ui.GOK)
			if e.HasUpdate() {
				st = ui.Warn.Render(ui.GUpdate)
			}
		}
		t.Row(st, ui.Bold.Render(p.Name), ui.Dim.Render(p.Owner), ui.Count(p.Downloads), ui.Dim.Render(ui.Ago(p.Updated)), ansiTrunc(p.Description, descW))
	}
	out("%s", indent(t.Render(), 2))
	note(fmt.Sprintf("%d result(s)%s · install with: ror2 install <mod>", len(hits), more(len(hits), f.limit)))
	fmt.Println()
	return 0
}

func more(n, limit int) string {
	if n > limit {
		return fmt.Sprintf(", showing %d (use -n for more)", limit)
	}
	return ""
}

func ansiTrunc(s string, w int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func cmdInfo(ctx context.Context, a *core.App, f flags) int {
	if len(f.args) == 0 {
		errorf("Which mod? e.g. ror2 info LookingGlass")
		return 2
	}
	if !loadCatalog(ctx, a, f) {
		return 1
	}
	p, _, _ := a.Cat.Lookup(strings.Join(f.args, " "))
	if p == nil {
		resolve(a, []string{strings.Join(f.args, " ")})
		return 1
	}
	header(p.Name)
	dark := lipgloss.HasDarkBackground()
	store := termimg.NewStore(filepath.Join(config.CacheDir(), "images"), termimg.Detect(), dark, os.Stdout)
	card := PackageCard(a, p, termWidth()-4)
	if isTTY && store.Mode != termimg.Off && p.Icon != "" && store.Load(ctx, p.Icon) == nil {
		if icon := store.Render(p.Icon, 16, 8); icon != nil && termWidth() > 70 {
			card = lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(icon, "\n"), "  ", PackageCard(a, p, termWidth()-24))
		}
	}
	fmt.Println(indent(card, 2))
	fmt.Println()
	update, stop := spinner("Loading README…")
	md, err := ts.Readme(ctx, p, p.Latest().Number)
	if err != nil {
		stop()
		note("README unavailable: " + err.Error())
		return 0
	}
	md, refs := termimg.ExtractImages(md)
	if !isTTY {
		store.Mode = termimg.Off
	}
	if store.Mode != termimg.Off && len(refs) > 0 {
		var wg sync.WaitGroup
		var done atomic.Int32
		for _, r := range refs {
			wg.Add(1)
			go func(url string) {
				defer wg.Done()
				_ = store.Load(ctx, url)
				update(fmt.Sprintf("Loading README images… %d/%d", done.Add(1), len(refs)))
			}(r.URL)
		}
		wg.Wait()
	}
	stop()
	width := min(termWidth()-4, 110)
	rendered := ui.RenderMarkdown(md, width, dark)
	fmt.Println(termimg.Compose(rendered, refs, store, width-4, 40, "◌"))
	return 0
}

// PackageCard renders a package summary box.
func PackageCard(a *core.App, p *ts.Package, width int) string {
	var b strings.Builder
	l := p.Latest()
	b.WriteString(ui.Accent.Render(p.Name) + "  " + ui.Dim.Render("v"+l.Number+"  by "+p.Owner) + "\n")
	if p.Description != "" {
		b.WriteString(lipgloss.NewStyle().Width(width-4).Render(p.Description) + "\n")
	}
	b.WriteString("\n")
	stats := fmt.Sprintf("%s %s downloads   %s %d   updated %s", ui.GDownload, ui.Count(p.Downloads), ui.GStar, p.Rating, ui.Ago(p.Updated))
	b.WriteString(ui.Dim.Render(stats) + "\n")
	if len(p.Categories) > 0 {
		var chips []string
		for _, c := range p.Categories {
			chips = append(chips, lipgloss.NewStyle().Foreground(ui.Void).Render("#"+strings.ReplaceAll(c, " ", "")))
		}
		b.WriteString(strings.Join(chips, " ") + "\n")
	}
	if e := a.Inv.Find(p.FullName); e != nil {
		st := ui.OK.Render(ui.GOK + " installed " + e.Version)
		if !e.Managed {
			st = ui.Info.Render(ui.GLocal + " installed by hand " + e.Version)
		}
		if e.HasUpdate() {
			st += ui.Warn.Render("  " + ui.GUpdate + " " + e.Latest() + " available")
		}
		b.WriteString(st + "\n")
	}
	if p.Deprecated {
		b.WriteString(ui.Bad.Render(ui.GWarn+" deprecated") + "\n")
	}
	if len(l.Deps) > 0 {
		b.WriteString("\n" + ui.Bold.Render("Needs") + "\n")
		for _, d := range l.Deps {
			full, min := ts.ParseDep(d)
			_, name := ts.SplitFullName(full)
			mark := ui.Bad.Render(ui.GFail)
			extra := ""
			if e := a.Inv.Find(full); e != nil {
				mark = ui.OK.Render(ui.GOK)
				extra = " (have " + e.Version + ")"
			} else if strings.EqualFold(name, "BepInExPack") && a.Inv.BepInEx.Installed {
				mark = ui.OK.Render(ui.GOK)
				extra = " (loader)"
			}
			b.WriteString(fmt.Sprintf("  %s %s %s\n", mark, name, ui.Dim.Render("≥"+min+extra)))
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.Border).Padding(0, 1).Width(width)
	return box.Render(strings.TrimRight(b.String(), "\n"))
}

func cmdInstall(ctx context.Context, a *core.App, f flags) int {
	if len(f.args) == 0 {
		errorf("Which mod? e.g. ror2 install LookingGlass")
		return 2
	}
	if !loadCatalog(ctx, a, f) {
		return 1
	}
	header("Install")
	reqs, ok := resolve(a, f.args)
	if !ok {
		return 1
	}
	for i := range reqs {
		reqs[i].Reinstall = f.reinstall
	}
	plan, err := a.PlanInstall(reqs)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	return applyPlan(ctx, a, plan, "before-install", f.yes)
}

func cmdUpdate(ctx context.Context, a *core.App, f flags) int {
	if !loadCatalog(ctx, a, f) {
		return 1
	}
	header("Update")
	var names []string
	if len(f.args) > 0 {
		es, ok := findInstalled(a, f.args)
		if !ok {
			return 1
		}
		for _, e := range es {
			if e.FullName == "" {
				warnf(e.Name + " is not on Thunderstore, so it cannot be updated here.")
				continue
			}
			names = append(names, e.FullName)
		}
	}
	plan, err := a.PlanUpdate(names)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	if len(f.args) == 0 {
		// Mention hand-installed mods that have updates too.
		var local []string
		for _, e := range a.Inv.Entries {
			if !e.Managed && e.HasUpdate() {
				local = append(local, e.Name)
			}
		}
		if len(local) > 0 {
			note("Hand-installed mods with updates: " + strings.Join(local, ", ") + ". Run `ror2 adopt` to track them, or name them here.")
			fmt.Println()
		}
	}
	return applyPlan(ctx, a, plan, "before-update", f.yes)
}

func cmdRemove(ctx context.Context, a *core.App, f flags) int {
	if len(f.args) == 0 {
		errorf("Which mod? e.g. ror2 remove LookingGlass")
		return 2
	}
	_ = loadCatalogQuiet(a)
	header("Remove")
	es, ok := findInstalled(a, f.args)
	if !ok {
		return 1
	}
	var managed []string
	var local []*core.Entry
	for _, e := range es {
		deps := a.Inv.Dependents(e.FullName)
		var names []string
		for _, d := range deps {
			if d != e && !contains(es, d) {
				names = append(names, d.Name)
			}
		}
		line := "  " + ui.Bad.Render("−") + " " + ui.Bold.Render(e.Name) + " " + ui.Dim.Render(e.Version)
		if !e.Managed {
			line += ui.Info.Render("  hand-installed: " + strings.Join(e.Local.Paths, ", "))
			local = append(local, e)
		} else {
			managed = append(managed, e.FullName)
		}
		out("%s", line)
		if len(names) > 0 {
			warnf("Still needed by " + strings.Join(names, ", "))
		}
	}
	if !confirm("Remove?", f.yes) {
		note("Cancelled.")
		return 1
	}
	warnIfRunning()
	if err := a.Remove(managed); err != nil {
		errorf(err.Error())
		return 1
	}
	if err := a.RemoveLocalEntries(local); err != nil {
		errorf(err.Error())
		return 1
	}
	success(fmt.Sprintf("Removed %d mod(s). Config files were kept.", len(es)))
	if orph := a.Orphans(); len(orph) > 0 {
		var n []string
		for _, o := range orph {
			n = append(n, o.Name)
		}
		note("No longer needed by anything: " + strings.Join(n, ", ") + " (ror2 doctor --fix --all removes them)")
	}
	fmt.Println()
	return 0
}

func contains(es []*core.Entry, e *core.Entry) bool {
	for _, x := range es {
		if x == e {
			return true
		}
	}
	return false
}

func loadCatalogQuiet(a *core.App) error {
	if a.Cat == nil {
		return a.LoadCachedCatalog()
	}
	return nil
}

func cmdEnable(ctx context.Context, a *core.App, f flags, on bool) int {
	_ = loadCatalogQuiet(a)
	verb := map[bool]string{true: "Enable", false: "Disable"}[on]
	header(verb)
	es, ok := findInstalled(a, f.args)
	if !ok || len(es) == 0 {
		if len(f.args) == 0 {
			errorf("Which mod?")
		}
		return 1
	}
	var names []string
	for _, e := range es {
		if !e.Managed {
			if e.Local == nil {
				continue
			}
			if err := a.SetLocalEnabled([]*core.Entry{e}, on); err != nil {
				errorf(err.Error())
				return 1
			}
			if on {
				success(ui.Bold.Render(e.Name) + " is on")
			} else {
				out("  %s %s is off", ui.Dim.Render(ui.GOff), ui.Bold.Render(e.Name))
			}
			continue
		}
		names = append(names, e.FullName)
		if !on {
			for _, d := range a.Inv.Dependents(e.FullName) {
				warnf(d.Name + " needs " + e.Name + " and may fail to load.")
			}
		}
	}
	if len(names) == 0 {
		fmt.Println()
		return 0
	}
	if err := a.SetEnabled(names, on); err != nil {
		errorf(err.Error())
		return 1
	}
	for _, n := range names {
		_, name := ts.SplitFullName(n)
		if on {
			success(ui.Bold.Render(name) + " is on")
		} else {
			out("  %s %s is off", ui.Dim.Render(ui.GOff), ui.Bold.Render(name))
		}
	}
	fmt.Println()
	return 0
}

func cmdList(ctx context.Context, a *core.App, f flags) int {
	f.offline = f.offline || !a.CatalogStale()
	if !loadCatalog(ctx, a, f) {
		_ = 0 // list still works without the catalog
	}
	header("Installed")
	b := a.Inv.BepInEx
	if b.Installed {
		out("  %s BepInEx %s %s", ui.OK.Render(ui.GOK), b.Version, ui.Dim.Render("· "+config.ShortPath(a.Game.Dir)))
	} else {
		out("  %s BepInEx is not installed", ui.Bad.Render(ui.GError))
	}
	fmt.Println()
	if len(a.Inv.Entries) == 0 {
		note("No mods installed. Try: ror2 search")
		return 0
	}
	updates, local := 0, 0
	for _, e := range a.Inv.Entries {
		if e.HasUpdate() {
			updates++
		}
		if !e.Managed {
			local++
		}
	}
	w := termWidth()
	nameW := 0
	for _, e := range a.Inv.Entries {
		nameW = max(nameW, lipgloss.Width(e.Name))
	}
	nameW = min(nameW, 30)
	verW := 0
	for _, e := range a.Inv.Entries {
		v := len(e.Version)
		if e.HasUpdate() {
			v += 3 + len(e.Latest())
		}
		verW = max(verW, v)
	}
	mods := 0
	for _, g := range a.Inv.Groups() {
		if g.Library && !f.all {
			var names []string
			for _, e := range g.Entries {
				n := e.Name
				if e.HasUpdate() {
					n += ui.Warn.Render(" " + ui.GUpdate)
				}
				names = append(names, n)
			}
			out("  %s %s", ui.Dim.Bold(true).Render(fmt.Sprintf("%s (%d)", g.Name, len(g.Entries))), ui.Fainter.Render("· ror2 list --all to expand"))
			out("%s", indent(ui.Dim.Render(ansi.Wordwrap(strings.Join(names, ", "), w-6, "")), 4))
			fmt.Println()
			continue
		}
		if !g.Library {
			mods += len(g.Entries)
		}
		head := ui.Accent
		if g.Library {
			head = ui.Dim.Bold(true)
		}
		out("  %s %s", head.Render(g.Name), ui.Fainter.Render(fmt.Sprintf("(%d)", len(g.Entries))))
		for _, e := range g.Entries {
			st := ui.OK.Render(ui.GOn)
			switch {
			case !e.Managed:
				st = ui.Info.Render(ui.GLocal)
			case !e.Enabled:
				st = ui.Dim.Render(ui.GOff)
			}
			ver := e.Version
			if ver == "" {
				ver = "?"
			}
			if e.HasUpdate() {
				ver = ui.Dim.Render(ver) + ui.Warn.Render(" "+ui.GUpdate+" "+e.Latest())
			} else {
				ver = ui.Dim.Render(ver)
			}
			name := lipgloss.NewStyle().Width(nameW).Render(ansiTrunc(e.Name, nameW))
			line := fmt.Sprintf("    %s %s  %s", st, ui.Bold.Render(name), lipgloss.NewStyle().Width(verW).Render(ver))
			if e.Pkg != nil && e.Pkg.Description != "" {
				room := w - lipgloss.Width(line) - 4
				if room > 15 {
					line += "  " + ui.Fainter.Render(ansiTrunc(e.Pkg.Description, room))
				}
			}
			out("%s", line)
		}
		fmt.Println()
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%d mods + %d libraries", mods, len(a.Inv.Entries)-mods))
	if updates > 0 {
		parts = append(parts, ui.Warn.Render(fmt.Sprintf("%d update(s)", updates)))
	}
	if local > 0 {
		parts = append(parts, ui.Info.Render(fmt.Sprintf("%d installed by hand", local)))
	}
	note(strings.Join(parts, ui.Dim.Render(" · ")))
	note(ui.OK.Render(ui.GOn) + ui.Dim.Render(" on  ") + ui.Dim.Render(ui.GOff+" off  ") + ui.Info.Render(ui.GLocal) + ui.Dim.Render(" installed by hand  ") + ui.Warn.Render(ui.GUpdate) + ui.Dim.Render(" update available"))
	if a.Cat == nil {
		note("Catalog not loaded; latest versions unknown.")
	}
	fmt.Println()
	return 0
}

func sevGlyph(s core.Severity) string {
	switch s {
	case core.SevError:
		return ui.Bad.Render(ui.GError)
	case core.SevWarn:
		return ui.Warn.Render(ui.GWarn)
	default:
		return ui.Info.Render(ui.GInfo)
	}
}

func cmdDoctor(ctx context.Context, a *core.App, f flags) int {
	if !loadCatalog(ctx, a, f) {
		return 1
	}
	header("Doctor")
	issues := a.Doctor()
	if a.Log != nil {
		note(fmt.Sprintf("BepInEx log from %s (%s), %d plugin(s) loaded", a.Log.Time.Format("Jan 2 15:04"), ui.Ago(a.Log.Time), len(a.Log.Loaded)))
		fmt.Println()
	}
	if len(issues) == 0 {
		success("Everything looks good.")
		fmt.Println()
		return 0
	}
	counts := map[core.Severity]int{}
	var fixes []*core.Fix
	for _, is := range issues {
		counts[is.Sev]++
		out("  %s %s", sevGlyph(is.Sev), ui.Bold.Render(is.Title))
		if is.Detail != "" {
			out("%s", indent(ui.Dim.Render(ansi.Wordwrap(is.Detail, termWidth()-8, "")), 4))
		}
		if is.Fix != nil {
			out("    %s %s", ui.Sel.Render(ui.GArrow), ui.Sel.Render(is.Fix.Label))
			if is.Sev > core.SevInfo || f.all {
				fixes = append(fixes, is.Fix)
			}
		}
		fmt.Println()
	}
	summary := []string{}
	if n := counts[core.SevError]; n > 0 {
		summary = append(summary, ui.Bad.Render(fmt.Sprintf("%d problem(s)", n)))
	}
	if n := counts[core.SevWarn]; n > 0 {
		summary = append(summary, ui.Warn.Render(fmt.Sprintf("%d warning(s)", n)))
	}
	if n := counts[core.SevInfo]; n > 0 {
		summary = append(summary, ui.Info.Render(fmt.Sprintf("%d note(s)", n)))
	}
	out("  %s", strings.Join(summary, ui.Dim.Render(" · ")))
	if !f.fix {
		if len(fixes) > 0 || counts[core.SevInfo] > 0 {
			note("Run `ror2 doctor --fix` to fix problems and warnings, add --all to include notes (adopting, cleanup).")
		}
		fmt.Println()
		if counts[core.SevError] > 0 {
			return 1
		}
		return 0
	}
	if len(fixes) == 0 {
		note("Nothing here can be fixed automatically.")
		fmt.Println()
		return 0
	}
	fmt.Println()
	batch, err := a.PlanFixes(fixes)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	out("  %s", ui.Bold.Render("Fix plan"))
	printPlan(batch.Plan)
	for _, n := range batch.Enable {
		out("  %s enable %s", ui.OK.Render(ui.GOn), n)
	}
	for _, n := range batch.Disable {
		out("  %s turn off %s", ui.Dim.Render(ui.GOff), n)
	}
	for _, n := range batch.Remove {
		out("  %s remove %s", ui.Bad.Render("−"), n)
	}
	for _, e := range batch.RemoveLocal {
		out("  %s remove hand-installed %s", ui.Bad.Render("−"), strings.Join(e.Local.Paths, ", "))
	}
	if batch.Empty() {
		success("Nothing to do.")
		return 0
	}
	if !confirm("Apply these fixes?", f.yes) {
		note("Cancelled.")
		return 1
	}
	fmt.Println()
	warnIfRunning()
	pp := &progressPrinter{}
	if err := a.ApplyFixes(ctx, batch, pp.event); err != nil {
		errorf("Stopped: " + err.Error())
		return 1
	}
	fmt.Println()
	left := 0
	for _, is := range a.Doctor() {
		if is.Sev == core.SevError {
			left++
		}
	}
	if left == 0 {
		success("Fixed. Launch the game to confirm everything loads (ror2 launch).")
	} else {
		warnf(fmt.Sprintf("%d problem(s) remain; run `ror2 doctor` again.", left))
	}
	fmt.Println()
	return 0
}

func cmdAdopt(ctx context.Context, a *core.App, f flags) int {
	if !loadCatalog(ctx, a, f) {
		return 1
	}
	header("Adopt hand-installed mods")
	var cands []*core.Entry
	var unknown []string
	for _, e := range a.Inv.Entries {
		if e.Managed {
			continue
		}
		if e.FullName == "" {
			unknown = append(unknown, e.Local.Name)
			continue
		}
		if m := a.Inv.Find(e.FullName); m != nil && m.Managed {
			continue // duplicate; doctor handles that
		}
		cands = append(cands, e)
	}
	if len(f.args) > 0 {
		sel, ok := findInstalled(a, f.args)
		if !ok {
			return 1
		}
		cands = sel
	}
	if len(cands) == 0 {
		success("No hand-installed mods to adopt.")
	}
	for _, e := range cands {
		conf := ui.OK.Render("sure")
		if e.Match != core.MatchStrong {
			conf = ui.Warn.Render("name only")
		}
		v := e.Version
		if v == "" {
			v = "?"
		}
		out("  %s %s %s %s  %s", ui.Info.Render(ui.GLocal), lipgloss.NewStyle().Width(22).Render(e.Local.Name), ui.Dim.Render(ui.GArrow), ui.Bold.Render(e.FullName)+" "+v, conf)
	}
	if len(unknown) > 0 {
		fmt.Println()
		note("Not on Thunderstore, left alone: " + strings.Join(unknown, ", "))
	}
	if len(cands) == 0 {
		fmt.Println()
		return 0
	}
	fmt.Println()
	plan, err := a.PlanAdopt(cands)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	return applyPlan(ctx, a, plan, "before-adopt", f.yes)
}

func cmdImport(ctx context.Context, a *core.App, f flags) int {
	if len(f.args) != 1 {
		errorf("Give one profile code or .r2z file, e.g. ror2 import 018c1a2b-…")
		return 2
	}
	if !loadCatalog(ctx, a, f) {
		return 1
	}
	header("Import profile")
	update, stop := spinner("Reading profile…")
	_ = update
	p, err := core.LoadProfile(ctx, f.args[0])
	stop()
	if err != nil {
		errorf(err.Error())
		return 1
	}
	out("  %s %s  %s", ui.Accent.Render(p.Name), ui.Dim.Render(fmt.Sprintf("%d mods", len(p.Mods))), ui.Dim.Render(fmt.Sprintf("%d config files", len(p.Configs))))
	fmt.Println()
	plan, err := a.PlanProfile(p)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	code := applyPlan(ctx, a, plan, "before-import", f.yes)
	if code != 0 {
		return code
	}
	n, err := a.ApplyProfileExtras(p, f.configs)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	if f.configs {
		success(fmt.Sprintf("Wrote %d config file(s) from the profile.", n))
	} else if len(p.Configs) > 0 {
		note("The profile has config files too; add --configs to copy them over yours.")
	}
	fmt.Println()
	return 0
}

func cmdLaunch(a *core.App) int {
	header("Launch")
	update, stop := spinner("Starting…")
	err := a.Launch(update)
	stop()
	if err != nil {
		errorf(err.Error())
		return 1
	}
	success("Steam was asked to start Risk of Rain 2.")
	note("BepInEx writes its log to " + config.ShortPath(a.Game.Dir) + "/BepInEx/LogOutput.log; run `ror2 doctor` after playing.")
	fmt.Println()
	return 0
}

func cmdRefresh(ctx context.Context, a *core.App) int {
	header("Refresh catalog")
	_ = a.LoadCachedCatalog()
	update, stop := spinner("Fetching the Thunderstore catalog…")
	err := a.RefreshCatalog(ctx, func(read, total int64) {
		if total > 0 {
			update(fmt.Sprintf("Fetching the Thunderstore catalog…  %s  %s", ui.Bar(float64(read)/float64(total), 20), ui.Dim.Render(ui.Bytes(read))))
		}
	})
	stop()
	if err != nil {
		errorf(err.Error())
		return 1
	}
	success(fmt.Sprintf("%d packages", len(a.Cat.Packages)))
	if n := len(a.Inv.Updates()); n > 0 {
		note(fmt.Sprintf("%d installed mod(s) have updates: ror2 update", n))
	}
	fmt.Println()
	return 0
}

func cmdBackups(a *core.App) int {
	header("Backups")
	list, err := game.ListBackups(a.Game.BackupRoot())
	if err != nil {
		errorf(err.Error())
		return 1
	}
	if len(list) == 0 {
		note("No backups yet. One is taken automatically before every change.")
		fmt.Println()
		return 0
	}
	t := newTable("Name", "Taken", "Size")
	for _, b := range list {
		t.Row(ui.Bold.Render(b.Name), ui.Dim.Render(ui.Ago(b.Time)), ui.Bytes(b.Size))
	}
	out("%s", indent(t.Render(), 2))
	note("Stored in " + config.ShortPath(a.Game.BackupRoot()))
	note("Copies are APFS clones, so they use little real disk space.")
	note("Restore with: ror2 restore <name>")
	fmt.Println()
	return 0
}

func cmdRestore(a *core.App, f flags) int {
	header("Restore")
	if len(f.args) != 1 {
		errorf("Which backup? See `ror2 backups`.")
		return 2
	}
	out("  This replaces BepInEx plugins, patchers, config and core with %s.", ui.Bold.Render(f.args[0]))
	if !confirm("Restore?", f.yes) {
		note("Cancelled.")
		return 1
	}
	safety, err := a.Game.Restore(a.Game.BackupRoot(), f.args[0], a.Cfg.KeepBackups)
	if err != nil {
		errorf(err.Error())
		return 1
	}
	success("Restored.")
	note("Your previous state was saved as " + config.ShortPath(safety))
	fmt.Println()
	return 0
}

func cmdConfig(f flags) int {
	cfg, _, err := config.Load()
	if err != nil {
		errorf(err.Error())
		return 1
	}
	if len(f.args) >= 1 && f.args[0] == "set" {
		if len(f.args) != 3 {
			errorf("Usage: ror2 config set <game-dir|wrapper|catalog-max-age|keep-backups|workspace|backdrop-image|backdrop-text> <value>")
			return 2
		}
		k, v := f.args[1], f.args[2]
		switch k {
		case "game-dir":
			if _, err := game.Open(v); err != nil {
				errorf(err.Error())
				return 1
			}
			cfg.GameDir = v
		case "wrapper":
			if _, err := os.Stat(config.WineBin(v)); err != nil {
				errorf("That does not look like a Sikarugir wrapper (no wine inside).")
				return 1
			}
			cfg.WrapperApp = v
		case "catalog-max-age":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				errorf("Give minutes, e.g. 60")
				return 1
			}
			cfg.CatalogMaxAgeMinutes = n
		case "workspace":
			if err := os.MkdirAll(v, 0o755); err != nil {
				errorf(err.Error())
				return 1
			}
			cfg.Set("workspace", v)
		case "backdrop-image", "backdrop-text":
			if _, err := os.Stat(v); err != nil {
				errorf(err.Error())
				return 1
			}
			abs, _ := filepath.Abs(v)
			if k == "backdrop-image" {
				cfg.Set("backdrop_image", abs)
				if b := cfg.Get("backdrop", "off"); b != "image" && b != "image-blocks" {
					cfg.Set("backdrop", "image")
				}
			} else {
				cfg.Set("backdrop_text", abs)
				cfg.Set("backdrop", "text")
			}
		case "keep-backups":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				errorf("Give a number, e.g. 8")
				return 1
			}
			cfg.KeepBackups = n
		default:
			errorf("Unknown setting " + k)
			return 2
		}
		if err := cfg.Save(); err != nil {
			errorf(err.Error())
			return 1
		}
		success("Saved.")
		return 0
	}
	header("Config")
	key := lipgloss.NewStyle().Foreground(ui.Equipment).Width(18)
	row := func(k, v string) { out("  %s %s", key.Render(k), v) }
	row("game-dir", config.ShortPath(cfg.GameDir))
	row("wrapper", config.ShortPath(cfg.WrapperApp))
	row("catalog-max-age", fmt.Sprintf("%d min", cfg.CatalogMaxAgeMinutes))
	row("keep-backups", fmt.Sprintf("%d", cfg.KeepBackups))
	fmt.Println()
	row("config file", config.ShortPath(config.Dir()+"/config.json"))
	row("cache", config.ShortPath(config.CacheDir()))
	if found := config.Detect(); len(found) > 1 {
		fmt.Println()
		note("Other installs found:")
		for _, i := range found[1:] {
			note("  " + config.ShortPath(i.Wrapper) + " → " + config.ShortPath(i.GameDir))
		}
	}
	fmt.Println()
	return 0
}
