package game

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Hand-installed mods (and your own dev builds) are switched off by moving
// their files here, keeping the plugins/ or patchers/ layout:
// disabled-local/<area>/<file or folder>.
const parkedLocalRel = "BepInEx/.ror2mm/disabled-local"

// ScanParkedLocal finds hand-installed mods that were switched off.
func (g *Game) ScanParkedLocal() []Local {
	var out []Local
	for _, area := range []string{"plugins", "patchers"} {
		dir := g.abs(parkedLocalRel + "/" + area)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		loose := map[string]*Local{}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			rel := "BepInEx/" + area + "/" + name
			if e.IsDir() {
				l := Local{Name: name, Area: area, Paths: []string{rel}, Parked: true}
				g.inspectFolder(&l, filepath.Join(dir, name))
				out = append(out, l)
				continue
			}
			stem := fileStem(name)
			l := loose[strings.ToLower(stem)]
			if l == nil {
				l = &Local{Name: stem, Area: area, Parked: true}
				loose[strings.ToLower(stem)] = l
			}
			l.Paths = append(l.Paths, rel)
			if strings.EqualFold(filepath.Ext(name), ".dll") {
				l.DLLs = append(l.DLLs, strings.TrimSuffix(name, filepath.Ext(name)))
			}
		}
		for _, l := range loose {
			out = append(out, *l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// SetLocalEnabled switches a hand-installed mod on or off by moving its files.
func (g *Game) SetLocalEnabled(l Local, on bool) error {
	if l.Parked != on {
		return nil // already in that state
	}
	for _, rel := range l.Paths {
		live := g.abs(rel)
		parked := g.abs(parkedLocalRel + "/" + strings.TrimPrefix(rel, "BepInEx/"))
		from, to := live, parked
		if on {
			from, to = parked, live
		}
		if _, err := os.Stat(to); err == nil {
			return fmt.Errorf("%s already exists, so %s was left as it is", to, l.Name)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	g.pruneDirs(map[string]bool{g.abs(parkedLocalRel + "/" + l.Area): true})
	return nil
}

var doorstopEnabled = regexp.MustCompile(`(?mi)^(\s*enabled\s*=\s*)(true|false)\s*$`)

// SetLoader turns BepInEx on or off for the next launch through
// doorstop_config.ini. Off starts the plain game without touching any mod.
func (g *Game) SetLoader(on bool) error {
	p := g.abs("doorstop_config.ini")
	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("could not read doorstop_config.ini: %w", err)
	}
	v := "false"
	if on {
		v = "true"
	}
	if !doorstopEnabled.Match(data) {
		return errors.New("doorstop_config.ini has no enabled = line")
	}
	out := doorstopEnabled.ReplaceAll(data, []byte("${1}"+v))
	if string(out) == string(data) {
		return nil
	}
	return os.WriteFile(p, out, 0o644)
}
