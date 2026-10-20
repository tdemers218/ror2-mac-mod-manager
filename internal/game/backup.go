package game

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Paths inside BepInEx that a backup covers. core is included because a
// restore should give back a working loader too.
var backupAreas = []string{"plugins", "patchers", "config", "monomod", "core", ".ror2mm/state.json", ".ror2mm/disabled"}

// BackupRoot is where snapshots live: inside the game folder, so they sit on
// the same disk and can be APFS clones (nearly free until files change).
func (g *Game) BackupRoot() string { return g.abs("BepInEx/.ror2mm/backups") }

// cloneCopy copies a file or folder, using APFS clones when the disk supports them.
func cloneCopy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := exec.Command("cp", "-c", "-R", "-p", src, dst).CombinedOutput()
	if err != nil {
		// Not APFS or clone refused: plain copy.
		os.RemoveAll(dst)
		out, err = exec.Command("cp", "-R", "-p", src, dst).CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// BackupInfo describes one saved snapshot.
type BackupInfo struct {
	Name string
	Path string
	Time time.Time
	Size int64
}

// Backup copies the BepInEx folders into backupRoot/<timestamp>-<label> and
// prunes old snapshots so only keep remain.
func (g *Game) Backup(backupRoot, label string, keep int) (string, error) {
	name := time.Now().Format("2006-01-02_15-04-05")
	if label != "" {
		name += "-" + label
	}
	dest := filepath.Join(backupRoot, name)
	for i := 2; ; i++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		dest = filepath.Join(backupRoot, fmt.Sprintf("%s-%d", name, i))
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	for _, a := range backupAreas {
		src := g.abs("BepInEx/" + a)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := cloneCopy(src, filepath.Join(dest, filepath.FromSlash(a))); err != nil {
			return "", fmt.Errorf("backup of %s failed: %w", a, err)
		}
	}
	_ = os.WriteFile(filepath.Join(dest, "GAME_DIR.txt"), []byte(g.Dir+"\n"), 0o644)
	if keep > 0 {
		list, _ := ListBackups(backupRoot)
		for i := keep; i < len(list); i++ {
			os.RemoveAll(list[i].Path)
		}
	}
	return dest, nil
}

// ListBackups returns snapshots, newest first.
func ListBackups(backupRoot string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []BackupInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(backupRoot, e.Name())
		out = append(out, BackupInfo{Name: e.Name(), Path: p, Time: info.ModTime(), Size: dirSize(p)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

func dirSize(p string) int64 {
	var n int64
	_ = filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n
}

// Restore puts a snapshot back. The current state is backed up first.
func (g *Game) Restore(backupRoot, name string, keep int) (string, error) {
	src := filepath.Join(backupRoot, name)
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("no backup named %s", name)
	}
	// No pruning here: it could delete the very snapshot being restored.
	_ = keep
	safety, err := g.Backup(backupRoot, "before-restore", 0)
	if err != nil {
		return "", err
	}
	for _, a := range backupAreas {
		from := filepath.Join(src, filepath.FromSlash(a))
		to := g.abs("BepInEx/" + a)
		if _, err := os.Stat(from); err != nil {
			// Not in the snapshot. Clear it unless it is the loader itself.
			if a != "core" {
				os.RemoveAll(to)
			}
			continue
		}
		if err := os.RemoveAll(to); err != nil {
			return safety, err
		}
		if err := cloneCopy(from, to); err != nil {
			return safety, fmt.Errorf("restore of %s failed: %w", a, err)
		}
	}
	reloaded, err := Open(g.Dir)
	if err != nil {
		return safety, err
	}
	g.state = reloaded.state
	return safety, nil
}
