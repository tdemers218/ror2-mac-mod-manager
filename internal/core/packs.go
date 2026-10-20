package core

// Mod packs: named snapshots of which mods are switched on, so a whole
// arrangement can be loaded in one go. The built-in Vanilla pack starts the
// game with no mods by turning the loader off, without moving any files.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"ror2mm/internal/config"
)

// VanillaPack is the name of the built-in no-mods pack.
const VanillaPack = "Vanilla"

// PackMod is one mod that is on in a pack.
type PackMod struct {
	FullName string `json:"full_name,omitempty"` // Thunderstore package, for mods ror2 installed
	Version  string `json:"version,omitempty"`
	Local    string `json:"local,omitempty"` // folder or file name, for hand-installed and dev mods
	Name     string `json:"name"`
}

// Pack is a saved mod arrangement.
type Pack struct {
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Mods     []PackMod `json:"mods"`
	Settings bool      `json:"settings"` // a copy of BepInEx/config was saved with it
	Vanilla  bool      `json:"-"`
}

// Key is how a pack mod is matched to what is installed.
func (pm PackMod) Key() string {
	if pm.FullName != "" {
		return "ts:" + strings.ToLower(pm.FullName)
	}
	return "local:" + strings.ToLower(pm.Local)
}

func entryKey(e *Entry) string {
	if e.Managed {
		return "ts:" + strings.ToLower(e.FullName)
	}
	if e.Local != nil {
		return "local:" + strings.ToLower(e.Local.Name)
	}
	return ""
}

func packsDir() string { return filepath.Join(config.Dir(), "packs") }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9 _.-]+`)

func packFile(name string) string {
	slug := strings.TrimSpace(unsafeName.ReplaceAllString(name, "_"))
	return filepath.Join(packsDir(), slug+".json")
}

func packSettingsDir(name string) string {
	return strings.TrimSuffix(packFile(name), ".json") + ".config"
}

// Packs lists the built-in Vanilla pack followed by saved packs, A to Z.
func (a *App) Packs() []*Pack {
	out := []*Pack{{Name: VanillaPack, Vanilla: true}}
	entries, _ := os.ReadDir(packsDir())
	var saved []*Pack
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(packsDir(), e.Name()))
		if err != nil {
			continue
		}
		var p Pack
		if json.Unmarshal(data, &p) == nil && p.Name != "" {
			saved = append(saved, &p)
		}
	}
	sort.Slice(saved, func(i, j int) bool { return strings.ToLower(saved[i].Name) < strings.ToLower(saved[j].Name) })
	return append(out, saved...)
}

// FindPack returns a pack by name (case-insensitive).
func (a *App) FindPack(name string) *Pack {
	for _, p := range a.Packs() {
		if strings.EqualFold(p.Name, strings.TrimSpace(name)) {
			return p
		}
	}
	return nil
}

// ValidPackName explains what is wrong with a new pack name, or returns "".
func ValidPackName(name string) string {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "Give the pack a name."
	case len(name) > 40:
		return "Keep the name under 40 characters."
	case strings.EqualFold(name, VanillaPack):
		return "Vanilla is the built-in no-mods pack; pick another name."
	}
	return ""
}

// SavePack records the mods that are on right now. withSettings also keeps
// a copy of every mod's config file, restored when the pack is loaded.
func (a *App) SavePack(name string, withSettings bool) (*Pack, error) {
	name = strings.TrimSpace(name)
	if msg := ValidPackName(name); msg != "" {
		return nil, errors.New(msg)
	}
	p := &Pack{Name: name, Created: time.Now(), Updated: time.Now(), Settings: withSettings}
	if old := a.FindPack(name); old != nil && !old.Vanilla {
		p.Created = old.Created
	}
	for _, e := range a.Inv.Entries {
		if !e.Enabled {
			continue
		}
		switch {
		case e.Managed:
			p.Mods = append(p.Mods, PackMod{FullName: e.FullName, Version: e.Version, Name: e.Name})
		case e.Local != nil:
			p.Mods = append(p.Mods, PackMod{Local: e.Local.Name, Version: e.Version, Name: e.Name})
		}
	}
	if err := os.MkdirAll(packsDir(), 0o755); err != nil {
		return nil, err
	}
	settings := packSettingsDir(name)
	os.RemoveAll(settings)
	if withSettings {
		if err := copyTree(filepath.Join(a.Game.Dir, "BepInEx", "config"), settings); err != nil {
			return nil, fmt.Errorf("could not copy mod settings: %w", err)
		}
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(packFile(name), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	return p, nil
}

// DeletePack removes a saved pack (never Vanilla).
func (a *App) DeletePack(name string) error {
	p := a.FindPack(name)
	if p == nil {
		return fmt.Errorf("no pack called %q", name)
	}
	if p.Vanilla {
		return errors.New("the Vanilla pack is built in and cannot be deleted")
	}
	os.RemoveAll(packSettingsDir(p.Name))
	if a.Cfg.Get("active_pack", "") == p.Name {
		a.Cfg.Set("active_pack", "")
		_ = a.Cfg.Save()
	}
	return os.Remove(packFile(p.Name))
}

// RenamePack gives a saved pack a new name.
func (a *App) RenamePack(oldName, newName string) error {
	p := a.FindPack(oldName)
	if p == nil || p.Vanilla {
		return fmt.Errorf("no saved pack called %q", oldName)
	}
	newName = strings.TrimSpace(newName)
	if msg := ValidPackName(newName); msg != "" {
		return errors.New(msg)
	}
	if q := a.FindPack(newName); q != nil && !strings.EqualFold(q.Name, p.Name) {
		return fmt.Errorf("there is already a pack called %q", q.Name)
	}
	oldFile, oldSettings := packFile(p.Name), packSettingsDir(p.Name)
	p.Name, p.Updated = newName, time.Now()
	data, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(packFile(newName), append(data, '\n'), 0o644); err != nil {
		return err
	}
	if oldFile != packFile(newName) {
		os.Remove(oldFile)
		if _, err := os.Stat(oldSettings); err == nil {
			_ = os.Rename(oldSettings, packSettingsDir(newName))
		}
	}
	if strings.EqualFold(a.Cfg.Get("active_pack", ""), oldName) {
		a.Cfg.Set("active_pack", newName)
		_ = a.Cfg.Save()
	}
	return nil
}

// PackPlan is what loading a pack will change.
type PackPlan struct {
	Pack         *Pack
	Enable       []*Entry // installed, will be switched on
	Disable      []*Entry // on now, not in the pack: switched off
	Install      *Plan    // in the pack but no longer installed: downloaded again
	Missing      []string // hand-installed mods in the pack that are gone
	LoaderOn     bool     // BepInEx runs after loading
	LoaderChange bool
	Settings     bool // the pack's saved mod settings are put back
}

// Empty reports whether loading would change nothing.
func (pp *PackPlan) Empty() bool {
	return len(pp.Enable) == 0 && len(pp.Disable) == 0 && (pp.Install == nil || pp.Install.Empty()) && !pp.LoaderChange && !pp.Settings
}

// PlanPack works out what loading a pack changes.
func (a *App) PlanPack(p *Pack) (*PackPlan, error) {
	loaderOn := a.Inv.BepInEx.DoorstopOn
	pp := &PackPlan{Pack: p, LoaderOn: !p.Vanilla}
	pp.LoaderChange = loaderOn != pp.LoaderOn
	if p.Vanilla {
		// Mods stay exactly as they are, so switching back is instant.
		return pp, nil
	}
	want := map[string]PackMod{}
	for _, m := range p.Mods {
		want[m.Key()] = m
	}
	have := map[string]*Entry{}
	for _, e := range a.Inv.Entries {
		k := entryKey(e)
		if k == "" {
			continue
		}
		have[k] = e
		_, inPack := want[k]
		switch {
		case inPack && !e.Enabled:
			pp.Enable = append(pp.Enable, e)
		case !inPack && e.Enabled:
			pp.Disable = append(pp.Disable, e)
		}
	}
	var reqs []Request
	for k, m := range want {
		if have[k] != nil {
			continue
		}
		if m.FullName == "" {
			pp.Missing = append(pp.Missing, m.Name)
			continue
		}
		r := Request{FullName: m.FullName, Version: m.Version}
		if a.Cat != nil {
			if pkg := a.Cat.Get(m.FullName); pkg != nil && pkg.Version(m.Version) == nil {
				r.Version = "" // that version is gone from Thunderstore: use the newest
			}
		}
		reqs = append(reqs, r)
	}
	sort.Strings(pp.Missing)
	if len(reqs) > 0 {
		if a.Cat == nil {
			return nil, errors.New("the Thunderstore catalog is needed to reinstall mods from this pack")
		}
		plan, err := a.PlanInstall(reqs)
		if err != nil {
			return nil, err
		}
		pp.Install = plan
	}
	if p.Settings {
		if _, err := os.Stat(packSettingsDir(p.Name)); err == nil {
			pp.Settings = true
		}
	}
	sortEntries(pp.Enable)
	sortEntries(pp.Disable)
	return pp, nil
}

func sortEntries(list []*Entry) {
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
}

// ApplyPack loads a pack: backs up, reinstalls missing mods, switches mods
// on and off, puts saved settings back and sets the loader.
func (a *App) ApplyPack(ctx context.Context, pp *PackPlan, progress func(Event)) error {
	if _, err := a.EnsureBackup("before-pack"); err != nil {
		return fmt.Errorf("could not back up BepInEx first, nothing was changed: %w", err)
	}
	defer a.Refresh()
	if pp.Install != nil && !pp.Install.Empty() {
		if err := a.Apply(ctx, pp.Install, "before-pack", progress); err != nil {
			return err
		}
	}
	set := func(list []*Entry, on bool) error {
		for _, e := range list {
			var err error
			if e.Managed {
				err = a.Game.SetEnabled(e.FullName, on)
			} else if e.Local != nil {
				err = a.Game.SetLocalEnabled(*e.Local, on)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", e.Name, err)
			}
		}
		return nil
	}
	if err := set(pp.Disable, false); err != nil {
		return err
	}
	if err := set(pp.Enable, true); err != nil {
		return err
	}
	if pp.Settings {
		if err := copyTree(packSettingsDir(pp.Pack.Name), filepath.Join(a.Game.Dir, "BepInEx", "config")); err != nil {
			return fmt.Errorf("could not put the pack's mod settings back: %w", err)
		}
	}
	if pp.LoaderChange || !pp.LoaderOn {
		if err := a.Game.SetLoader(pp.LoaderOn); err != nil {
			return err
		}
	}
	a.Cfg.Set("active_pack", pp.Pack.Name)
	return a.Cfg.Save()
}

// ActivePack is the last loaded pack and whether things still match it.
func (a *App) ActivePack() (p *Pack, matches bool) {
	name := a.Cfg.Get("active_pack", "")
	if name == "" {
		return nil, false
	}
	p = a.FindPack(name)
	if p == nil {
		return nil, false
	}
	return p, a.PackMatches(p)
}

// PackMatches reports whether the game is set up exactly as the pack says.
func (a *App) PackMatches(p *Pack) bool {
	if p.Vanilla {
		return !a.Inv.BepInEx.DoorstopOn
	}
	if !a.Inv.BepInEx.DoorstopOn {
		return false
	}
	want := map[string]bool{}
	for _, m := range p.Mods {
		want[m.Key()] = true
	}
	n := 0
	for _, e := range a.Inv.Entries {
		k := entryKey(e)
		if k == "" || !e.Enabled {
			continue
		}
		if !want[k] {
			return false
		}
		n++
	}
	return n == len(want)
}

// PackModsInstalled says how many of a pack's mods are installed now.
func (a *App) PackModsInstalled(p *Pack) int {
	n := 0
	for _, m := range p.Mods {
		for _, e := range a.Inv.Entries {
			if entryKey(e) == m.Key() {
				n++
				break
			}
		}
	}
	return n
}

// copyTree copies every file under src into dst, replacing files that exist.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
