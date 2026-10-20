package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"ror2mm/internal/game"
	"ror2mm/internal/semver"
	ts "ror2mm/internal/thunderstore"
)

// Severity of a doctor finding.
type Severity int

const (
	SevInfo Severity = iota
	SevWarn
	SevError
)

// Fix is what the doctor can do about an issue.
type Fix struct {
	Label       string
	Install     []Request
	Adopt       []*Entry
	Enable      []string
	Disable     []string
	Remove      []string
	RemoveLocal []*Entry
}

// Issue is one doctor finding.
type Issue struct {
	Sev    Severity
	Title  string
	Detail string
	Fix    *Fix
	key    string
}

// Doctor checks the install for missing dependencies, failed loads, and more.
func (a *App) Doctor() []*Issue {
	var issues []*Issue
	byKey := map[string]*Issue{}
	add := func(is *Issue) {
		if is.key != "" {
			if prev, ok := byKey[is.key]; ok {
				if prev.Detail != "" && is.Detail != "" && !strings.Contains(prev.Detail, is.Detail) {
					prev.Detail += "\n" + is.Detail
				}
				if is.Sev > prev.Sev {
					prev.Sev = is.Sev
				}
				return
			}
			byKey[is.key] = is
		}
		issues = append(issues, is)
	}
	inv := a.Inv

	// 1. Loader itself.
	b := inv.BepInEx
	if !b.Installed {
		add(&Issue{Sev: SevError, Title: "BepInEx is not installed",
			Detail: "Mods cannot load without it. Missing: " + strings.Join(b.MissingPieces, ", ")})
	} else {
		if !b.DoorstopDLL {
			add(&Issue{Sev: SevError, Title: "winhttp.dll is missing from the game folder",
				Detail: "This is BepInEx's doorstop; without it the game starts without mods."})
		}
		if !b.DoorstopOn {
			add(&Issue{Sev: SevWarn, Title: "Doorstop is turned off",
				Detail: "doorstop_config.ini has enabled = false, so BepInEx will not start."})
		}
	}

	// 2. RoR2BepInExPack (comes with the official pack; manual installs often miss it).
	if b.Installed && inv.Find(ror2BepInExPack) == nil && a.Cat != nil && a.Cat.Get(ror2BepInExPack) != nil {
		add(&Issue{Sev: SevError, key: "missing:" + strings.ToLower(ror2BepInExPack),
			Title:  "RoR2BepInExPack is missing",
			Detail: "It is part of the standard BepInEx pack for Risk of Rain 2. R2API and most larger mods need it.",
			Fix:    &Fix{Label: "Install RoR2BepInExPack", Install: []Request{{FullName: ror2BepInExPack}}}})
	}

	// 3. Declared dependencies of everything installed.
	type need struct {
		by  []string
		min string
	}
	missing := map[string]*need{}
	var order []string
	for _, e := range inv.Entries {
		if !e.Enabled {
			continue
		}
		for _, d := range e.Deps() {
			full, min := ts.ParseDep(d)
			if isBepInExPack(full) {
				continue
			}
			dep := inv.Find(full)
			switch {
			case dep == nil:
				k := strings.ToLower(full)
				if missing[k] == nil {
					missing[k] = &need{}
					order = append(order, full)
				}
				missing[k].by = append(missing[k].by, e.Name)
				if missing[k].min == "" || semver.Less(missing[k].min, min) {
					missing[k].min = min
				}
			case dep.Version != "" && min != "" && semver.Less(dep.Version, min):
				fix := &Fix{Label: "Update " + dep.Name, Install: []Request{{FullName: dep.FullName}}}
				if !dep.Managed {
					fix.Label = "Replace " + dep.Name + " with the latest version"
				}
				add(&Issue{Sev: SevWarn, key: "old:" + strings.ToLower(dep.FullName),
					Title:  fmt.Sprintf("%s is too old for %s", dep.Name, e.Name),
					Detail: fmt.Sprintf("%s needs %s %s or newer; %s is installed.", e.Name, dep.Name, min, dep.Version),
					Fix:    fix})
			case !dep.Enabled:
				add(&Issue{Sev: SevWarn, key: "disabled:" + strings.ToLower(dep.FullName),
					Title:  fmt.Sprintf("%s is disabled but %s needs it", dep.Name, e.Name),
					Detail: "Mods that depend on a disabled mod usually fail to load.",
					Fix:    &Fix{Label: "Enable " + dep.Name, Enable: []string{dep.FullName}}})
			}
		}
	}
	for _, full := range order {
		n := missing[strings.ToLower(full)]
		_, name := ts.SplitFullName(full)
		is := &Issue{Sev: SevError, key: "missing:" + strings.ToLower(full),
			Title:  name + " is missing",
			Detail: "Needed by " + strings.Join(uniq(n.by), ", ") + "."}
		if a.Cat != nil && a.Cat.Get(full) != nil {
			is.Fix = &Fix{Label: "Install " + name, Install: []Request{{FullName: full}}}
		} else {
			is.Detail += " It is not on Thunderstore, so it has to be installed by hand."
		}
		add(is)
	}

	// 4. What BepInEx itself complained about on the last launch.
	if a.Log != nil {
		when := "on the last launch (" + a.Log.Time.Format("Jan 2, 15:04") + ")"
		for _, f := range a.Log.Failures {
			if len(f.Missing) == 0 {
				add(&Issue{Sev: SevWarn, key: "fail:" + strings.ToLower(f.Plugin.Name),
					Title:  f.Plugin.Name + " failed to load " + when,
					Detail: "BepInEx said: because " + f.Reason})
				continue
			}
			for _, guid := range f.Missing {
				p := a.GuidToPackage(guid)
				if p == nil {
					add(&Issue{Sev: SevError, key: "guid:" + strings.ToLower(guid),
						Title:  f.Plugin.Name + " could not load " + when,
						Detail: "It needs a plugin with ID \"" + guid + "\", which could not be matched to a Thunderstore package."})
					continue
				}
				if inv.Find(p.FullName) != nil {
					continue // installed since then
				}
				add(&Issue{Sev: SevError, key: "missing:" + strings.ToLower(p.FullName),
					Title:  p.Name + " is missing",
					Detail: f.Plugin.Name + " refused to load " + when + " because it needs " + guid + ".",
					Fix:    &Fix{Label: "Install " + p.Name, Install: []Request{{FullName: p.FullName}}}})
			}
		}
	}

	// 4a. A mod's code references an assembly that is not installed.
	if a.Log != nil {
		for _, ma := range a.Log.MissingAssemblies {
			modName := ma.Mod
			for _, e := range inv.Entries {
				for _, d := range e.DLLs {
					if strings.EqualFold(d, ma.Mod) {
						modName = e.Name
					}
				}
			}
			p := a.assemblyToPackage(ma.Missing)
			if p != nil && inv.Find(p.FullName) != nil {
				continue // installed since that launch
			}
			is := &Issue{Sev: SevWarn, key: "asm:" + strings.ToLower(ma.Missing),
				Title: modName + " is missing " + ma.Missing,
				Detail: modName + " uses " + ma.Missing + " but it is not installed, so the game could not read all of " + modName +
					"'s code on the last launch. Optional integrations usually fail quietly, but parts of the mod can stop working " +
					"(for example custom interactables or chests)."}
			if p != nil {
				is.Fix = &Fix{Label: "Install " + p.Name, Install: []Request{{FullName: p.FullName}}}
			}
			add(is)
		}
	}

	// 4b. Exceptions while the game loads its content hang the loading screen.
	if a.Log != nil {
		seen := map[string]bool{}
		for _, c := range a.Log.Crashes {
			if !c.DuringLoad {
				continue
			}
			mods := a.implicated(c)
			key := c.Message
			if len(mods) > 0 {
				key = mods[0].Name + "|" + c.Message
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			var names []string
			for _, e := range mods {
				names = append(names, e.Name)
			}
			msg := c.Message
			if len(msg) > 160 {
				msg = msg[:157] + "..."
			}
			is := &Issue{Sev: SevError, key: "crash:" + key,
				Title: "The game crashed while loading its content",
				Detail: "This stops the game on the loading screen (it freezes partway, often around 50-60%). " +
					"Error on the last launch: " + msg + "."}
			if len(names) > 0 {
				is.Detail += " The stack trace runs through " + strings.Join(names, ", ") + "."
			}
			if culprit := a.culprit(mods); culprit != nil {
				is.Title = culprit.Name + " crashed while the game was loading"
				is.Detail += " Turn " + culprit.Name + " off and launch again; if the game then loads, it is the cause " +
					"(check for an update, or tell its author along with BepInEx/LogOutput.log)."
				if culprit.Managed && culprit.Enabled {
					is.Fix = &Fix{Label: "Turn off " + culprit.Name, Disable: []string{culprit.FullName}}
				}
			}
			add(is)
		}
	}

	// 5. Duplicates and hand-installed mods.
	for _, e := range inv.Entries {
		if e.Managed || e.FullName == "" {
			continue
		}
		if m := inv.Find(e.FullName); m != nil && m != e && m.Managed {
			add(&Issue{Sev: SevWarn, Title: e.Name + " is installed twice",
				Detail: "A hand-installed copy (" + strings.Join(e.Local.Paths, ", ") + ") sits next to the tracked one. BepInEx will load only one of them.",
				Fix:    &Fix{Label: "Remove the hand-installed copy", RemoveLocal: []*Entry{e}}})
			continue
		}
		v := e.Version
		if v == "" {
			v = "unknown version"
		}
		detail := fmt.Sprintf("Looks like %s %s (%s). Adopting reinstalls it from Thunderstore so ror2 can update it.", e.FullName, v, matchWord(e.Match))
		add(&Issue{Sev: SevInfo, Title: e.Local.Name + " was installed by hand",
			Detail: detail, Fix: &Fix{Label: "Adopt " + e.Name, Adopt: []*Entry{e}}})
	}

	// 6. Deprecated packages.
	for _, e := range inv.Entries {
		if e.Pkg != nil && e.Pkg.Deprecated {
			add(&Issue{Sev: SevWarn, Title: e.Name + " is deprecated",
				Detail: "The author marked it deprecated on Thunderstore. It may stop working after game updates."})
		}
	}

	// 7. Dependencies nothing needs any more.
	for _, e := range a.Orphans() {
		add(&Issue{Sev: SevInfo, Title: e.Name + " is no longer needed",
			Detail: "It was installed as a dependency, but no installed mod uses it now.",
			Fix:    &Fix{Label: "Remove " + e.Name, Remove: []string{e.FullName}}})
	}

	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Sev > issues[j].Sev })
	return issues
}

// assemblyToPackage guesses the Thunderstore package that ships an assembly.
func (a *App) assemblyToPackage(asm string) *ts.Package {
	if a.Cat == nil {
		return nil
	}
	var best *ts.Package
	for _, n := range []string{asm, strings.SplitN(asm, ".", 2)[0]} {
		for _, p := range a.Cat.ByNorm(ts.Normalize(n)) {
			if best == nil || (best.Deprecated && !p.Deprecated) || (best.Deprecated == p.Deprecated && p.Downloads > best.Downloads) {
				best = p
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

// implicated lists installed mods that appear in a crash's stack trace,
// innermost first.
func (a *App) implicated(c game.Crash) []*Entry {
	var out []*Entry
	seen := map[*Entry]bool{}
	for _, fr := range c.Frames {
		root := fr
		if i := strings.IndexAny(root, ".+ ("); i > 0 {
			root = root[:i]
		}
		switch root {
		case "RoR2", "UnityEngine", "HG", "System", "Mono", "MonoMod", "EntityStates", "Unity", "BepInEx", "DMD", "Rewired", "TMPro":
			// Game and loader code: look for mod names in the rest of the line only.
			root = ""
		}
		nr := ts.Normalize(root)
		nfr := ts.Normalize(fr)
		for _, e := range a.Inv.Entries {
			if seen[e] {
				continue
			}
			hit := false
			for _, d := range e.DLLs {
				nd := ts.Normalize(d)
				first := ts.Normalize(strings.SplitN(d, ".", 2)[0])
				if nr != "" && (nr == nd || nr == first) {
					hit = true
				}
			}
			if n := ts.Normalize(e.Name); len(n) >= 5 && strings.Contains(nfr, n) {
				hit = true
			}
			if hit {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	return out
}

// culprit picks the mod to turn off: the one that depends on the others in
// the trace (the outermost mod), since libraries usually fail on its behalf.
func (a *App) culprit(mods []*Entry) *Entry {
	if len(mods) == 0 {
		return nil
	}
	for _, e := range mods {
		dependsOnOthers := false
		for _, d := range e.Deps() {
			full, _ := ts.ParseDep(d)
			for _, o := range mods {
				if o != e && strings.EqualFold(o.FullName, full) {
					dependsOnOthers = true
				}
			}
		}
		if dependsOnOthers {
			return e
		}
	}
	return mods[len(mods)-1]
}

func matchWord(c Confidence) string {
	switch c {
	case MatchStrong:
		return "name and version match"
	case MatchName:
		return "name matches, version unconfirmed"
	}
	return "no match"
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// GuidToPackage guesses the package that provides a BepInEx plugin ID.
func (a *App) GuidToPackage(guid string) *ts.Package {
	if a.Cat == nil {
		return nil
	}
	g := strings.ToLower(strings.TrimLeft(guid, "_"))
	// R2API modules: com.bepis.r2api.items -> RiskofThunder-R2API_Items.
	if strings.HasPrefix(g, "com.bepis.r2api") {
		sub := strings.TrimPrefix(strings.TrimPrefix(g, "com.bepis.r2api"), ".")
		if sub == "" {
			sub = "core"
		}
		for _, p := range a.Cat.ByNorm("r2api" + ts.Normalize(sub)) {
			if strings.EqualFold(p.Owner, "RiskofThunder") {
				return p
			}
		}
	}
	segs := strings.Split(g, ".")
	try := []string{ts.Normalize(segs[len(segs)-1])}
	if len(segs) >= 2 {
		try = append(try, ts.Normalize(segs[len(segs)-2]+segs[len(segs)-1]))
	}
	for _, n := range try {
		cands := a.Cat.ByNorm(n)
		var best *ts.Package
		for _, p := range cands {
			// Prefer a package whose owner appears in the GUID (com.rune580.riskofoptions).
			if strings.Contains(g, strings.ToLower(p.Owner)) && !p.Deprecated {
				return p
			}
			if best == nil || (best.Deprecated && !p.Deprecated) || (best.Deprecated == p.Deprecated && p.Downloads > best.Downloads) {
				best = p
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}

// FixBatch is everything needed to apply a set of fixes.
type FixBatch struct {
	Plan        *Plan
	Enable      []string
	Disable     []string
	Remove      []string
	RemoveLocal []*Entry
}

// Empty reports whether the batch does nothing.
func (b *FixBatch) Empty() bool {
	return b.Plan.Empty() && len(b.Enable) == 0 && len(b.Disable) == 0 && len(b.Remove) == 0 && len(b.RemoveLocal) == 0
}

// PlanFixes merges fixes into one batch.
func (a *App) PlanFixes(fixes []*Fix) (*FixBatch, error) {
	b := &FixBatch{}
	var reqs []Request
	adopted := map[string]bool{}
	for _, f := range fixes {
		reqs = append(reqs, f.Install...)
		b.Enable = append(b.Enable, f.Enable...)
		b.Disable = append(b.Disable, f.Disable...)
		b.Remove = append(b.Remove, f.Remove...)
		b.RemoveLocal = append(b.RemoveLocal, f.RemoveLocal...)
		for _, e := range f.Adopt {
			v := ""
			if e.Match == MatchStrong {
				v = e.Version
			}
			reqs = append(reqs, Request{FullName: e.FullName, Version: v})
			adopted[strings.ToLower(e.FullName)] = true
		}
	}
	plan, err := a.PlanInstall(reqs)
	if err != nil {
		return nil, err
	}
	for _, s := range plan.Steps {
		switch {
		case adopted[strings.ToLower(s.Pkg.FullName)]:
			s.Reason = "replaces hand-installed copy"
		case s.Reason == "requested":
			// Doctor installs are dependencies, not things the player picked.
			s.AsDep = true
			if s.Kind == StepInstall {
				s.Reason = "fixes a missing dependency"
			} else {
				s.Reason = "too old for a mod that needs it"
			}
		}
	}
	b.Plan = plan
	return b, nil
}

// ApplyFixes runs a fix batch.
func (a *App) ApplyFixes(ctx context.Context, b *FixBatch, progress func(Event)) error {
	if _, err := a.EnsureBackup("before-doctor"); err != nil {
		return err
	}
	if err := a.Apply(ctx, b.Plan, "before-doctor", progress); err != nil {
		return err
	}
	if len(b.Enable) > 0 {
		if err := a.SetEnabled(b.Enable, true); err != nil {
			return err
		}
	}
	if len(b.Disable) > 0 {
		if err := a.SetEnabled(b.Disable, false); err != nil {
			return err
		}
	}
	if len(b.RemoveLocal) > 0 {
		if err := a.RemoveLocalEntries(b.RemoveLocal); err != nil {
			return err
		}
	}
	if len(b.Remove) > 0 {
		if err := a.Remove(b.Remove); err != nil {
			return err
		}
	}
	a.Refresh()
	return nil
}
