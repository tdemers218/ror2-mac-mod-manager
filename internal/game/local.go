package game

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Manifest is a Thunderstore manifest.json found in a mod folder.
type Manifest struct {
	Name         string   `json:"name"`
	Version      string   `json:"version_number"`
	Dependencies []string `json:"dependencies"`
	Website      string   `json:"website_url"`
	Description  string   `json:"description"`
}

// Local is a mod in the game folder that the manager did not install.
type Local struct {
	Name        string   // folder name, or file name without extension
	Area        string   // "plugins" or "patchers"
	Paths       []string // relative paths that belong to it
	DLLs        []string // DLL names (without .dll)
	Manifest    *Manifest
	FileVersion string // version from the main DLL's file properties
}

// Generated or loader-owned folders that are never mods.
var ignoredLocal = map[string]bool{"mmhook": true, ".ds_store": true}

// ScanLocal finds mods in plugins/ and patchers/ that the manager does not own.
func (g *Game) ScanLocal() []Local {
	owned := map[string]bool{}
	for _, p := range g.state.Packages {
		for _, f := range p.Files {
			parts := strings.SplitN(f, "/", 4)
			if len(parts) >= 3 {
				owned[strings.ToLower(strings.Join(parts[:3], "/"))] = true
			}
		}
	}

	var out []Local
	for _, area := range []string{"plugins", "patchers"} {
		dir := g.abs("BepInEx/" + area)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		loose := map[string]*Local{}
		for _, e := range entries {
			name := e.Name()
			rel := "BepInEx/" + area + "/" + name
			if ignoredLocal[strings.ToLower(name)] || strings.HasPrefix(name, ".") || owned[strings.ToLower(rel)] {
				continue
			}
			if e.IsDir() {
				l := Local{Name: name, Area: area, Paths: []string{rel}}
				g.inspectFolder(&l, filepath.Join(dir, name))
				if len(l.DLLs) == 0 && l.Manifest == nil {
					continue // nothing loadable in it
				}
				out = append(out, l)
				continue
			}
			stem := fileStem(name)
			l := loose[strings.ToLower(stem)]
			if l == nil {
				l = &Local{Name: stem, Area: area}
				loose[strings.ToLower(stem)] = l
			}
			l.Paths = append(l.Paths, rel)
			if strings.EqualFold(filepath.Ext(name), ".dll") {
				l.DLLs = append(l.DLLs, strings.TrimSuffix(name, filepath.Ext(name)))
				if l.FileVersion == "" {
					l.FileVersion = PEVersion(filepath.Join(dir, name))
				}
			}
		}
		for _, l := range loose {
			if len(l.DLLs) > 0 {
				out = append(out, *l)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func (g *Game) inspectFolder(l *Local, dir string) {
	if data, err := os.ReadFile(filepath.Join(dir, "manifest.json")); err == nil {
		var m Manifest
		if json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &m) == nil && m.Name != "" {
			l.Manifest = &m
		}
	}
	var biggest int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".dll") {
			return nil
		}
		stem := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		l.DLLs = append(l.DLLs, stem)
		// The DLL named like the folder is the main one; otherwise take the biggest.
		info, _ := d.Info()
		if strings.EqualFold(stem, l.Name) || (l.FileVersion == "" && info != nil && info.Size() > biggest) {
			if v := PEVersion(p); v != "" {
				l.FileVersion = v
			}
			if info != nil {
				biggest = info.Size()
			}
		}
		return nil
	})
}

func fileStem(name string) string {
	s := strings.TrimSuffix(name, filepath.Ext(name))
	if strings.EqualFold(filepath.Ext(s), ".dll") {
		s = strings.TrimSuffix(s, filepath.Ext(s))
	}
	return s
}

// PEVersion reads the file version from a Windows DLL's version resource.
func PEVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sig := []byte{0xBD, 0x04, 0xEF, 0xFE}
	i := bytes.Index(data, sig)
	if i < 0 || i+16 > len(data) {
		return ""
	}
	ms := binary.LittleEndian.Uint32(data[i+8:])
	ls := binary.LittleEndian.Uint32(data[i+12:])
	v := fmt.Sprintf("%d.%d.%d", ms>>16, ms&0xffff, ls>>16)
	if v == "0.0.0" {
		return ""
	}
	return v
}

// ---------- BepInEx log ----------

// Plugin is a plugin named in the log.
type Plugin struct {
	Name    string
	Version string
}

// Failure is a plugin BepInEx refused to load.
type Failure struct {
	Plugin  Plugin
	Reason  string
	Missing []string // plugin GUIDs it needed but could not find
}

// Log is what the last game launch reported.
type Log struct {
	Path     string
	Time     time.Time
	Loaded   []Plugin
	Failures []Failure
	Crashes  []Crash
	// Assemblies a mod tried to use that are not installed (e.g. an optional
	// integration). The game then cannot read all of the mod's code.
	MissingAssemblies []MissingAssembly
}

// MissingAssembly is "mod assembly X could not load assembly Y".
type MissingAssembly struct {
	Mod     string // assembly of the mod that failed, e.g. ItemQualities
	Missing string // assembly it needs, e.g. ProperSave
}

// Crash is an exception the game logged, with its stack trace.
type Crash struct {
	Message string
	Frames  []string
	// DuringLoad is set when it happened inside the game's content loading,
	// which leaves the game stuck on the loading screen.
	DuringLoad bool
}

var (
	reTypesFailed = regexp.MustCompile(`Assembly\.GetTypes\(\) failed for ([^,]+),`)
	reNoAssembly  = regexp.MustCompile(`Could not load file or assembly '([^,']+)`)
	reLoading     = regexp.MustCompile(`\] Loading \[(.+) (\S+)\]\s*$`)
	reMissing     = regexp.MustCompile(`Could not load \[(.+) (\S+)\] because it has missing dependencies: (.+)$`)
	reCantLoad    = regexp.MustCompile(`(?:Could not load|Skipping) \[(.+?)(?: (\S+))?\] because (.+)$`)
	reDepVer      = regexp.MustCompile(`\s*\(v.*\)$`)
)

// ReadLog parses BepInEx/LogOutput.log. It returns nil if there is no log.
func (g *Game) ReadLog() *Log {
	p := g.abs("BepInEx/LogOutput.log")
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	l := &Log{Path: p}
	if fi, err := f.Stat(); err == nil {
		l.Time = fi.ModTime()
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	var cur *Crash
	inTrace := false
	finish := func() {
		if cur != nil {
			for _, fr := range cur.Frames {
				if strings.Contains(fr, "LoadGameContent") || strings.Contains(fr, "LoadStaticContentAsync") {
					cur.DuringLoad = true
				}
			}
			l.Crashes = append(l.Crashes, *cur)
		}
		cur, inTrace = nil, false
	}
	typesFailedFor := ""
	seenMissing := map[string]bool{}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if m := reTypesFailed.FindStringSubmatch(line); m != nil {
			typesFailedFor = strings.TrimSpace(m[1])
		}
		if typesFailedFor != "" {
			if m := reNoAssembly.FindStringSubmatch(line); m != nil {
				k := typesFailedFor + "|" + m[1]
				if !seenMissing[k] {
					seenMissing[k] = true
					l.MissingAssemblies = append(l.MissingAssemblies, MissingAssembly{Mod: typesFailedFor, Missing: m[1]})
				}
				typesFailedFor = ""
			}
		}
		// Exceptions: "[Error  : Unity Log] SomeException: …" then "Stack trace:" and frames.
		if strings.HasPrefix(line, "[Error  : Unity Log]") && strings.Contains(line, "Exception") {
			finish()
			cur = &Crash{Message: strings.TrimSpace(strings.TrimPrefix(line, "[Error  : Unity Log]"))}
			continue
		}
		if cur != nil {
			switch {
			case line == "Stack trace:":
				inTrace = true
				continue
			case inTrace && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "["):
				if len(cur.Frames) < 60 {
					cur.Frames = append(cur.Frames, strings.TrimSpace(line))
				}
				continue
			default:
				finish()
			}
		}
		if !strings.Contains(line, ":   BepInEx]") {
			continue
		}
		if m := reLoading.FindStringSubmatch(line); m != nil {
			l.Loaded = append(l.Loaded, Plugin{m[1], m[2]})
			continue
		}
		if m := reMissing.FindStringSubmatch(line); m != nil {
			var miss []string
			for _, s := range strings.Split(m[3], ",") {
				s = reDepVer.ReplaceAllString(strings.TrimSpace(s), "")
				if s != "" {
					miss = append(miss, s)
				}
			}
			l.Failures = append(l.Failures, Failure{Plugin: Plugin{m[1], m[2]}, Reason: "missing dependencies", Missing: miss})
			continue
		}
		if m := reCantLoad.FindStringSubmatch(line); m != nil {
			l.Failures = append(l.Failures, Failure{Plugin: Plugin{m[1], m[2]}, Reason: m[3]})
		}
	}
	finish()
	return l
}
