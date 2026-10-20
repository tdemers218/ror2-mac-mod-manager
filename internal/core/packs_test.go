package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ror2mm/internal/config"
	"ror2mm/internal/game"
)

// fakeGame builds a tiny game folder with two tracked mods and one
// hand-installed mod.
func fakeGame(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ROR2MM_DATA", t.TempDir())
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(config.GameExe, "")
	write("winhttp.dll", "")
	write("doorstop_config.ini", "[General]\n\nenabled = true\n\ntarget_assembly=BepInEx\\core\\BepInEx.Preloader.dll\n")
	write("BepInEx/core/BepInEx.dll", "")
	write("BepInEx/core/BepInEx.Preloader.dll", "")
	write("BepInEx/plugins/A-One/One.dll", "")
	write("BepInEx/plugins/B-Two/Two.dll", "")
	write("BepInEx/plugins/HandMod/HandMod.dll", "")
	write("BepInEx/config/one.cfg", "speed = 1\n")
	write("BepInEx/.ror2mm/state.json", `{"version":1,"packages":{
		"A-One":{"full_name":"A-One","version":"1.0.0","enabled":true,"files":["BepInEx/plugins/A-One/One.dll"]},
		"B-Two":{"full_name":"B-Two","version":"2.0.0","enabled":true,"files":["BepInEx/plugins/B-Two/Two.dll"]}}}`)
	g, err := game.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Cfg: &config.Config{GameDir: dir, KeepBackups: 3}, Game: g}
	a.Refresh()
	return a
}

func enabledNames(a *App) string {
	var on []string
	for _, e := range a.Inv.Entries {
		if e.Enabled {
			on = append(on, e.Name)
		}
	}
	return strings.Join(on, ",")
}

func TestPacksSaveLoadAndVanilla(t *testing.T) {
	a := fakeGame(t)
	ctx := context.Background()
	if _, err := a.SavePack("Full", true); err != nil {
		t.Fatal(err)
	}
	// Switch One and the hand-installed mod off, then save a smaller pack.
	if err := a.Game.SetEnabled("A-One", false); err != nil {
		t.Fatal(err)
	}
	a.Refresh()
	for _, e := range a.Inv.Entries {
		if e.Local != nil && e.Name == "HandMod" {
			if err := a.Game.SetLocalEnabled(*e.Local, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	a.Refresh()
	if got := enabledNames(a); got != "Two" {
		t.Fatalf("after switching off: %s", got)
	}
	if _, err := a.SavePack("Light", false); err != nil {
		t.Fatal(err)
	}
	// Change a setting, then load Full: everything comes back, settings too.
	os.WriteFile(filepath.Join(a.Game.Dir, "BepInEx/config/one.cfg"), []byte("speed = 9\n"), 0o644)
	pp, err := a.PlanPack(a.FindPack("Full"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pp.Enable) != 2 || len(pp.Disable) != 0 || !pp.Settings {
		t.Fatalf("plan: enable %d disable %d settings %v", len(pp.Enable), len(pp.Disable), pp.Settings)
	}
	if err := a.ApplyPack(ctx, pp, nil); err != nil {
		t.Fatal(err)
	}
	if got := enabledNames(a); got != "HandMod,One,Two" {
		t.Fatalf("after Full: %s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(a.Game.Dir, "BepInEx/config/one.cfg")); string(b) != "speed = 1\n" {
		t.Fatalf("settings not restored: %q", b)
	}
	if p, ok := a.ActivePack(); p == nil || p.Name != "Full" || !ok {
		t.Fatalf("active pack: %v %v", p, ok)
	}
	// Vanilla turns the loader off and leaves mods alone.
	pp, _ = a.PlanPack(a.FindPack(VanillaPack))
	if err := a.ApplyPack(ctx, pp, nil); err != nil {
		t.Fatal(err)
	}
	if a.Inv.BepInEx.DoorstopOn || enabledNames(a) != "HandMod,One,Two" {
		t.Fatalf("vanilla: loader %v mods %s", a.Inv.BepInEx.DoorstopOn, enabledNames(a))
	}
	if _, ok := a.ActivePack(); !ok {
		t.Fatal("vanilla should match")
	}
	// Light switches the loader back on and turns One and HandMod off.
	pp, _ = a.PlanPack(a.FindPack("Light"))
	if err := a.ApplyPack(ctx, pp, nil); err != nil {
		t.Fatal(err)
	}
	if !a.Inv.BepInEx.DoorstopOn || enabledNames(a) != "Two" {
		t.Fatalf("light: loader %v mods %s", a.Inv.BepInEx.DoorstopOn, enabledNames(a))
	}
	if _, err := os.Stat(filepath.Join(a.Game.Dir, "BepInEx/.ror2mm/disabled-local/plugins/HandMod/HandMod.dll")); err != nil {
		t.Fatalf("hand-installed mod not parked: %v", err)
	}
	// Rename and delete.
	if err := a.RenamePack("Light", "Quick"); err != nil || a.FindPack("Quick") == nil || a.FindPack("Light") != nil {
		t.Fatalf("rename: %v", err)
	}
	if a.Cfg.Get("active_pack", "") != "Quick" {
		t.Fatal("active pack not renamed")
	}
	if err := a.DeletePack("Quick"); err != nil || a.FindPack("Quick") != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := a.DeletePack(VanillaPack); err == nil {
		t.Fatal("vanilla must not be deletable")
	}
}
