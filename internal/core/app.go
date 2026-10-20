// Package core ties the catalog and the game folder together: inventory,
// dependency planning, doctor checks and applying changes.
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ror2mm/internal/config"
	"ror2mm/internal/game"
	ts "ror2mm/internal/thunderstore"
)

// App is the loaded state of the manager.
type App struct {
	Cfg  *config.Config
	Game *game.Game
	Cat  *ts.Catalog
	Log  *game.Log
	Inv  *Inventory

	backedUp bool
}

// New loads config and the game folder. The catalog is loaded separately.
func New() (*App, error) {
	cfg, _, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("could not read config: %w", err)
	}
	if cfg.GameDir == "" {
		return nil, errors.New("could not find Risk of Rain 2 in any Sikarugir wrapper.\nSet it with: ror2 config set game-dir \"/path/to/Risk of Rain 2\"")
	}
	g, err := game.Open(cfg.GameDir)
	if err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, Game: g}
	a.Log = g.ReadLog()
	a.Refresh()
	return a, nil
}

// CatalogStale reports whether the cached catalog should be refreshed.
func (a *App) CatalogStale() bool {
	return a.Cat == nil || a.Cat.Age() > time.Duration(a.Cfg.CatalogMaxAgeMinutes)*time.Minute
}

// LoadCachedCatalog loads the catalog from disk only.
func (a *App) LoadCachedCatalog() error {
	c, err := ts.LoadCached(config.CacheDir())
	if err != nil {
		return err
	}
	a.Cat = c
	a.Refresh()
	return nil
}

// RefreshCatalog downloads the latest catalog from Thunderstore.
func (a *App) RefreshCatalog(ctx context.Context, progress ts.Progress) error {
	c, err := ts.Fetch(ctx, config.CacheDir(), a.Cat, progress)
	if c != nil {
		a.Cat = c
		a.Refresh()
	}
	return err
}

// EnsureCatalog loads the cache and refreshes it when stale. If the network
// fails but a cached copy exists, the cached copy is used and a warning returned.
func (a *App) EnsureCatalog(ctx context.Context, offline bool, progress ts.Progress) (warning string, err error) {
	if a.Cat == nil {
		_ = a.LoadCachedCatalog()
	}
	if offline {
		if a.Cat == nil {
			return "", errors.New("no cached catalog yet; run once without --offline")
		}
		return "", nil
	}
	if !a.CatalogStale() {
		return "", nil
	}
	if err := a.RefreshCatalog(ctx, progress); err != nil {
		if a.Cat != nil {
			return fmt.Sprintf("using catalog from %s ago (%v)", HumanDuration(a.Cat.Age()), err), nil
		}
		return "", err
	}
	return "", nil
}

// Refresh rebuilds the inventory after files or the catalog change.
func (a *App) Refresh() {
	a.Inv = BuildInventory(a.Cat, a.Game, a.Log)
}

// Reload rereads everything from disk: config, the game folder's install
// record, and the BepInEx log. Used when another ror2 process or the game
// changed things.
func (a *App) Reload() error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	g, err := game.Open(cfg.GameDir)
	if err != nil {
		return err
	}
	a.Cfg, a.Game = cfg, g
	a.Log = g.ReadLog()
	a.Refresh()
	return nil
}

// DiskStamp summarizes when the files ror2 shows last changed, so a running
// app can notice outside changes cheaply.
func (a *App) DiskStamp() string {
	var b strings.Builder
	for _, p := range []string{
		filepath.Join(a.Game.Dir, "BepInEx", "LogOutput.log"),
		filepath.Join(a.Game.Dir, "BepInEx", ".ror2mm", "state.json"),
		filepath.Join(a.Game.Dir, "BepInEx", "plugins"),
		filepath.Join(a.Game.Dir, "BepInEx", "patchers"),
		filepath.Join(config.Dir(), "config.json"),
	} {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%d;", fi.ModTime().UnixNano())
		} else {
			b.WriteString("-;")
		}
	}
	return b.String()
}

// ReloadLog rereads the BepInEx log (after the game ran).
func (a *App) ReloadLog() {
	a.Log = a.Game.ReadLog()
	a.Refresh()
}

// EnsureBackup snapshots BepInEx once per run, before the first change.
func (a *App) EnsureBackup(label string) (string, error) {
	if a.backedUp {
		return "", nil
	}
	root := a.Game.BackupRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	p, err := a.Game.Backup(root, label, a.Cfg.KeepBackups)
	if err != nil {
		return "", err
	}
	a.backedUp = true
	return p, nil
}

// HumanDuration formats a duration like "3m", "5h", "2d".
func HumanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
