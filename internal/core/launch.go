package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ror2mm/internal/config"
)

// SteamRunning reports whether Windows Steam is running inside the configured wrapper.
func (a *App) SteamRunning() bool {
	w := a.Cfg.WrapperApp
	if w == "" {
		return false
	}
	// The server shows up as ".../wine/lib/wine/../../bin/wineserver", so match
	// "<wrapper>/Contents/SharedSupport/wine/ ... wineserver" loosely.
	server := regexp.QuoteMeta(filepath.Join(w, "Contents", "SharedSupport", "wine")) + ".*wineserver"
	if exec.Command("pgrep", "-f", server).Run() != nil {
		return false
	}
	return exec.Command("pgrep", "-if", `\\steam\.exe`).Run() == nil
}

// GameRunning reports whether Risk of Rain 2 is running. Under Wine the
// process shows as a Windows path (Z:\...\Risk of Rain 2.exe) or the bare exe
// name; anything else that merely mentions the file name does not count.
func GameRunning() bool {
	return exec.Command("pgrep", "-f", `(^|\\)Risk of Rain 2\.exe`).Run() == nil
}

// Launch starts the game through Steam in the wrapper. If Steam is not running
// it opens the wrapper first and waits for Steam (up to about two minutes).
// status is called with short progress messages.
func (a *App) Launch(status func(string)) error {
	if status == nil {
		status = func(string) {}
	}
	w := a.Cfg.WrapperApp
	if w == "" {
		return errors.New("no Sikarugir wrapper set (ror2 config set wrapper <path to .app>)")
	}
	if _, err := os.Stat(w); err != nil {
		return fmt.Errorf("wrapper not found: %s", w)
	}
	if GameRunning() {
		return errors.New("Risk of Rain 2 is already running")
	}
	if !a.SteamRunning() {
		status("Opening " + strings.TrimSuffix(filepath.Base(w), ".app") + "…")
		if out, err := exec.Command("open", "-a", w).CombinedOutput(); err != nil {
			return fmt.Errorf("could not open the wrapper: %s", strings.TrimSpace(string(out)))
		}
		status("Waiting for Steam to start…")
		deadline := time.Now().Add(2 * time.Minute)
		for !a.SteamRunning() {
			if time.Now().After(deadline) {
				return errors.New("Steam did not start in time; press Play in Steam instead")
			}
			time.Sleep(2 * time.Second)
		}
		// Steam needs a moment after the process appears before it accepts commands.
		status("Steam is up, giving it a moment to log in…")
		time.Sleep(20 * time.Second)
	}
	status("Asking Steam to start Risk of Rain 2…")
	if err := a.sendAppLaunch(); err != nil {
		return err
	}
	// Steam may show a dialog (update, cloud sync) first; give it a while.
	status("Waiting for the game to appear…")
	deadline := time.Now().Add(90 * time.Second)
	for !GameRunning() {
		if time.Now().After(deadline) {
			return fmt.Errorf("Steam got the request but the game did not start. Check Steam for a dialog, or press Play there. (Details: %s)", config.ShortPath(launchLog()))
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func launchLog() string { return filepath.Join(config.CacheDir(), "launch.log") }

// sendAppLaunch runs a second steam.exe in the same Wine prefix; it hands
// "-applaunch" to the Steam that is already running and exits.
func (a *App) sendAppLaunch() error {
	w := a.Cfg.WrapperApp
	_ = os.MkdirAll(config.CacheDir(), 0o755)
	logf, err := os.Create(launchLog())
	if err != nil {
		return err
	}
	cmd := exec.Command(config.WineBin(w), `C:\Program Files (x86)\Steam\steam.exe`, "-applaunch", config.AppID)
	cmd.Env = append(os.Environ(),
		"WINEPREFIX="+config.Prefix(w),
		"WINEDEBUG=-all",
		"WINEESYNC=1",
		"WINEMSYNC=1",
	)
	cmd.Stdout, cmd.Stderr = logf, logf
	fmt.Fprintf(logf, "%s\n%s %v\n\n", time.Now().Format(time.RFC3339), cmd.Path, cmd.Args[1:])
	if err := cmd.Start(); err != nil {
		logf.Close()
		return fmt.Errorf("could not run Wine: %w", err)
	}
	go func() { cmd.Wait(); logf.Close() }()
	return nil
}
