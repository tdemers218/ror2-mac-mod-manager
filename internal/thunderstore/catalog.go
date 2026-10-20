// Package thunderstore talks to the Thunderstore API for Risk of Rain 2.
package thunderstore

import (
	"compress/gzip"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	Community  = "riskofrain2"
	BaseURL    = "https://thunderstore.io"
	packageAPI = BaseURL + "/c/" + Community + "/api/v1/package/"
	userAgent  = "ror2mm/0.1 (+mac)"
	cacheFile  = "catalog.gob"
	cacheVer   = 2
)

// Version is one released version of a package.
type Version struct {
	Number    string
	Deps      []string
	Downloads int
	Created   time.Time
	Size      int64
}

// Package is a Thunderstore package, slimmed down to what the manager needs.
type Package struct {
	Owner       string
	Name        string
	FullName    string
	Description string
	Icon        string
	Website     string
	URL         string
	Categories  []string
	Rating      int
	Deprecated  bool
	NSFW        bool
	Pinned      bool
	Created     time.Time
	Updated     time.Time
	Downloads   int
	Versions    []Version // newest first

	norm string
}

// Latest returns the newest version.
func (p *Package) Latest() *Version {
	if len(p.Versions) == 0 {
		return nil
	}
	return &p.Versions[0]
}

// Version finds a specific version, or nil.
func (p *Package) Version(num string) *Version {
	for i := range p.Versions {
		if p.Versions[i].Number == num {
			return &p.Versions[i]
		}
	}
	return nil
}

// Norm is the package name lowercased with separators removed, for matching.
func (p *Package) Norm() string {
	if p.norm == "" {
		p.norm = Normalize(p.Name)
	}
	return p.norm
}

// Catalog is the full package list for the community.
type Catalog struct {
	Packages     []*Package
	Fetched      time.Time
	LastModified string

	byFull map[string]*Package
	byNorm map[string][]*Package
}

type cacheEnvelope struct {
	Ver          int
	Fetched      time.Time
	LastModified string
	Packages     []*Package
}

func (c *Catalog) index() {
	c.byFull = make(map[string]*Package, len(c.Packages))
	c.byNorm = make(map[string][]*Package)
	for _, p := range c.Packages {
		c.byFull[strings.ToLower(p.FullName)] = p
		n := p.Norm()
		c.byNorm[n] = append(c.byNorm[n], p)
	}
}

// Get finds a package by "Owner-Name" (case-insensitive).
func (c *Catalog) Get(fullName string) *Package {
	return c.byFull[strings.ToLower(fullName)]
}

// ByNorm returns every package whose normalized name equals n.
func (c *Catalog) ByNorm(n string) []*Package { return c.byNorm[n] }

// Age is how long ago the catalog was downloaded.
func (c *Catalog) Age() time.Duration { return time.Since(c.Fetched) }

// Normalize lowercases a name and drops anything that is not a letter or digit,
// so "Risk_Of_Options", "RiskOfOptions" and "risk-of-options" all match.
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LoadCached reads the catalog saved by the last Fetch.
func LoadCached(cacheDir string) (*Catalog, error) {
	f, err := os.Open(filepath.Join(cacheDir, cacheFile))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var env cacheEnvelope
	if err := gob.NewDecoder(f).Decode(&env); err != nil {
		return nil, fmt.Errorf("catalog cache is unreadable: %w", err)
	}
	if env.Ver != cacheVer {
		return nil, errors.New("catalog cache is from an older version")
	}
	c := &Catalog{Packages: env.Packages, Fetched: env.Fetched, LastModified: env.LastModified}
	c.index()
	return c, nil
}

func (c *Catalog) save(cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(cacheDir, cacheFile+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	env := cacheEnvelope{Ver: cacheVer, Fetched: c.Fetched, LastModified: c.LastModified, Packages: c.Packages}
	if err := gob.NewEncoder(f).Encode(&env); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(cacheDir, cacheFile))
}

// Progress reports bytes read so far and the total (total may be 0 if unknown).
type Progress func(read, total int64)

type rawVersion struct {
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	Icon          string    `json:"icon"`
	VersionNumber string    `json:"version_number"`
	Dependencies  []string  `json:"dependencies"`
	Downloads     int       `json:"downloads"`
	DateCreated   time.Time `json:"date_created"`
	WebsiteURL    string    `json:"website_url"`
	FileSize      int64     `json:"file_size"`
}

type rawPackage struct {
	Name        string       `json:"name"`
	FullName    string       `json:"full_name"`
	Owner       string       `json:"owner"`
	PackageURL  string       `json:"package_url"`
	DateCreated time.Time    `json:"date_created"`
	DateUpdated time.Time    `json:"date_updated"`
	Rating      int          `json:"rating_score"`
	Pinned      bool         `json:"is_pinned"`
	Deprecated  bool         `json:"is_deprecated"`
	NSFW        bool         `json:"has_nsfw_content"`
	Categories  []string     `json:"categories"`
	Versions    []rawVersion `json:"versions"`
}

type countingReader struct {
	r     io.Reader
	read  int64
	total int64
	fn    Progress
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	if c.fn != nil {
		c.fn(c.read, c.total)
	}
	return n, err
}

var httpClient = &http.Client{Timeout: 5 * time.Minute}

// Fetch downloads the catalog (or confirms the cached one is current) and caches it.
// If prev is non-nil and the server says nothing changed, prev is returned with a fresh timestamp.
func Fetch(ctx context.Context, cacheDir string, prev *Catalog, progress Progress) (*Catalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, packageAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	// Asking for gzip ourselves means we see compressed byte counts for progress.
	req.Header.Set("Accept-Encoding", "gzip")
	if prev != nil && prev.LastModified != "" {
		req.Header.Set("If-Modified-Since", prev.LastModified)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Thunderstore: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && prev != nil {
		// Copy rather than touch prev, which the UI may be reading.
		c := *prev
		c.Fetched = time.Now()
		_ = c.save(cacheDir)
		return &c, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Thunderstore answered %s", resp.Status)
	}

	var body io.Reader = &countingReader{r: resp.Body, total: resp.ContentLength, fn: progress}
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		body = gz
	}

	var raws []rawPackage
	if err := json.NewDecoder(body).Decode(&raws); err != nil {
		return nil, fmt.Errorf("could not read the Thunderstore catalog: %w", err)
	}

	c := &Catalog{Fetched: time.Now(), LastModified: resp.Header.Get("Last-Modified")}
	c.Packages = make([]*Package, 0, len(raws))
	for _, r := range raws {
		if len(r.Versions) == 0 {
			continue
		}
		p := &Package{
			Owner: r.Owner, Name: r.Name, FullName: r.FullName, URL: r.PackageURL,
			Categories: r.Categories, Rating: r.Rating, Deprecated: r.Deprecated,
			NSFW: r.NSFW, Pinned: r.Pinned, Created: r.DateCreated, Updated: r.DateUpdated,
		}
		latest := r.Versions[0]
		p.Description = latest.Description
		p.Icon = latest.Icon
		p.Website = latest.WebsiteURL
		p.Versions = make([]Version, len(r.Versions))
		for i, v := range r.Versions {
			p.Versions[i] = Version{Number: v.VersionNumber, Deps: v.Dependencies, Downloads: v.Downloads, Created: v.DateCreated, Size: v.FileSize}
			p.Downloads += v.Downloads
		}
		c.Packages = append(c.Packages, p)
	}
	c.index()
	if err := c.save(cacheDir); err != nil {
		return c, fmt.Errorf("catalog downloaded but could not be cached: %w", err)
	}
	return c, nil
}

// SortMode orders search results.
type SortMode int

const (
	SortRelevance SortMode = iota
	SortDownloads
	SortUpdated
	SortRating
	SortName
	SortCreated
	SortSize
)

var sortNames = [...]string{"relevance", "downloads", "updated", "rating", "name", "created", "size"}

func (s SortMode) String() string {
	if int(s) < len(sortNames) {
		return sortNames[s]
	}
	return "downloads"
}

// ParseSortMode turns a name from String back into a SortMode.
func ParseSortMode(name string) (SortMode, bool) {
	for i, n := range sortNames {
		if n == name {
			return SortMode(i), true
		}
	}
	return SortDownloads, false
}

// SortKey is one level of a multi-level sort. By default every mode puts the
// "best" first (most downloads, newest, highest rated, A to Z, biggest);
// Reverse flips that.
type SortKey struct {
	Mode    SortMode
	Reverse bool
}

// SearchOptions filters a search.
type SearchOptions struct {
	Sort SortMode
	// Sorts, when set, replaces Sort: ties on the first key are broken by the next.
	Sorts             []SortKey
	IncludeDeprecated bool
	IncludeNSFW       bool
	Category          string
	// OnlyCategories keeps packages in at least one of these categories.
	OnlyCategories []string
	// HideCategories drops packages in any of these categories.
	HideCategories []string
	// HideLibraries skips packages tagged "Libraries" (APIs other mods need).
	HideLibraries bool
	// UpdatedAfter, when set, drops packages last updated before it.
	UpdatedAfter time.Time
	// Exclude drops any package it returns true for.
	Exclude func(*Package) bool
}

// Search finds packages matching every word in q (name, owner, description).
// With an empty query it lists everything in the chosen order.
func (c *Catalog) Search(q string, opt SearchOptions) []*Package {
	words := strings.Fields(strings.ToLower(q))
	type hit struct {
		p     *Package
		score int
	}
	onlyLibs := opt.Category == "Libraries" || containsFold(opt.OnlyCategories, "Libraries")
	hits := make([]hit, 0, 256)
	for _, p := range c.Packages {
		if p.Deprecated && !opt.IncludeDeprecated {
			continue
		}
		if p.NSFW && !opt.IncludeNSFW {
			continue
		}
		if opt.Category != "" && !hasCategory(p, opt.Category) {
			continue
		}
		if len(opt.OnlyCategories) > 0 && !hasAnyCategory(p, opt.OnlyCategories) {
			continue
		}
		if hasAnyCategory(p, opt.HideCategories) {
			continue
		}
		if opt.HideLibraries && !onlyLibs && hasCategory(p, "Libraries") {
			continue
		}
		if !opt.UpdatedAfter.IsZero() && p.Updated.Before(opt.UpdatedAfter) {
			continue
		}
		if opt.Exclude != nil && opt.Exclude(p) {
			continue
		}
		score, ok := matchScore(p, words)
		if !ok {
			continue
		}
		hits = append(hits, hit{p, score})
	}
	keys := opt.Sorts
	if len(keys) == 0 {
		keys = []SortKey{{Mode: opt.Sort}}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		for _, k := range keys {
			var c int
			if k.Mode == SortRelevance {
				if len(words) == 0 {
					continue // nothing to rank by without a query
				}
				c = cmpInt(a.score, b.score)
			} else {
				c = Compare(a.p, b.p, k.Mode)
			}
			if k.Reverse {
				c = -c
			}
			if c != 0 {
				return c < 0
			}
		}
		// Final tie-breakers keep the order stable and sensible.
		if c := Compare(a.p, b.p, SortDownloads); c != 0 {
			return c < 0
		}
		return Compare(a.p, b.p, SortName) < 0
	})
	out := make([]*Package, len(hits))
	for i, h := range hits {
		out[i] = h.p
	}
	return out
}

// Compare orders two packages by one mode, best first: it is negative when a
// should come before b.
func Compare(a, b *Package, mode SortMode) int {
	switch mode {
	case SortDownloads:
		return cmpInt(a.Downloads, b.Downloads)
	case SortUpdated:
		return cmpTime(a.Updated, b.Updated)
	case SortCreated:
		return cmpTime(a.Created, b.Created)
	case SortRating:
		return cmpInt(a.Rating, b.Rating)
	case SortSize:
		return cmpInt(int(pkgSize(a)), int(pkgSize(b)))
	case SortName:
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	}
	return 0
}

func pkgSize(p *Package) int64 {
	if l := p.Latest(); l != nil {
		return l.Size
	}
	return 0
}

// cmpInt puts the larger value first.
func cmpInt(a, b int) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	}
	return 0
}

// cmpTime puts the later time first.
func cmpTime(a, b time.Time) int {
	switch {
	case a.After(b):
		return -1
	case a.Before(b):
		return 1
	}
	return 0
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func hasAnyCategory(p *Package, cats []string) bool {
	for _, c := range cats {
		if hasCategory(p, c) {
			return true
		}
	}
	return false
}

func hasCategory(p *Package, cat string) bool {
	for _, c := range p.Categories {
		if strings.EqualFold(c, cat) {
			return true
		}
	}
	return false
}

func matchScore(p *Package, words []string) (int, bool) {
	if len(words) == 0 {
		return 0, true
	}
	name := strings.ToLower(p.Name)
	norm := p.Norm()
	owner := strings.ToLower(p.Owner)
	desc := strings.ToLower(p.Description)
	score := 0
	for _, w := range words {
		nw := Normalize(w)
		switch {
		case norm == nw:
			score += 100
		case strings.HasPrefix(norm, nw):
			score += 60
		case strings.Contains(name, w) || (nw != "" && strings.Contains(norm, nw)):
			score += 40
		case strings.Contains(owner, w):
			score += 20
		case strings.Contains(desc, w):
			score += 5
		default:
			return 0, false
		}
	}
	return score, true
}

// Categories lists every category in use, most common first.
func (c *Catalog) Categories() []string {
	counts := map[string]int{}
	for _, p := range c.Packages {
		for _, cat := range p.Categories {
			counts[cat]++
		}
	}
	out := make([]string, 0, len(counts))
	for k := range counts {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if counts[out[i]] != counts[out[j]] {
			return counts[out[i]] > counts[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// ParseDep splits "Owner-Name-1.2.3" into "Owner-Name" and "1.2.3".
func ParseDep(dep string) (fullName, version string) {
	i := strings.LastIndex(dep, "-")
	if i <= 0 {
		return dep, ""
	}
	return dep[:i], dep[i+1:]
}

// SplitFullName splits "Owner-Name" into owner and name.
func SplitFullName(full string) (owner, name string) {
	i := strings.Index(full, "-")
	if i <= 0 {
		return "", full
	}
	return full[:i], full[i+1:]
}
