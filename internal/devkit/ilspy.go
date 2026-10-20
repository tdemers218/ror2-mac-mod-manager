package devkit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"ror2mm/internal/config"
)

// ilspyVersion runs on the .NET 8 SDK; newer releases need .NET 9+.
const ilspyVersion = "9.1.0.7988"

func toolDir() string { return filepath.Join(config.CacheDir(), "tools") }

// ILSpy returns the ilspycmd decompiler, installing it into ror2's cache the
// first time. status gets a message if an install happens.
func ILSpy(status func(string)) (string, error) {
	if p, err := exec.LookPath("ilspycmd"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(toolDir(), "ilspycmd"), filepath.Join(home, ".dotnet", "tools", "ilspycmd")} {
		if exists(p) {
			return p, nil
		}
	}
	dn, err := Dotnet()
	if err != nil {
		return "", err
	}
	if status != nil {
		status("Installing the ILSpy decompiler (one time)…")
	}
	out, err := exec.Command(dn, "tool", "install", "ilspycmd", "--version", ilspyVersion, "--tool-path", toolDir()).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("could not install ilspycmd: %s", strings.TrimSpace(string(out)))
	}
	return filepath.Join(toolDir(), "ilspycmd"), nil
}

// ResolveDLL finds an assembly by name in the game, BepInEx core or plugins.
// An empty name means RoR2.dll.
func ResolveDLL(gameDir, name string) (string, error) {
	if name == "" {
		name = "RoR2"
	}
	if filepath.IsAbs(name) && exists(name) {
		return name, nil
	}
	name = strings.TrimSuffix(name, ".dll") + ".dll"
	for _, dir := range []string{
		filepath.Join(gameDir, "Risk of Rain 2_Data", "Managed"),
		filepath.Join(gameDir, "BepInEx", "core"),
	} {
		if p := filepath.Join(dir, name); exists(p) {
			return p, nil
		}
	}
	var found string
	_ = filepath.Walk(filepath.Join(gameDir, "BepInEx", "plugins"), func(p string, info os.FileInfo, err error) error {
		if err == nil && found == "" && strings.EqualFold(info.Name(), name) {
			found = p
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("%s not found in the game, BepInEx core or plugins", name)
	}
	return found, nil
}

// ListTypes lists type names in an assembly that contain pattern (any case).
func ListTypes(gameDir, dll, pattern string, status func(string)) ([]string, error) {
	tool, err := ILSpy(status)
	if err != nil {
		return nil, err
	}
	path, err := ResolveDLL(gameDir, dll)
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(tool, "-l", "cisde", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ilspycmd failed: %w", err)
	}
	pat := strings.ToLower(pattern)
	var res []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// Lines look like "Class RoR2.CharacterBody".
		if i := strings.IndexByte(line, ' '); i > 0 {
			kind, name := line[:i], line[i+1:]
			if pat == "" || strings.Contains(strings.ToLower(name), pat) {
				res = append(res, strings.ToLower(kind)+" "+name)
			}
		}
	}
	sort.Strings(res)
	return res, nil
}

// Decompile returns C# source for one type.
func Decompile(gameDir, dll, typeName string, status func(string)) (string, error) {
	tool, err := ILSpy(status)
	if err != nil {
		return "", err
	}
	path, err := ResolveDLL(gameDir, dll)
	if err != nil {
		return "", err
	}
	if !strings.Contains(typeName, ".") && (dll == "" || strings.EqualFold(dll, "RoR2")) {
		typeName = "RoR2." + typeName
	}
	var stderr bytes.Buffer
	cmd := exec.Command(tool, "-t", typeName, path)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "type not found"
		}
		return "", errors.New(typeName + ": " + msg + " (find names with: ror2 dev types <word>)")
	}
	return string(out), nil
}
