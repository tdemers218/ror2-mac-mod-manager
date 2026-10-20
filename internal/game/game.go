// Package game manages the files inside the Risk of Rain 2 folder.
package game

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ror2mm/internal/config"
)

// Installed is a package the manager put in the game folder.
type Installed struct {
	FullName     string    `json:"full_name"`
	Version      string    `json:"version"`
	Enabled      bool      `json:"enabled"`
	AsDependency bool      `json:"as_dependency"`
	InstalledAt  time.Time `json:"installed_at"`
	// Files the package owns, relative to the game folder, using forward slashes.
	Files []string `json:"files"`
	// Config files it shipped. These are kept on uninstall so settings survive.
	Configs []string `json:"configs,omitempty"`
}

type state struct {
	Version  int                   `json:"version"`
	Packages map[string]*Installed `json:"packages"`
}

// Game is one Risk of Rain 2 install.
type Game struct {
	Dir   string
	state state
}

const stateRel = "BepInEx/.ror2mm/state.json"
const disabledRel = "BepInEx/.ror2mm/disabled"

// Open loads the game folder and the manager's record of what it installed.
func Open(dir string) (*Game, error) {
	if dir == "" {
		return nil, errors.New("game folder is not set (run: ror2 config set game-dir <path>)")
	}
	if _, err := os.Stat(filepath.Join(dir, config.GameExe)); err != nil {
		return nil, fmt.Errorf("no %s in %s", config.GameExe, dir)
	}
	g := &Game{Dir: dir, state: state{Version: 1, Packages: map[string]*Installed{}}}
	data, err := os.ReadFile(g.abs(stateRel))
	if err == nil {
		if err := json.Unmarshal(data, &g.state); err != nil {
			return nil, fmt.Errorf("manager state file is damaged: %w", err)
		}
		if g.state.Packages == nil {
			g.state.Packages = map[string]*Installed{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return g, nil
}

func (g *Game) abs(rel string) string { return filepath.Join(g.Dir, filepath.FromSlash(rel)) }

// Save writes the state file.
func (g *Game) Save() error {
	p := g.abs(stateRel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(g.state, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Packages returns managed packages sorted by name.
func (g *Game) Packages() []*Installed {
	out := make([]*Installed, 0, len(g.state.Packages))
	for _, p := range g.state.Packages {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName) })
	return out
}

// Get returns a managed package by full name (case-insensitive).
func (g *Game) Get(fullName string) *Installed {
	if p, ok := g.state.Packages[fullName]; ok {
		return p
	}
	for k, p := range g.state.Packages {
		if strings.EqualFold(k, fullName) {
			return p
		}
	}
	return nil
}

// BepInEx describes whether the mod loader is set up.
type BepInEx struct {
	Installed     bool
	Version       string
	DoorstopDLL   bool
	DoorstopOn    bool
	MissingPieces []string
}

// BepInExStatus checks the loader files.
func (g *Game) BepInExStatus() BepInEx {
	var b BepInEx
	check := func(rel, label string) bool {
		if _, err := os.Stat(g.abs(rel)); err != nil {
			b.MissingPieces = append(b.MissingPieces, label)
			return false
		}
		return true
	}
	core := check("BepInEx/core/BepInEx.dll", "BepInEx/core/BepInEx.dll")
	check("BepInEx/core/BepInEx.Preloader.dll", "BepInEx/core/BepInEx.Preloader.dll")
	b.DoorstopDLL = check("winhttp.dll", "winhttp.dll (doorstop)")
	b.Installed = core
	if data, err := os.ReadFile(g.abs("doorstop_config.ini")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			l := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(line), " ", ""))
			if strings.HasPrefix(l, "enabled=") {
				b.DoorstopOn = strings.HasPrefix(l, "enabled=true")
			}
		}
	} else {
		b.MissingPieces = append(b.MissingPieces, "doorstop_config.ini")
	}
	if v := PEVersion(g.abs("BepInEx/core/BepInEx.dll")); v != "" {
		b.Version = v
	}
	return b
}

// ---------- installing ----------

// FileOp is a callback for each file written, for progress display.
type FileOp func(rel string)

// InstallZip puts a package zip into the game folder, replacing any older copy
// of the same package. Existing config files are never overwritten.
func (g *Game) InstallZip(fullName, version, zipPath string, asDep bool) error {
	prev := g.Get(fullName)
	wasDisabled := prev != nil && !prev.Enabled
	if prev != nil {
		asDep = prev.AsDependency && asDep
		if err := g.removeFiles(prev); err != nil {
			return err
		}
		delete(g.state.Packages, prev.FullName)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("%s: bad zip: %w", fullName, err)
	}
	defer zr.Close()

	inst := &Installed{FullName: fullName, Version: version, Enabled: true, AsDependency: asDep, InstalledAt: time.Now()}
	seen := map[string]bool{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel, isConfig, ok := Destination(fullName, f.Name)
		if !ok {
			continue
		}
		if seen[strings.ToLower(rel)] {
			continue
		}
		seen[strings.ToLower(rel)] = true
		dest := g.abs(rel)
		if isConfig {
			if _, err := os.Stat(dest); err == nil {
				continue // keep the player's settings
			}
		}
		if err := extract(f, dest); err != nil {
			return fmt.Errorf("%s: %w", fullName, err)
		}
		if isConfig {
			inst.Configs = append(inst.Configs, rel)
		} else {
			inst.Files = append(inst.Files, rel)
		}
	}
	if len(inst.Files) == 0 && len(inst.Configs) == 0 {
		return fmt.Errorf("%s: zip had nothing to install", fullName)
	}
	g.state.Packages[fullName] = inst
	if wasDisabled {
		if err := g.SetEnabled(fullName, false); err != nil {
			return err
		}
	}
	return g.Save()
}

// Destination maps a path inside a Thunderstore zip to where it goes in the game
// folder, following the same rules as r2modman for BepInEx games.
func Destination(fullName, zipPath string) (rel string, isConfig bool, ok bool) {
	p := strings.ReplaceAll(zipPath, `\`, "/")
	p = strings.TrimPrefix(p, "./")
	p = path.Clean(p)
	if p == "." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") || strings.Contains(p, "/../") {
		return "", false, false
	}
	parts := strings.Split(p, "/")
	for _, s := range parts {
		if s == "__MACOSX" || s == ".DS_Store" || s == ".." {
			return "", false, false
		}
	}
	if strings.EqualFold(parts[0], "BepInEx") && len(parts) > 1 {
		parts = parts[1:]
	}
	first := strings.ToLower(parts[0])
	rest := parts[1:]
	switch {
	case len(rest) > 0 && (first == "plugins" || first == "patchers" || first == "monomod" || first == "core"):
		return path.Join(append([]string{"BepInEx", first, fullName}, rest...)...), false, true
	case len(rest) > 0 && first == "config":
		return path.Join(append([]string{"BepInEx", "config"}, rest...)...), true, true
	case len(rest) == 0 && strings.HasSuffix(first, ".mm.dll"):
		return path.Join("BepInEx", "monomod", fullName, parts[0]), false, true
	default:
		return path.Join(append([]string{"BepInEx", "plugins", fullName}, parts...)...), false, true
	}
}

func extract(f *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := dest + ".ror2mm-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// Uninstall removes a package's files. Its config files are kept.
func (g *Game) Uninstall(fullName string) error {
	p := g.Get(fullName)
	if p == nil {
		return fmt.Errorf("%s is not installed by ror2", fullName)
	}
	if err := g.removeFiles(p); err != nil {
		return err
	}
	delete(g.state.Packages, p.FullName)
	return g.Save()
}

func (g *Game) removeFiles(p *Installed) error {
	base := ""
	if !p.Enabled {
		base = disabledRel + "/" + p.FullName
	}
	dirs := map[string]bool{}
	for _, rel := range p.Files {
		full := g.abs(rel)
		if base != "" {
			full = g.abs(base + "/" + rel)
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		dirs[filepath.Dir(full)] = true
	}
	g.pruneDirs(dirs)
	if base != "" {
		os.RemoveAll(g.abs(base))
	}
	return nil
}

// pruneDirs removes now-empty folders, walking up but never past BepInEx/<area>.
func (g *Game) pruneDirs(dirs map[string]bool) {
	stops := map[string]bool{g.Dir: true}
	for _, a := range []string{"BepInEx", "BepInEx/plugins", "BepInEx/patchers", "BepInEx/monomod", "BepInEx/core", "BepInEx/config", "BepInEx/.ror2mm", disabledRel} {
		stops[g.abs(a)] = true
	}
	list := make([]string, 0, len(dirs))
	for d := range dirs {
		list = append(list, d)
	}
	// Deepest first.
	sort.Slice(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	for _, d := range list {
		for !stops[d] && strings.HasPrefix(d, g.Dir) {
			removeJunk(d)
			if err := os.Remove(d); err != nil {
				break
			}
			d = filepath.Dir(d)
		}
	}
}

// removeJunk deletes Finder litter so a folder can count as empty.
func removeJunk(dir string) {
	os.Remove(filepath.Join(dir, ".DS_Store"))
}

// SetEnabled turns a package on or off by moving its files out of BepInEx's sight.
func (g *Game) SetEnabled(fullName string, on bool) error {
	p := g.Get(fullName)
	if p == nil {
		return fmt.Errorf("%s is not installed by ror2", fullName)
	}
	if p.Enabled == on {
		return nil
	}
	store := disabledRel + "/" + p.FullName
	dirs := map[string]bool{}
	for _, rel := range p.Files {
		live, parked := g.abs(rel), g.abs(store+"/"+rel)
		from, to := live, parked
		if on {
			from, to = parked, live
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		dirs[filepath.Dir(from)] = true
	}
	g.pruneDirs(dirs)
	if on {
		os.RemoveAll(g.abs(store))
	}
	p.Enabled = on
	return g.Save()
}

// MarkExplicit records that the player asked for this package directly.
func (g *Game) MarkExplicit(fullName string) {
	if p := g.Get(fullName); p != nil && p.AsDependency {
		p.AsDependency = false
		_ = g.Save()
	}
}

// RemoveLocal deletes files that were not installed by the manager (after a backup).
func (g *Game) RemoveLocal(relPaths []string) error {
	dirs := map[string]bool{}
	for _, rel := range relPaths {
		full := g.abs(rel)
		if err := os.RemoveAll(full); err != nil {
			return err
		}
		dirs[filepath.Dir(full)] = true
	}
	g.pruneDirs(dirs)
	return nil
}
