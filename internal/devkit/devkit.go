// Package devkit creates, builds, deploys and packages your own Risk of Rain 2
// mods, and sets them up for AI coding agents (Claude Code, Codex).
package devkit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode"

	"ror2mm/internal/config"
)

//go:embed tmpl
var tmplFS embed.FS

// Template is a starting point for a new mod.
type Template struct {
	ID    string
	Label string
	About string
	Deps  []string // Thunderstore packages the template needs (besides BepInExPack)
	Refs  []string // plugin DLLs to reference, found under BepInEx/plugins
}

// Templates lists the starting points.
var Templates = []Template{
	{ID: "blank", Label: "Blank plugin",
		About: "An empty plugin with logging and one setting. Pick this to start from your description alone."},
	{ID: "tweak", Label: "Gameplay tweak",
		About: "Hooks into the game to change how something works, with a setting players can adjust. Starts as: bonus gold for every kill.",
		Deps:  []string{"RiskofThunder-HookGenPatcher"}},
	{ID: "item", Label: "New item",
		About: "Adds a custom item through R2API. Starts as: a white item that gives +10% movement speed per stack.",
		Deps:  []string{"RiskofThunder-R2API_Items", "RiskofThunder-R2API_Language", "RiskofThunder-R2API_RecalculateStats"},
		Refs:  []string{"R2API.Core", "R2API.Items", "R2API.Language", "R2API.RecalculateStats", "R2API.ContentManagement"}},
}

// TemplateByID finds a template (blank if unknown).
func TemplateByID(id string) Template {
	for _, t := range Templates {
		if t.ID == id {
			return t
		}
	}
	return Templates[0]
}

// Game assemblies every project references (when present in the install).
var managed = []string{
	"RoR2", "UnityEngine", "UnityEngine.CoreModule", "UnityEngine.PhysicsModule", "UnityEngine.AnimationModule",
	"UnityEngine.UI", "UnityEngine.UIModule", "UnityEngine.InputLegacyModule", "UnityEngine.AssetBundleModule",
	"UnityEngine.AudioModule", "UnityEngine.ParticleSystemModule", "UnityEngine.TextRenderingModule",
	"Unity.Addressables", "Unity.ResourceManager", "Unity.TextMeshPro", "LegacyResourcesAPI",
	"KinematicCharacterController", "com.unity.multiplayer-hlapi.Runtime", "HGCSharpUtils", "Rewired_Core", "Zio",
}

// Meta is stored in ror2mod.json so the manager recognizes a project.
type Meta struct {
	Name        string    `json:"name"`
	Author      string    `json:"author"`
	Description string    `json:"description"`
	Template    string    `json:"template"`
	Created     time.Time `json:"created"`
	// When each AI agent was last opened here (used to resume conversations).
	AgentSessions map[string]time.Time `json:"agent_sessions,omitempty"`
	// The Claude Code conversation for this project. Resumed by ID because
	// Claude's --continue skips sessions started by another program.
	ClaudeSession string `json:"claude_session,omitempty"`
}

// Project is a mod you are making.
type Project struct {
	Meta
	Dir     string
	Version string
}

const metaFile = "ror2mod.json"

// Workspace is the folder new mod projects go in.
func Workspace(cfg *config.Config) string {
	if ws := cfg.Get("workspace", ""); ws != "" {
		return ws
	}
	// Next to the game's drive if it lives on another volume, else in home.
	if strings.HasPrefix(cfg.GameDir, "/Volumes/") {
		parts := strings.SplitN(cfg.GameDir, "/", 4)
		if len(parts) >= 3 {
			return filepath.Join("/", parts[1], parts[2], "RoR2Mods")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "RoR2Mods")
}

// List finds projects in the workspace, newest first.
func List(ws string) []*Project {
	entries, _ := os.ReadDir(ws)
	var out []*Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if p, err := Load(filepath.Join(ws, e.Name())); err == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified().After(out[j].Modified()) })
	return out
}

// Load reads a project folder.
func Load(dir string) (*Project, error) {
	data, err := os.ReadFile(filepath.Join(dir, metaFile))
	if err != nil {
		return nil, err
	}
	p := &Project{Dir: dir}
	if err := json.Unmarshal(data, &p.Meta); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", metaFile, err)
	}
	if m, err := p.Manifest(); err == nil {
		p.Version = m.Version
	}
	return p, nil
}

// Find returns the project containing dir (walking up), for commands run inside one.
func Find(dir string) (*Project, error) {
	d, _ := filepath.Abs(dir)
	for {
		if _, err := os.Stat(filepath.Join(d, metaFile)); err == nil {
			return Load(d)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return nil, errors.New("not inside a mod project (no ror2mod.json here or above); pass a project name")
		}
		d = parent
	}
}

// Modified is when the project's code last changed.
func (p *Project) Modified() time.Time {
	var t time.Time
	_ = filepath.WalkDir(p.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "bin" || d.Name() == "obj" || d.Name() == ".git" || d.Name() == "build") {
			return filepath.SkipDir
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(t) {
			t = info.ModTime()
		}
		return nil
	})
	return t
}

// Manifest is Thunderstore's manifest.json.
type Manifest struct {
	Name         string   `json:"name"`
	Version      string   `json:"version_number"`
	Website      string   `json:"website_url"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
}

// Manifest reads manifest.json.
func (p *Project) Manifest() (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(p.Dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &m); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	return &m, nil
}

// DLL is where the built plugin ends up.
func (p *Project) DLL() string { return filepath.Join(p.Dir, "bin", "Release", p.Name+".dll") }

// Built reports whether a DLL exists and when it was built.
func (p *Project) Built() (time.Time, bool) {
	fi, err := os.Stat(p.DLL())
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

// DevFolder is the plugin folder name used when deploying into the game.
func (p *Project) DevFolder() string { return "dev-" + p.Author + "-" + p.Name }

func (p *Project) devPath(gameDir string) string {
	return filepath.Join(gameDir, "BepInEx", "plugins", p.DevFolder())
}

// Deployed reports whether the dev copy is in the game and when it was copied.
func (p *Project) Deployed(gameDir string) (time.Time, bool) {
	fi, err := os.Stat(filepath.Join(p.devPath(gameDir), p.Name+".dll"))
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

// ---------- creating ----------

var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{1,63}$`)

// ValidName checks a mod or author name against Thunderstore and C# rules.
func ValidName(kind, s string) error {
	if !nameRe.MatchString(s) {
		return fmt.Errorf("%s must start with a letter and use only letters, digits and _ (2-64 characters)", kind)
	}
	return nil
}

// NewOptions describes a new project.
type NewOptions struct {
	Name        string
	Author      string
	Description string
	Template    string
	GameDir     string
	// Dependency strings with versions for manifest.json, e.g. bbepis-BepInExPack-5.4.2121.
	Deps []string
}

type tmplData struct {
	Name, Author, GUID, Version, Description, Title, Upper, GameDir string
	TemplateLabel, SkillsDir                                        string
	Managed, Refs                                                   []string
}

// New creates a project folder, sets up git, and returns it.
func New(ws string, o NewOptions) (*Project, error) {
	if err := ValidName("Mod name", o.Name); err != nil {
		return nil, err
	}
	if err := ValidName("Author", o.Author); err != nil {
		return nil, err
	}
	if strings.TrimSpace(o.Description) == "" {
		o.Description = "A Risk of Rain 2 mod."
	}
	dir := filepath.Join(ws, o.Name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%s already exists", dir)
	}
	t := TemplateByID(o.Template)
	d := tmplData{
		Name: o.Name, Author: o.Author, Version: "0.1.0",
		GUID:          "com." + strings.ToLower(o.Author) + "." + strings.ToLower(o.Name),
		Description:   strings.TrimSpace(o.Description),
		Title:         titleCase(o.Name),
		Upper:         strings.ToUpper(o.Name),
		GameDir:       o.GameDir,
		TemplateLabel: t.Label,
		Managed:       managed, Refs: t.Refs,
	}
	files := map[string]string{
		o.Name + ".csproj": "project.csproj.tmpl",
		"Plugin.cs":        "plugin_" + t.ID + ".cs.tmpl",
		"README.md":        "readme.md.tmpl",
		"CHANGELOG.md":     "changelog.md.tmpl",
		".gitignore":       "gitignore.tmpl",
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cleanup := func(err error) (*Project, error) {
		os.RemoveAll(dir)
		return nil, err
	}
	for out, name := range files {
		if err := render(filepath.Join(dir, out), name, d); err != nil {
			return cleanup(err)
		}
	}
	// Agent instructions: Claude Code reads CLAUDE.md and .claude/skills,
	// Codex reads AGENTS.md and .agents/skills.
	for out, skills := range map[string]string{"CLAUDE.md": ".claude/skills", "AGENTS.md": ".agents/skills"} {
		dd := d
		dd.SkillsDir = skills
		if err := render(filepath.Join(dir, out), "agent.md.tmpl", dd); err != nil {
			return cleanup(err)
		}
		if err := writeSkills(filepath.Join(dir, skills), d); err != nil {
			return cleanup(err)
		}
	}
	// C# string literals in the template take the description as-is; keep it safe.
	desc := d.Description
	if len(desc) > 250 {
		desc = desc[:247] + "..."
	}
	man := Manifest{Name: o.Name, Version: "0.1.0", Website: "", Description: desc, Dependencies: o.Deps}
	if man.Dependencies == nil {
		man.Dependencies = []string{}
	}
	if err := writeJSON(filepath.Join(dir, "manifest.json"), man); err != nil {
		return cleanup(err)
	}
	meta := Meta{Name: o.Name, Author: o.Author, Description: d.Description, Template: t.ID, Created: time.Now()}
	if err := writeJSON(filepath.Join(dir, metaFile), meta); err != nil {
		return cleanup(err)
	}
	claudeSettings := map[string]any{"permissions": map[string]any{"allow": AllowedCommands}}
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		return cleanup(err)
	}
	if err := writeJSON(filepath.Join(dir, ".claude", "settings.json"), claudeSettings); err != nil {
		return cleanup(err)
	}
	if err := writeIcon(filepath.Join(dir, "icon.png"), o.Name); err != nil {
		return cleanup(err)
	}
	gitInit(dir)
	return Load(dir)
}

func render(dest, name string, d tmplData) error {
	src, err := tmplFS.ReadFile("tmpl/" + name)
	if err != nil {
		return err
	}
	return renderBytes(dest, name, src, d)
}

func renderBytes(dest, name string, src []byte, d tmplData) error {
	t, err := template.New(name).Parse(string(src))
	if err != nil {
		return err
	}
	if strings.HasSuffix(name, ".cs.tmpl") {
		d.Description = csEscape(d.Description)
		d.Title = csEscape(d.Title)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, buf.Bytes(), 0o644)
}

func writeSkills(dest string, d tmplData) error {
	return fs.WalkDir(tmplFS, "tmpl/skills", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		src, err := tmplFS.ReadFile(path)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "tmpl/skills/")
		return renderBytes(filepath.Join(dest, rel), rel, src, d)
	})
}

func csEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return strings.ReplaceAll(s, "\n", " ")
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// titleCase turns "MoveFaster" or "move_faster" into "Move Faster".
func titleCase(s string) string {
	var b strings.Builder
	prev := ' '
	for i, r := range s {
		if r == '_' {
			b.WriteRune(' ')
			prev = ' '
			continue
		}
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(prev) {
			b.WriteRune(' ')
		}
		if prev == ' ' {
			r = unicode.ToUpper(r)
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

// writeIcon makes a 256x256 placeholder icon (Thunderstore needs exactly that size).
func writeIcon(path, seed string) error {
	const n = 256
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	a := color.NRGBA{0xC7, 0x7D, 0xFF, 0xFF}
	b := color.NRGBA{0xFF, 0x9F, 0x43, 0xFF}
	// Rotate the gradient a little per mod so icons differ.
	h := 0
	for _, r := range seed {
		h = h*31 + int(r)
	}
	angle := float64(h%360) * math.Pi / 180
	dx, dy := math.Cos(angle), math.Sin(angle)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)/n-0.5, float64(y)/n-0.5
			t := math.Max(0, math.Min(1, fx*dx+fy*dy+0.5))
			c := color.NRGBA{
				uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
				uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
				uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t), 0xFF}
			// A dark diamond in the middle, like the mod manager's logo.
			if math.Abs(fx)+math.Abs(fy) < 0.28 {
				c = color.NRGBA{0x15, 0x12, 0x1C, 0xFF}
			} else if math.Abs(fx)+math.Abs(fy) < 0.31 {
				c = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func gitInit(dir string) {
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	run := func(args ...string) error {
		c := exec.Command("git", args...)
		c.Dir = dir
		return c.Run()
	}
	if run("init", "-q") != nil {
		return
	}
	_ = run("add", "-A")
	_ = run("commit", "-q", "-m", "New mod from the ror2 template")
}

// ---------- building ----------

// Dotnet finds the dotnet command.
func Dotnet() (string, error) {
	if p, err := exec.LookPath("dotnet"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/usr/local/share/dotnet/dotnet", "/opt/homebrew/bin/dotnet"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("the .NET SDK is not installed (get it with: brew install --cask dotnet-sdk)")
}

// Build compiles the project in Release mode, streaming output to out.
func (p *Project) Build(ctx context.Context, gameDir string, out io.Writer) error {
	dn, err := Dotnet()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, dn, "build", "-c", "Release", "-nologo", "-v", "q", "-clp:NoSummary", "-p:GameDir="+gameDir)
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), "DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1")
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed")
	}
	if _, ok := p.Built(); !ok {
		return fmt.Errorf("build finished but %s is missing", p.DLL())
	}
	return nil
}

// Deploy copies the built plugin into the game as a dev mod.
func (p *Project) Deploy(gameDir string) (string, error) {
	if _, ok := p.Built(); !ok {
		return "", errors.New("build it first")
	}
	dest := p.devPath(gameDir)
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	copies := map[string]string{p.DLL(): p.Name + ".dll"}
	if pdb := strings.TrimSuffix(p.DLL(), ".dll") + ".pdb"; exists(pdb) {
		copies[pdb] = p.Name + ".pdb"
	}
	for _, f := range []string{"manifest.json", "icon.png", "README.md"} {
		if exists(filepath.Join(p.Dir, f)) {
			copies[filepath.Join(p.Dir, f)] = f
		}
	}
	for src, name := range copies {
		if err := copyFile(src, filepath.Join(dest, name)); err != nil {
			return "", err
		}
	}
	return dest, nil
}

// Undeploy removes the dev copy from the game.
func (p *Project) Undeploy(gameDir string) error { return os.RemoveAll(p.devPath(gameDir)) }

// Package builds the Thunderstore upload zip and returns its path.
func (p *Project) Package() (string, error) {
	m, err := p.Manifest()
	if err != nil {
		return "", err
	}
	var problems []string
	if err := ValidName("Manifest name", m.Name); err != nil {
		problems = append(problems, err.Error())
	}
	if len(m.Description) > 250 {
		problems = append(problems, "manifest description is longer than 250 characters")
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(m.Version) {
		problems = append(problems, "manifest version_number must look like 1.2.3")
	}
	if w, h, err := pngSize(filepath.Join(p.Dir, "icon.png")); err != nil || w != 256 || h != 256 {
		problems = append(problems, "icon.png must be a 256x256 PNG")
	}
	if !exists(filepath.Join(p.Dir, "README.md")) {
		problems = append(problems, "README.md is missing")
	}
	if _, ok := p.Built(); !ok {
		problems = append(problems, "the plugin is not built")
	}
	if len(problems) > 0 {
		return "", errors.New(strings.Join(problems, "; "))
	}
	if err := os.MkdirAll(filepath.Join(p.Dir, "build"), 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(p.Dir, "build", fmt.Sprintf("%s-%s-%s.zip", p.Author, m.Name, m.Version))
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	add := func(src, name string) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	}
	entries := [][2]string{
		{filepath.Join(p.Dir, "manifest.json"), "manifest.json"},
		{filepath.Join(p.Dir, "icon.png"), "icon.png"},
		{filepath.Join(p.Dir, "README.md"), "README.md"},
		{p.DLL(), "plugins/" + p.Name + "/" + p.Name + ".dll"},
	}
	if exists(filepath.Join(p.Dir, "CHANGELOG.md")) {
		entries = append(entries, [2]string{filepath.Join(p.Dir, "CHANGELOG.md"), "CHANGELOG.md"})
	}
	for _, e := range entries {
		if err := add(e[0], e[1]); err != nil {
			zw.Close()
			f.Close()
			return "", err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return "", err
	}
	return out, f.Close()
}

func pngSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	return cfg.Width, cfg.Height, err
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ---------- AI agents ----------

// Agent is an AI coding assistant that can be opened in a project.
type Agent struct {
	ID    string
	Name  string
	Cmd   string
	About string
}

// Agents lists supported assistants.
var Agents = []Agent{
	{ID: "claude", Name: "Claude Code", Cmd: "claude", About: "Anthropic's coding agent. Reads CLAUDE.md and .claude/skills."},
	{ID: "codex", Name: "Codex", Cmd: "codex", About: "OpenAI's coding agent. Reads AGENTS.md and .agents/skills."},
}

// AgentPath finds an agent's command, or "".
func AgentPath(a Agent) string {
	if p, err := exec.LookPath(a.Cmd); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".local", "bin", a.Cmd), "/usr/local/bin/" + a.Cmd, "/opt/homebrew/bin/" + a.Cmd} {
		if exists(p) {
			return p
		}
	}
	return ""
}

// AllowedCommands are the shell commands agents may run without asking.
// They are written to .claude/settings.json and also passed on the command
// line, because Claude Code ignores project settings in folders it has not
// been explicitly asked to trust (which happens when a parent folder is trusted).
var AllowedCommands = []string{
	"Bash(ror2 dev:*)", "Bash(ror2 doctor)", "Bash(ror2 info:*)", "Bash(ror2 search:*)", "Bash(ror2 list)",
	"Bash(dotnet build:*)", "Bash(git status)", "Bash(git diff:*)", "Bash(git add:*)", "Bash(git commit:*)", "Bash(git log:*)",
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

func claudeSessionDir(p *Project) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects", nonAlnum.ReplaceAllString(p.Dir, "-"))
}

// claudeSession returns the conversation to resume: the one ror2 recorded,
// or (for older projects) the newest one in the folder.
func claudeSession(p *Project) string {
	dir := claudeSessionDir(p)
	if p.ClaudeSession != "" && exists(filepath.Join(dir, p.ClaudeSession+".jsonl")) {
		return p.ClaudeSession
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var newest string
	var t time.Time
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil && fi.ModTime().After(t) {
			newest, t = f, fi.ModTime()
		}
	}
	return strings.TrimSuffix(filepath.Base(newest), ".jsonl")
}

// HasSession reports whether the agent has a previous conversation in this project.
func HasSession(a Agent, p *Project) bool {
	if a.ID == "claude" {
		return claudeSession(p) != ""
	}
	_, ok := p.AgentSessions[a.ID]
	return ok
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// AgentCommand builds the command that opens an agent in the project.
// With resume, it continues the project's conversation (if any).
func AgentCommand(a Agent, bin string, p *Project, gameDir, prompt string, resume bool) *exec.Cmd {
	var args []string
	switch a.ID {
	case "claude":
		if prompt != "" {
			args = append(args, prompt)
		}
		if id := claudeSession(p); resume && id != "" {
			args = append(args, "--resume", id)
			p.ClaudeSession = id
		} else {
			p.ClaudeSession = newUUID()
			args = append(args, "--session-id", p.ClaudeSession)
		}
		_ = writeJSON(filepath.Join(p.Dir, metaFile), p.Meta)
		args = append(args, "--allowedTools")
		args = append(args, AllowedCommands...)
	default: // codex
		if resume && HasSession(a, p) {
			args = append(args, "resume", "--last")
		}
		// Codex only writes inside the project unless told otherwise; deploying
		// copies into the game, and ror2 keeps tools in its cache.
		for _, d := range []string{filepath.Join(gameDir, "BepInEx", "plugins"), config.CacheDir()} {
			args = append(args, "--add-dir", d)
		}
		if prompt != "" {
			args = append(args, prompt)
		}
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = p.Dir
	return cmd
}

// ClaudeTrusted reports whether Claude Code has been told to trust dir.
// It only reads Claude Code's state file; ror2 never changes it.
func ClaudeTrusted(dir string) bool {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return false
	}
	var st struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(data, &st) != nil {
		return false
	}
	return st.Projects[dir].Trusted
}

// TrustHint explains Claude Code's first-run folder prompt.
const TrustHint = "Claude Code first asks whether you trust this folder. ror2 created it, so choose \"Yes, I trust this folder\" (press ↓ then Enter). The default answer, \"No, exit\", closes Claude right away."

// MarkSession records that an agent was used in the project.
func (p *Project) MarkSession(a Agent) {
	if p.AgentSessions == nil {
		p.AgentSessions = map[string]time.Time{}
	}
	p.AgentSessions[a.ID] = time.Now()
	_ = writeJSON(filepath.Join(p.Dir, metaFile), p.Meta)
}

// KickoffPrompt is the first message for an agent in a fresh project.
func KickoffPrompt(p *Project) string {
	return fmt.Sprintf("This is a new Risk of Rain 2 mod project called %s. What I want it to do: %s\n\n"+
		"Read CLAUDE.md/AGENTS.md and the ror2-mod-workflow skill first. Then look at Plugin.cs, "+
		"propose a short plan in plain language, and once I agree, build it step by step: "+
		"`ror2 dev build` after each change, `ror2 dev deploy` when it compiles, and tell me how to test it in-game.",
		p.Name, p.Description)
}
