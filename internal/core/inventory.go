package core

import (
	"sort"
	"strings"

	"ror2mm/internal/game"
	"ror2mm/internal/semver"
	ts "ror2mm/internal/thunderstore"
)

// Confidence says how sure we are that a hand-installed mod is a given package.
type Confidence int

const (
	MatchNone   Confidence = iota
	MatchName              // name matches but version could not be confirmed
	MatchStrong            // name and version both match
)

// Entry is one installed mod, managed or not.
type Entry struct {
	Managed      bool
	FullName     string // "" for local mods we could not identify
	Name         string
	Owner        string
	Version      string // "" if unknown
	Enabled      bool
	AsDependency bool
	Pkg          *ts.Package
	Local        *game.Local
	Match        Confidence
	Dev          bool // your own mod, deployed from a project with `ror2 dev deploy`
	DLLs         []string
}

// Latest is the newest version on Thunderstore, or "".
func (e *Entry) Latest() string {
	if e.Pkg == nil || e.Pkg.Latest() == nil {
		return ""
	}
	return e.Pkg.Latest().Number
}

// HasUpdate reports whether a newer version exists.
func (e *Entry) HasUpdate() bool {
	l := e.Latest()
	return l != "" && e.Version != "" && semver.Less(e.Version, l)
}

// Deps returns the dependency strings for the installed version.
func (e *Entry) Deps() []string {
	if e.Pkg != nil {
		if v := e.Pkg.Version(e.Version); v != nil {
			return v.Deps
		}
		if e.Version == "" && e.Pkg.Latest() != nil {
			return e.Pkg.Latest().Deps
		}
	}
	if e.Local != nil && e.Local.Manifest != nil {
		return e.Local.Manifest.Dependencies
	}
	return nil
}

// Inventory is everything installed.
type Inventory struct {
	Entries []*Entry
	BepInEx game.BepInEx
	byFull  map[string]*Entry
}

// Find returns the installed entry for a package. Managed copies win over local ones.
func (inv *Inventory) Find(fullName string) *Entry {
	return inv.byFull[strings.ToLower(fullName)]
}

// Managed returns only the entries ror2 installed.
func (inv *Inventory) Managed() []*Entry {
	var out []*Entry
	for _, e := range inv.Entries {
		if e.Managed {
			out = append(out, e)
		}
	}
	return out
}

// Updates returns entries with a newer version available.
func (inv *Inventory) Updates() []*Entry {
	var out []*Entry
	for _, e := range inv.Entries {
		if e.HasUpdate() {
			out = append(out, e)
		}
	}
	return out
}

// Dependents lists enabled entries that depend on fullName.
func (inv *Inventory) Dependents(fullName string) []*Entry {
	var out []*Entry
	for _, e := range inv.Entries {
		if !e.Enabled {
			continue
		}
		for _, d := range e.Deps() {
			if full, _ := ts.ParseDep(d); strings.EqualFold(full, fullName) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// BuildInventory combines the manager's records, a scan of the folder and the catalog.
func BuildInventory(cat *ts.Catalog, g *game.Game, log *game.Log) *Inventory {
	inv := &Inventory{byFull: map[string]*Entry{}, BepInEx: g.BepInExStatus()}
	for _, p := range g.Packages() {
		owner, name := ts.SplitFullName(p.FullName)
		e := &Entry{Managed: true, FullName: p.FullName, Name: name, Owner: owner, Version: p.Version,
			Enabled: p.Enabled, AsDependency: p.AsDependency, Match: MatchStrong}
		for _, f := range p.Files {
			if strings.HasSuffix(strings.ToLower(f), ".dll") {
				base := f[strings.LastIndex(f, "/")+1:]
				e.DLLs = append(e.DLLs, strings.TrimSuffix(base, base[len(base)-4:]))
			}
		}
		if cat != nil {
			e.Pkg = cat.Get(p.FullName)
		}
		inv.Entries = append(inv.Entries, e)
		inv.byFull[strings.ToLower(p.FullName)] = e
	}
	for _, l := range g.ScanLocal() {
		l := l
		e := &Entry{Name: l.Name, Local: &l, Enabled: true, DLLs: l.DLLs}
		identify(e, cat, log)
		inv.Entries = append(inv.Entries, e)
		if e.FullName != "" {
			if _, taken := inv.byFull[strings.ToLower(e.FullName)]; !taken {
				inv.byFull[strings.ToLower(e.FullName)] = e
			}
		}
	}
	sort.SliceStable(inv.Entries, func(i, j int) bool {
		return strings.ToLower(inv.Entries[i].Name) < strings.ToLower(inv.Entries[j].Name)
	})
	return inv
}

// identify guesses which Thunderstore package a hand-installed mod is.
func identify(e *Entry, cat *ts.Catalog, log *game.Log) {
	l := e.Local
	// dev-<Author>-<Name>: a mod you are making; never match it to Thunderstore.
	if strings.HasPrefix(l.Name, "dev-") {
		e.Dev = true
		parts := strings.SplitN(strings.TrimPrefix(l.Name, "dev-"), "-", 2)
		if len(parts) == 2 {
			e.Owner, e.Name = parts[0], parts[1]
		}
		if l.Manifest != nil {
			e.Version = l.Manifest.Version
		} else {
			e.Version = l.FileVersion
		}
		return
	}
	names := []string{l.Name}
	if l.Manifest != nil {
		names = append([]string{l.Manifest.Name}, names...)
	}
	names = append(names, l.DLLs...)
	// "BepInEx.MonoMod.HookGenPatcher" -> also try "HookGenPatcher".
	for _, n := range append([]string(nil), names...) {
		if i := strings.LastIndex(n, "."); i >= 0 && len(n)-i > 4 {
			names = append(names, n[i+1:])
		}
	}
	norms := map[string]bool{}
	for _, n := range names {
		if v := ts.Normalize(n); len(v) >= 3 {
			norms[v] = true
		}
	}

	// Versions seen for this mod: manifest, BepInEx log, DLL file version.
	var claimed []string
	if l.Manifest != nil && l.Manifest.Version != "" {
		claimed = append(claimed, l.Manifest.Version)
	}
	if log != nil {
		check := func(p game.Plugin) {
			if norms[ts.Normalize(p.Name)] && p.Version != "" {
				claimed = append(claimed, p.Version)
			}
		}
		for _, p := range log.Loaded {
			check(p)
		}
		for _, f := range log.Failures {
			check(f.Plugin)
		}
	}
	if len(claimed) > 0 {
		e.Version = claimed[0]
	} else if l.FileVersion != "" {
		e.Version = l.FileVersion
	}
	if l.Manifest != nil {
		e.Name = l.Manifest.Name
	}
	if cat == nil {
		return
	}

	// If the folder is named Owner-Name, trust it.
	if p := cat.Get(l.Name); p != nil {
		e.Pkg, e.FullName, e.Owner, e.Name, e.Match = p, p.FullName, p.Owner, p.Name, MatchStrong
		return
	}

	type cand struct {
		p      *ts.Package
		score  int
		strong bool
		ver    string
	}
	var best *cand
	consider := func(p *ts.Package, nameScore int) {
		c := cand{p: p, score: nameScore}
		for _, v := range claimed {
			if p.Version(v) != nil {
				c.score += 100
				c.strong, c.ver = true, v
				break
			}
		}
		if !c.strong && l.FileVersion != "" && p.Version(l.FileVersion) != nil {
			c.score += 60
			c.strong, c.ver = true, l.FileVersion
		}
		if p.Deprecated {
			c.score -= 40
		}
		if best == nil || c.score > best.score || (c.score == best.score && p.Downloads > best.p.Downloads) {
			best = &c
		}
	}
	seen := map[*ts.Package]bool{}
	for n := range norms {
		for _, p := range cat.ByNorm(n) {
			if !seen[p] {
				seen[p] = true
				consider(p, 50)
			}
		}
	}
	for _, p := range cat.Packages {
		if seen[p] {
			continue
		}
		pn := p.Norm()
		for n := range norms {
			if len(n) >= 5 && strings.HasPrefix(pn, n) {
				seen[p] = true
				consider(p, 20)
				break
			}
		}
	}
	if best == nil || best.score < 30 {
		return
	}
	e.Pkg, e.FullName, e.Owner, e.Name = best.p, best.p.FullName, best.p.Owner, best.p.Name
	if best.strong {
		e.Match = MatchStrong
		e.Version = best.ver
	} else {
		e.Match = MatchName
	}
}
