// Package config stores settings and finds the game inside Sikarugir wrappers.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	AppID    = "632360"
	GameName = "Risk of Rain 2"
	GameExe  = "Risk of Rain 2.exe"
)

// Config is saved as JSON in the app support folder.
type Config struct {
	GameDir    string `json:"game_dir"`
	WrapperApp string `json:"wrapper_app"`
	// How old the cached Thunderstore catalog may get before it is refreshed.
	CatalogMaxAgeMinutes int `json:"catalog_max_age_minutes"`
	// How many automatic backups of BepInEx to keep.
	KeepBackups int `json:"keep_backups"`
	// Look-and-feel settings from the Settings tab (theme, borders, …).
	UI map[string]string `json:"ui,omitempty"`
}

// Get returns a UI setting, or def when unset.
func (c *Config) Get(key, def string) string {
	if v, ok := c.UI[key]; ok && v != "" {
		return v
	}
	return def
}

// Set stores a UI setting (call Save to persist).
func (c *Config) Set(key, val string) {
	if c.UI == nil {
		c.UI = map[string]string{}
	}
	c.UI[key] = val
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// Dir holds config, backups and other persistent data.
// ROR2MM_DATA overrides it (used for testing).
func Dir() string {
	if d := os.Getenv("ROR2MM_DATA"); d != "" {
		return d
	}
	return filepath.Join(home(), "Library", "Application Support", "ror2mm")
}

// CacheDir holds the catalog and downloaded zips; safe to delete.
func CacheDir() string { return filepath.Join(home(), "Library", "Caches", "ror2mm") }

func path() string { return filepath.Join(Dir(), "config.json") }

// Load reads the config, filling in anything missing by looking at the system.
// The bool reports whether detection had to run.
func Load() (*Config, bool, error) {
	c := &Config{}
	data, err := os.ReadFile(path())
	if err == nil {
		if err := json.Unmarshal(data, c); err != nil {
			return nil, false, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	if c.CatalogMaxAgeMinutes == 0 {
		c.CatalogMaxAgeMinutes = 60
	}
	if c.KeepBackups == 0 {
		c.KeepBackups = 8
	}
	// ROR2MM_GAME_DIR points at a different game folder for this run only (testing).
	if d := os.Getenv("ROR2MM_GAME_DIR"); d != "" {
		c.GameDir = d
		return c, false, nil
	}
	detected := false
	if c.GameDir == "" || c.WrapperApp == "" {
		detected = true
		if found := Detect(); len(found) > 0 {
			if c.GameDir == "" {
				c.GameDir = found[0].GameDir
			}
			if c.WrapperApp == "" {
				c.WrapperApp = found[0].Wrapper
			}
			_ = c.Save()
		}
	}
	return c, detected, nil
}

// Save writes the config.
func (c *Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path(), append(data, '\n'), 0o644)
}

// Install is one place the game was found.
type Install struct {
	Wrapper string    // path to the Sikarugir .app
	GameDir string    // Mac path to the game folder
	Used    time.Time // when the wrapper's prefix was last written to
}

// Prefix returns the Wine prefix inside a Sikarugir wrapper.
func Prefix(wrapper string) string {
	return filepath.Join(wrapper, "Contents", "SharedSupport", "prefix")
}

// WineBin returns the wrapper's wine binary.
func WineBin(wrapper string) string {
	return filepath.Join(wrapper, "Contents", "SharedSupport", "wine", "bin", "wine")
}

var pathRe = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// Detect looks through Sikarugir wrappers for Windows Steam installs that have the game.
// The most recently used wrapper comes first.
func Detect() []Install {
	var wrappers []string
	globs := []string{
		filepath.Join(home(), "Applications", "Sikarugir", "*.app"),
		filepath.Join(home(), "Applications", "*.app"),
		"/Applications/Sikarugir/*.app",
		"/Volumes/*/Sikarugir/*.app",
	}
	seen := map[string]bool{}
	for _, g := range globs {
		m, _ := filepath.Glob(g)
		for _, w := range m {
			if seen[w] {
				continue
			}
			seen[w] = true
			if _, err := os.Stat(filepath.Join(Prefix(w), "drive_c")); err == nil {
				wrappers = append(wrappers, w)
			}
		}
	}

	var out []Install
	for _, w := range wrappers {
		prefix := Prefix(w)
		steam := filepath.Join(prefix, "drive_c", "Program Files (x86)", "Steam")
		libs := []string{steam}
		if data, err := os.ReadFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf")); err == nil {
			for _, m := range pathRe.FindAllStringSubmatch(string(data), -1) {
				libs = append(libs, WinToMac(prefix, strings.ReplaceAll(m[1], `\\`, `\`)))
			}
		}
		var used time.Time
		if fi, err := os.Stat(filepath.Join(prefix, "user.reg")); err == nil {
			used = fi.ModTime()
		}
		for _, lib := range libs {
			dir := filepath.Join(lib, "steamapps", "common", GameName)
			if _, err := os.Stat(filepath.Join(dir, GameExe)); err == nil {
				out = append(out, Install{Wrapper: w, GameDir: dir, Used: used})
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Used.After(out[j].Used) })
	return out
}

// WinToMac converts a Wine path like Z:\Volumes\X or C:\Program Files to a Mac path.
func WinToMac(prefix, p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) >= 2 && p[1] == ':' {
		drive := strings.ToLower(p[:1])
		rest := p[2:]
		if drive == "z" {
			return filepath.Clean("/" + rest)
		}
		if drive == "c" {
			return filepath.Join(prefix, "drive_c", rest)
		}
		// Other drives are symlinks in dosdevices.
		if target, err := os.Readlink(filepath.Join(prefix, "dosdevices", drive+":")); err == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(prefix, "dosdevices", target)
			}
			return filepath.Join(target, rest)
		}
	}
	return p
}

// ShortPath swaps the home folder for ~ for display.
func ShortPath(p string) string {
	h := home()
	if h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}
