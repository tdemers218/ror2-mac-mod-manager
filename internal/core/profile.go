package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	ts "ror2mm/internal/thunderstore"
)

// ProfileMod is one mod listed in an r2modman profile.
type ProfileMod struct {
	FullName string
	Version  string
	Enabled  bool
}

// Profile is an r2modman / Thunderstore Mod Manager profile export.
type Profile struct {
	Name    string
	Mods    []ProfileMod
	Configs map[string][]byte // path relative to BepInEx/config -> contents
}

type r2x struct {
	ProfileName string `yaml:"profileName"`
	Mods        []struct {
		Name    string `yaml:"name"`
		Version struct {
			Major int `yaml:"major"`
			Minor int `yaml:"minor"`
			Patch int `yaml:"patch"`
		} `yaml:"version"`
		Enabled *bool `yaml:"enabled"`
	} `yaml:"mods"`
}

// LoadProfile reads an .r2z file or fetches a profile code from Thunderstore.
func LoadProfile(ctx context.Context, src string) (*Profile, error) {
	var data []byte
	if fi, err := os.Stat(src); err == nil && !fi.IsDir() {
		data, err = os.ReadFile(src)
		if err != nil {
			return nil, err
		}
	} else {
		code := strings.TrimSpace(src)
		u := ts.BaseURL + "/api/experimental/legacyprofile/get/" + code + "/"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("could not fetch profile code: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("profile code %q not found (%s)", code, resp.Status)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		s := strings.TrimSpace(string(body))
		s = strings.TrimPrefix(s, "#r2modman")
		data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("profile code did not contain a profile: %w", err)
		}
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a profile archive: %w", err)
	}
	p := &Profile{Configs: map[string][]byte{}}
	found := false
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(rc, 32<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(name, "export.r2x") {
			var x r2x
			if err := yaml.Unmarshal(content, &x); err != nil {
				return nil, fmt.Errorf("profile list is unreadable: %w", err)
			}
			found = true
			p.Name = x.ProfileName
			for _, m := range x.Mods {
				en := true
				if m.Enabled != nil {
					en = *m.Enabled
				}
				p.Mods = append(p.Mods, ProfileMod{
					FullName: m.Name,
					Version:  fmt.Sprintf("%d.%d.%d", m.Version.Major, m.Version.Minor, m.Version.Patch),
					Enabled:  en,
				})
			}
			continue
		}
		clean := path.Clean(name)
		if strings.HasPrefix(clean, "../") {
			continue
		}
		clean = strings.TrimPrefix(clean, "BepInEx/")
		if strings.HasPrefix(clean, "config/") {
			p.Configs[strings.TrimPrefix(clean, "config/")] = content
		}
	}
	if !found {
		return nil, fmt.Errorf("archive has no export.r2x; is it an r2modman profile?")
	}
	return p, nil
}

// PlanProfile plans installing every mod in a profile at its listed version
// (falling back to the newest when that version is gone).
func (a *App) PlanProfile(p *Profile) (*Plan, error) {
	var reqs []Request
	var missing []string
	for _, m := range p.Mods {
		if isBepInExPack(m.FullName) {
			reqs = append(reqs, Request{FullName: m.FullName})
			continue
		}
		pkg := a.Cat.Get(m.FullName)
		if pkg == nil {
			missing = append(missing, m.FullName)
			continue
		}
		v := m.Version
		if pkg.Version(v) == nil {
			v = ""
		}
		reqs = append(reqs, Request{FullName: pkg.FullName, Version: v})
	}
	plan, err := a.PlanInstall(reqs)
	if err != nil {
		return nil, err
	}
	for _, m := range missing {
		plan.Warnings = append(plan.Warnings, m+" is no longer on Thunderstore; skipped")
	}
	return plan, nil
}

// ApplyProfileExtras disables mods the profile had off and optionally writes its configs.
func (a *App) ApplyProfileExtras(p *Profile, writeConfigs bool) (int, error) {
	var off []string
	for _, m := range p.Mods {
		if !m.Enabled && a.Game.Get(m.FullName) != nil {
			off = append(off, m.FullName)
		}
	}
	if len(off) > 0 {
		if err := a.SetEnabled(off, false); err != nil {
			return 0, err
		}
	}
	n := 0
	if writeConfigs {
		for rel, data := range p.Configs {
			dest := a.Game.Dir + "/BepInEx/config/" + rel
			if err := os.MkdirAll(path.Dir(dest), 0o755); err != nil {
				return n, err
			}
			if err := os.WriteFile(dest, data, 0o644); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}
