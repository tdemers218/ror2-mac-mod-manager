package thunderstore

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DownloadURL is where Thunderstore serves a package version's zip.
func DownloadURL(owner, name, version string) string {
	return fmt.Sprintf("%s/package/download/%s/%s/%s/", BaseURL, url.PathEscape(owner), url.PathEscape(name), url.PathEscape(version))
}

// Download fetches a package zip into the cache (reusing it if already there)
// and returns its path.
func Download(ctx context.Context, cacheDir string, p *Package, version string, progress Progress) (string, error) {
	dir := filepath.Join(cacheDir, "packages")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, fmt.Sprintf("%s-%s.zip", p.FullName, version))
	if validZip(dest) {
		if progress != nil {
			if fi, err := os.Stat(dest); err == nil {
				progress(fi.Size(), fi.Size())
			}
		}
		return dest, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DownloadURL(p.Owner, p.Name, version), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download of %s failed: %w", p.FullName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download of %s %s failed: %s", p.FullName, version, resp.Status)
	}

	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	total := resp.ContentLength
	if total <= 0 {
		if v := p.Version(version); v != nil {
			total = v.Size
		}
	}
	_, err = io.Copy(f, &countingReader{r: resp.Body, total: total, fn: progress})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("download of %s interrupted: %w", p.FullName, err)
	}
	if !validZip(tmp) {
		os.Remove(tmp)
		return "", fmt.Errorf("download of %s is not a valid zip", p.FullName)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func validZip(path string) bool {
	r, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	r.Close()
	return true
}

// Readme fetches a package version's README as markdown.
func Readme(ctx context.Context, p *Package, version string) (string, error) {
	u := fmt.Sprintf("%s/api/experimental/package/%s/%s/%s/readme/", BaseURL, url.PathEscape(p.Owner), url.PathEscape(p.Name), url.PathEscape(version))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("README not available (%s)", resp.Status)
	}
	var body struct {
		Markdown string `json:"markdown"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Markdown, nil
}

// Lookup turns what a person typed into a package and an optional version.
// It accepts "Owner-Name", "Owner-Name-1.2.3", "Owner/Name", Thunderstore URLs,
// or just a mod name like "LookingGlass".
// When a bare name matches several packages, the best guess comes first in alts.
func (c *Catalog) Lookup(query string) (p *Package, version string, alts []*Package) {
	q := strings.TrimSpace(query)
	if strings.Contains(q, "thunderstore.io") {
		if u, err := url.Parse(q); err == nil {
			parts := []string{}
			for _, s := range strings.Split(u.Path, "/") {
				if s != "" {
					parts = append(parts, s)
				}
			}
			// /c/riskofrain2/p/Owner/Name/ or /package/Owner/Name/
			for i := 0; i+2 < len(parts); i++ {
				if parts[i] == "p" || parts[i] == "package" {
					q = parts[i+1] + "-" + parts[i+2]
					break
				}
			}
		}
	}
	q = strings.ReplaceAll(q, "/", "-")

	if p := c.Get(q); p != nil {
		return p, "", nil
	}
	if full, ver := ParseDep(q); ver != "" {
		if p := c.Get(full); p != nil {
			return p, ver, nil
		}
	}

	// Bare name: exact normalized match first, then prefix matches.
	n := Normalize(q)
	cands := append([]*Package(nil), c.ByNorm(n)...)
	if len(cands) == 0 {
		for _, p := range c.Packages {
			if strings.HasPrefix(p.Norm(), n) && n != "" {
				cands = append(cands, p)
			}
		}
	}
	if len(cands) == 0 {
		return nil, "", nil
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Deprecated != cands[j].Deprecated {
			return !cands[i].Deprecated
		}
		return cands[i].Downloads > cands[j].Downloads
	})
	return cands[0], "", cands
}
