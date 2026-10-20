package cli

import (
	"context"
	"fmt"
	"strings"

	"ror2mm/internal/core"
	"ror2mm/internal/ui"
)

// cmdPack handles `ror2 pack list|save|load|rename|delete`.
func cmdPack(ctx context.Context, a *core.App, f flags) int {
	sub := "list"
	args := f.args
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	name := strings.Join(args, " ")
	switch sub {
	case "list", "ls":
		header("Mod packs")
		active, matches := a.ActivePack()
		for _, p := range a.Packs() {
			mark, st := ui.Dim.Render(ui.GOff), ui.Bold
			if active != nil && active.Name == p.Name {
				mark = ui.OK.Render(ui.GOn)
				if !matches {
					mark = ui.Warn.Render(ui.GOn)
				}
			}
			about := fmt.Sprintf("%d mods", len(p.Mods))
			if p.Vanilla {
				about = "no mods: the plain game"
			} else if p.Settings {
				about += " + settings"
			}
			out("  %s %s  %s", mark, st.Render(p.Name), ui.Dim.Render(about))
		}
		if active != nil && !matches {
			fmt.Println()
			note("Mods changed since " + active.Name + " was loaded. `ror2 pack save " + active.Name + "` updates it.")
		}
		fmt.Println()
		note("ror2 pack save <name> [--configs]   ror2 pack load <name>   ror2 pack rename <old> <new>   ror2 pack delete <name>")
		fmt.Println()
		return 0
	case "save":
		header("Save pack")
		if msg := core.ValidPackName(name); msg != "" {
			errorf(msg)
			return 2
		}
		if old := a.FindPack(name); old != nil && !confirm("Replace the pack "+old.Name+" with the mods that are on now?", f.yes) {
			note("Cancelled.")
			return 1
		}
		p, err := a.SavePack(name, f.configs)
		if err != nil {
			errorf(err.Error())
			return 1
		}
		extra := ""
		if p.Settings {
			extra = " and their settings"
		}
		success(fmt.Sprintf("Saved %s: %d mods%s", ui.Bold.Render(p.Name), len(p.Mods), extra))
		fmt.Println()
		return 0
	case "load", "use", "apply":
		header("Load pack")
		p := a.FindPack(name)
		if p == nil {
			errorf(fmt.Sprintf("No pack called %q. See `ror2 pack list`.", name))
			return 1
		}
		_ = loadCatalogQuiet(a)
		pp, err := a.PlanPack(p)
		if err != nil {
			errorf(err.Error())
			return 1
		}
		printPackPlan(pp)
		if pp.Empty() {
			success("Already set up as " + p.Name + ".")
			fmt.Println()
			return 0
		}
		if !confirm("Load "+p.Name+"?", f.yes) {
			note("Cancelled.")
			return 1
		}
		warnIfRunning()
		prog := &progressPrinter{}
		if err := a.ApplyPack(ctx, pp, prog.event); err != nil {
			errorf("Stopped: " + err.Error())
			note("A backup was taken first; see `ror2 backups`.")
			return 1
		}
		success("Loaded " + ui.Bold.Render(p.Name) + ". Start the game with `ror2 launch`.")
		fmt.Println()
		return 0
	case "rename", "mv":
		header("Rename pack")
		if len(args) < 2 {
			errorf("Usage: ror2 pack rename <old name> <new name>  (quote names with spaces)")
			return 2
		}
		if err := a.RenamePack(args[0], strings.Join(args[1:], " ")); err != nil {
			errorf(err.Error())
			return 1
		}
		success("Renamed.")
		fmt.Println()
		return 0
	case "delete", "rm", "remove":
		header("Delete pack")
		if !confirm("Delete the pack "+name+"? Your mods stay installed.", f.yes) {
			note("Cancelled.")
			return 1
		}
		if err := a.DeletePack(name); err != nil {
			errorf(err.Error())
			return 1
		}
		success("Deleted " + name + ".")
		fmt.Println()
		return 0
	}
	errorf("Unknown pack command " + sub + ". Use list, save, load, rename or delete.")
	return 2
}

func printPackPlan(pp *core.PackPlan) {
	if pp.Pack.Vanilla {
		out("  %s", ui.Bold.Render("Vanilla: the game starts with no mods."))
		note("Mods are not moved or removed, so loading another pack later is instant.")
		fmt.Println()
		return
	}
	list := func(title string, es []*core.Entry, glyph string) {
		if len(es) == 0 {
			return
		}
		out("  %s", ui.Bold.Render(title))
		for _, e := range es {
			out("    %s %s", glyph, e.Name)
		}
	}
	list(fmt.Sprintf("Switch on (%d)", len(pp.Enable)), pp.Enable, ui.OK.Render(ui.GOn))
	list(fmt.Sprintf("Switch off (%d)", len(pp.Disable)), pp.Disable, ui.Dim.Render(ui.GOff))
	if pp.Install != nil && !pp.Install.Empty() {
		out("  %s", ui.Bold.Render(fmt.Sprintf("Download again (%d)", len(pp.Install.Steps))))
		for _, s := range pp.Install.Steps {
			out("    %s %s %s", ui.Info.Render(ui.GDownload), s.Pkg.Name, ui.Dim.Render(s.Version))
		}
	}
	for _, m := range pp.Missing {
		warnf(m + " was installed by hand and is gone, so it can't be brought back.")
	}
	if pp.Settings {
		note("The pack's saved mod settings will be put back.")
	}
	if pp.LoaderChange {
		note("Mods will load again (the loader was off).")
	}
	fmt.Println()
}
