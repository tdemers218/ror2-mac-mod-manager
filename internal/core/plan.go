package core

import (
	"context"
	"fmt"
	"strings"

	"ror2mm/internal/config"
	"ror2mm/internal/semver"
	ts "ror2mm/internal/thunderstore"
)

// StepKind is what a plan step does.
type StepKind int

const (
	StepInstall StepKind = iota
	StepUpdate
	StepAdopt     // replace a hand-installed copy with a tracked one
	StepReinstall // same version again
)

func (k StepKind) String() string {
	return [...]string{"install", "update", "adopt", "reinstall"}[k]
}

// Step is one package to put in place.
type Step struct {
	Kind     StepKind
	Pkg      *ts.Package
	Version  string
	From     string // previous version, if any
	Reason   string // "requested" or "needed by X"
	AsDep    bool
	Replaces []*Entry // hand-installed copies to remove first
}

// Plan is a list of steps plus notes.
type Plan struct {
	Steps    []*Step
	Skipped  []string // already fine
	Warnings []string
}

// Empty reports whether there is nothing to do.
func (p *Plan) Empty() bool { return len(p.Steps) == 0 }

// Request asks for a package, optionally at a set version.
type Request struct {
	FullName  string
	Version   string
	Reinstall bool
}

const ror2BepInExPack = "RiskofThunder-RoR2BepInExPack"

// isBepInExPack reports whether a dependency is the BepInEx loader itself, which
// is installed into the game root rather than managed as a mod.
func isBepInExPack(full string) bool {
	_, name := ts.SplitFullName(full)
	return strings.EqualFold(name, "BepInExPack")
}

type planner struct {
	a       *App
	plan    *Plan
	planned map[string]string
	visited map[string]bool
}

// PlanInstall works out everything needed to install the requests, including
// missing or too-old dependencies.
func (a *App) PlanInstall(reqs []Request) (*Plan, error) {
	if a.Cat == nil {
		return nil, fmt.Errorf("the Thunderstore catalog is not loaded")
	}
	pl := &planner{a: a, plan: &Plan{}, planned: map[string]string{}, visited: map[string]bool{}}
	for _, r := range reqs {
		pl.visit(r.FullName, "", "requested", true, r.Version, r.Reinstall)
	}
	return pl.plan, nil
}

func (pl *planner) visit(full, minVer, reason string, explicit bool, want string, reinstall bool) {
	key := strings.ToLower(full)
	inv := pl.a.Inv

	if isBepInExPack(full) {
		if !inv.BepInEx.Installed {
			pl.plan.Warnings = append(pl.plan.Warnings, "BepInEx is not installed in the game folder; mods will not load")
			return
		}
		// The Thunderstore BepInEx pack for RoR2 also ships RoR2BepInExPack, which
		// most mods silently rely on. Make sure it is present.
		if inv.Find(ror2BepInExPack) == nil {
			pl.visit(ror2BepInExPack, "", "part of the RoR2 BepInEx pack", false, "", false)
		}
		return
	}

	p := pl.a.Cat.Get(full)
	if p == nil {
		pl.plan.Warnings = append(pl.plan.Warnings, fmt.Sprintf("%s is not on Thunderstore (%s)", full, reason))
		return
	}
	if _, done := pl.planned[key]; done {
		return
	}
	if pl.visited[key] && !explicit {
		return
	}
	pl.visited[key] = true

	cur := inv.Find(p.FullName)
	target := ""
	if explicit {
		target = want
		if target == "" {
			target = p.Latest().Number
		}
		if p.Version(target) == nil {
			pl.plan.Warnings = append(pl.plan.Warnings, fmt.Sprintf("%s has no version %s; using %s", p.FullName, target, p.Latest().Number))
			target = p.Latest().Number
		}
		if cur != nil && cur.Managed && cur.Version == target && !reinstall {
			pl.plan.Skipped = append(pl.plan.Skipped, fmt.Sprintf("%s %s is already installed", p.Name, target))
			pl.a.Game.MarkExplicit(p.FullName)
			pl.deps(p, target)
			return
		}
	} else {
		if cur != nil && (cur.Version == "" || minVer == "" || !semver.Less(cur.Version, minVer)) {
			// Present and new enough. Still check its own dependencies.
			pl.deps(p, cur.Version)
			return
		}
		target = p.Latest().Number
	}

	s := &Step{Pkg: p, Version: target, Reason: reason, AsDep: !explicit}
	switch {
	case cur == nil:
		s.Kind = StepInstall
	case !cur.Managed:
		s.Kind = StepAdopt
		s.From = cur.Version
	case cur.Version == target:
		s.Kind = StepReinstall
		s.From = cur.Version
	default:
		s.Kind = StepUpdate
		s.From = cur.Version
	}
	if p.Deprecated {
		pl.plan.Warnings = append(pl.plan.Warnings, fmt.Sprintf("%s is marked deprecated on Thunderstore", p.FullName))
	}
	// Any hand-installed copies of this package get replaced.
	for _, e := range inv.Entries {
		if !e.Managed && strings.EqualFold(e.FullName, p.FullName) {
			s.Replaces = append(s.Replaces, e)
		}
	}
	pl.planned[key] = target
	pl.plan.Steps = append(pl.plan.Steps, s)
	pl.deps(p, target)
}

func (pl *planner) deps(p *ts.Package, version string) {
	v := p.Version(version)
	if v == nil {
		v = p.Latest()
	}
	for _, d := range v.Deps {
		full, min := ts.ParseDep(d)
		pl.visit(full, min, "needed by "+p.Name, false, "", false)
	}
}

// PlanUpdate plans updates for the named packages, or every tracked one if none
// are named. Dependencies are pulled in as needed.
func (a *App) PlanUpdate(fullNames []string) (*Plan, error) {
	var reqs []Request
	if len(fullNames) == 0 {
		for _, e := range a.Inv.Entries {
			if e.Managed && e.HasUpdate() {
				reqs = append(reqs, Request{FullName: e.FullName})
			}
		}
	} else {
		for _, n := range fullNames {
			reqs = append(reqs, Request{FullName: n})
		}
	}
	plan, err := a.PlanInstall(reqs)
	if err != nil {
		return nil, err
	}
	// An update should keep "installed as dependency" flags as they were.
	for _, s := range plan.Steps {
		if e := a.Inv.Find(s.Pkg.FullName); e != nil && e.Managed {
			s.AsDep = e.AsDependency
		}
	}
	return plan, nil
}

// PlanAdopt replaces identified hand-installed mods with tracked copies at the
// same version when known (latest otherwise).
func (a *App) PlanAdopt(entries []*Entry) (*Plan, error) {
	var reqs []Request
	for _, e := range entries {
		if e.Managed || e.Pkg == nil {
			continue
		}
		v := ""
		if e.Match == MatchStrong {
			v = e.Version
		}
		reqs = append(reqs, Request{FullName: e.FullName, Version: v})
	}
	plan, err := a.PlanInstall(reqs)
	if err != nil {
		return nil, err
	}
	for _, s := range plan.Steps {
		if s.Reason == "requested" && s.Kind == StepAdopt {
			s.Reason = "replaces hand-installed copy"
		}
	}
	return plan, nil
}

// Event reports progress while a plan runs.
type Event struct {
	Index, Total int
	Step         *Step
	Phase        string // "download", "install", "done"
	Read, Size   int64
	Err          error
}

// Apply runs a plan: backup, download, remove replaced copies, install.
// It stops at the first error; everything before it stays installed.
func (a *App) Apply(ctx context.Context, plan *Plan, label string, progress func(Event)) error {
	if plan.Empty() {
		return nil
	}
	if progress == nil {
		progress = func(Event) {}
	}
	if _, err := a.EnsureBackup(label); err != nil {
		return fmt.Errorf("could not back up BepInEx first, nothing was changed: %w", err)
	}
	defer a.Refresh()
	total := len(plan.Steps)
	for i, s := range plan.Steps {
		ev := Event{Index: i, Total: total, Step: s}
		ev.Phase = "download"
		progress(ev)
		zip, err := ts.Download(ctx, config.CacheDir(), s.Pkg, s.Version, func(read, size int64) {
			e := ev
			e.Read, e.Size = read, size
			progress(e)
		})
		if err != nil {
			ev.Err = err
			progress(ev)
			return err
		}
		ev.Phase = "install"
		progress(ev)
		var local []string
		for _, r := range s.Replaces {
			local = append(local, r.Local.Paths...)
		}
		if len(local) > 0 {
			if err := a.Game.RemoveLocal(local); err != nil {
				ev.Err = err
				progress(ev)
				return err
			}
		}
		if err := a.Game.InstallZip(s.Pkg.FullName, s.Version, zip, s.AsDep); err != nil {
			ev.Err = err
			progress(ev)
			return err
		}
		ev.Phase = "done"
		progress(ev)
	}
	return nil
}

// Remove uninstalls tracked packages (backing up first).
func (a *App) Remove(fullNames []string) error {
	if _, err := a.EnsureBackup("before-remove"); err != nil {
		return err
	}
	defer a.Refresh()
	for _, n := range fullNames {
		if err := a.Game.Uninstall(n); err != nil {
			return err
		}
	}
	return nil
}

// RemoveLocalEntries deletes hand-installed mods (backing up first).
func (a *App) RemoveLocalEntries(entries []*Entry) error {
	if _, err := a.EnsureBackup("before-remove"); err != nil {
		return err
	}
	defer a.Refresh()
	for _, e := range entries {
		if e.Local == nil {
			continue
		}
		if err := a.Game.RemoveLocal(e.Local.Paths); err != nil {
			return err
		}
	}
	return nil
}

// SetEnabled turns tracked packages on or off.
func (a *App) SetEnabled(fullNames []string, on bool) error {
	defer a.Refresh()
	for _, n := range fullNames {
		if err := a.Game.SetEnabled(n, on); err != nil {
			return err
		}
	}
	return nil
}

// SetLocalEnabled turns hand-installed (and dev) mods on or off by moving
// their files aside.
func (a *App) SetLocalEnabled(entries []*Entry, on bool) error {
	defer a.Refresh()
	for _, e := range entries {
		if e.Local == nil {
			continue
		}
		if err := a.Game.SetLocalEnabled(*e.Local, on); err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
	}
	return nil
}

// Orphans lists tracked packages installed only as dependencies that nothing needs now.
func (a *App) Orphans() []*Entry {
	var out []*Entry
	for _, e := range a.Inv.Entries {
		// Count turned-off mods too: their dependencies are still needed.
		if e.Managed && e.AsDependency && len(a.Inv.neededBy(e)) == 0 {
			if e.FullName == ror2BepInExPack {
				continue // needed by the loader setup, not by a mod
			}
			out = append(out, e)
		}
	}
	return out
}
