package core

import (
	"sort"
	"strings"

	ts "ror2mm/internal/thunderstore"
)

// Group is a section of the installed list.
type Group struct {
	Name    string
	About   string
	Library bool // the "Libraries & dependencies" section
	Entries []*Entry
}

// Category order for grouping. The first matching category of a mod wins.
// Platform tags (Client-side, Server-side), DLC tags and "Mods" are ignored
// because nearly everything has them.
var groupOrder = []struct{ cat, name, about string }{
	{"Modpacks", "Modpacks", "Bundles that pull in a set of mods."},
	{"Player Characters", "Survivors", "New playable characters."},
	{"Items", "Items", "New or changed items and equipment."},
	{"Artifacts", "Artifacts", "New artifacts (run modifiers)."},
	{"Skills", "Skills", "New or changed survivor skills."},
	{"Enemies", "Enemies", "New or changed monsters and bosses."},
	{"Maps", "Stages", "New stages and map changes."},
	{"Gamemodes", "Game modes", "New ways to play."},
	{"Skins", "Skins", "Cosmetic skins."},
	{"Emotes", "Emotes", "Emotes and animations."},
	{"Audio", "Audio", "Music and sound."},
	{"Language", "Language", "Translations."},
	{"Tweaks", "Tweaks & QoL", "Balance changes, quality-of-life and UI improvements."},
	{"Tools", "Tools", "Utilities and developer tools."},
}

func hasCat(p *ts.Package, cat string) bool {
	if p == nil {
		return false
	}
	for _, c := range p.Categories {
		if strings.EqualFold(c, cat) {
			return true
		}
	}
	return false
}

// neededBy lists installed entries (enabled or not) that declare e as a
// dependency. Modpacks do not count: what a pack pulls in is still a mod.
func (inv *Inventory) neededBy(e *Entry) []*Entry {
	if e.FullName == "" {
		return nil
	}
	var out []*Entry
	for _, o := range inv.Entries {
		if o == e || hasCat(o.Pkg, "Modpacks") {
			continue
		}
		for _, d := range o.Deps() {
			if full, _ := ts.ParseDep(d); strings.EqualFold(full, e.FullName) {
				out = append(out, o)
				break
			}
		}
	}
	return out
}

// IsLibrary reports whether an entry is support code rather than something
// you play with: tagged "Libraries" on Thunderstore, or only there because
// other mods need it.
func (inv *Inventory) IsLibrary(e *Entry) bool {
	if e.Dev {
		return false
	}
	if hasCat(e.Pkg, "Libraries") {
		return true
	}
	if len(inv.neededBy(e)) == 0 {
		return false
	}
	// Something needs it. If the player explicitly installed it, it is theirs.
	return !e.Managed || e.AsDependency
}

// DevGroup is the section for your own mods in development.
const DevGroup = "Your mods (in development)"

// GroupName returns the section an entry belongs in.
func GroupName(e *Entry) string {
	if e.Dev {
		return DevGroup
	}
	if e.Pkg == nil {
		return "Not on Thunderstore"
	}
	for _, g := range groupOrder {
		if hasCat(e.Pkg, g.cat) {
			return g.name
		}
	}
	return "Other mods"
}

// LibrariesGroup is the name of the dependencies section.
const LibrariesGroup = "Libraries & dependencies"

// Groups sorts installed entries into sections: mods by category first,
// then mods not on Thunderstore, then libraries and dependencies.
func (inv *Inventory) Groups() []*Group {
	byName := map[string]*Group{}
	add := func(name string, e *Entry) {
		g := byName[name]
		if g == nil {
			g = &Group{Name: name}
			byName[name] = g
		}
		g.Entries = append(g.Entries, e)
	}
	for _, e := range inv.Entries {
		if inv.IsLibrary(e) {
			add(LibrariesGroup, e)
		} else {
			add(GroupName(e), e)
		}
	}
	var out []*Group
	if g := byName[DevGroup]; g != nil {
		g.About = "Mods you are making in the Create tab, deployed with ror2 dev deploy."
		out = append(out, g)
	}
	for _, o := range groupOrder {
		if g := byName[o.name]; g != nil {
			g.About = o.about
			out = append(out, g)
		}
	}
	if g := byName["Other mods"]; g != nil {
		g.About = "Mods without a more specific category."
		out = append(out, g)
	}
	if g := byName["Not on Thunderstore"]; g != nil {
		g.About = "Installed by hand and not found on Thunderstore. ror2 leaves these alone."
		out = append(out, g)
	}
	if g := byName[LibrariesGroup]; g != nil {
		g.Library = true
		g.About = "Support code other mods need: APIs, patchers and shared helpers. You do not use these directly; they are kept because something depends on them."
		out = append(out, g)
	}
	for _, g := range out {
		sort.SliceStable(g.Entries, func(i, j int) bool {
			return strings.ToLower(g.Entries[i].Name) < strings.ToLower(g.Entries[j].Name)
		})
	}
	return out
}
