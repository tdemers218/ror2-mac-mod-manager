package tui

// The "Sort & filter" panel: several sort levels and any number of things
// to show or hide, for the Browse and Installed tabs. Everything is saved to
// the config as soon as it changes.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"ror2mm/internal/core"
	ts "ror2mm/internal/thunderstore"
	"ror2mm/internal/ui"
)

// sortField is something a list can be ordered by. best and worst describe
// the two directions in words.
type sortField struct {
	id, label, best, worst string
}

var browseSortFields = []sortField{
	{"relevance", "Best match", "best first", "worst first"},
	{"downloads", "Downloads", "most first", "fewest first"},
	{"updated", "Last updated", "newest first", "oldest first"},
	{"created", "First released", "newest first", "oldest first"},
	{"rating", "Rating", "highest first", "lowest first"},
	{"name", "Name", "A → Z", "Z → A"},
	{"size", "Download size", "largest first", "smallest first"},
}

var installedSortFields = []sortField{
	{"name", "Name", "A → Z", "Z → A"},
	{"updates", "Has an update", "updates first", "updates last"},
	{"enabled", "Enabled", "enabled first", "disabled first"},
	{"updated", "Last updated", "newest first", "oldest first"},
	{"downloads", "Downloads", "most first", "fewest first"},
	{"rating", "Rating", "highest first", "lowest first"},
	{"size", "Download size", "largest first", "smallest first"},
}

// sortKey is one level of a sort: a field, best-first unless reversed.
type sortKey struct {
	field   string
	reverse bool
}

const maxSortKeys = 4

// toggle is a show/hide switch. on is the value that shows the thing.
type toggle struct {
	key, label, help string
	on, off          string
	onLabel          string
	offLabel         string
	def              string
}

var browseToggles = []toggle{
	{key: "browse_libraries", label: "Libraries", help: "Support code other mods need. They install automatically as dependencies.", on: "show", off: "hide", def: "hide"},
	{key: "deprecated", label: "Deprecated mods", help: "Mods their authors abandoned.", on: "show", off: "hide", def: "hide"},
	{key: "browse_nsfw", label: "NSFW mods", help: "Mods marked as having adult content.", on: "show", off: "hide", def: "hide"},
	{key: "browse_installed", label: "Mods you already have", help: "Hide these to only see what is new to you.", on: "show", off: "hide", def: "show"},
	{key: "browse_stale", label: "Not updated in a year", help: "Older mods are more likely to break with the current game.", on: "show", off: "hide", def: "show"},
}

var installedToggles = []toggle{
	{key: "inst_group", label: "Group by category", help: "Sections like Survivors and Items, or one flat list.", on: "on", off: "off", onLabel: "on", offLabel: "off", def: "on"},
	{key: "inst_libraries", label: "Libraries & dependencies", help: "Mods that are only there because another mod needs them.", on: "show", off: "hide", def: "show"},
	{key: "inst_disabled", label: "Disabled mods", help: "Mods you switched off with space.", on: "show", off: "hide", def: "show"},
	{key: "inst_local", label: "Hand-installed mods", help: "Mods ror2 did not install.", on: "show", off: "hide", def: "show"},
	{key: "inst_current", label: "Up-to-date mods", help: "Hide these to see only mods with an update.", on: "show", off: "hide", def: "show"},
}

func (t toggle) label2(v string) string {
	if v == t.on {
		if t.onLabel != "" {
			return t.onLabel
		}
		return "shown"
	}
	if t.offLabel != "" {
		return t.offLabel
	}
	return "hidden"
}

// ---------- reading and saving ----------

func (m *Model) shown(t toggle) bool { return m.app.Cfg.Get(t.key, t.def) == t.on }

func findToggle(list []toggle, key string) toggle {
	for _, t := range list {
		if t.key == key {
			return t
		}
	}
	return toggle{}
}

func (m *Model) setToggle(t toggle, show bool) {
	v := t.off
	if show {
		v = t.on
	}
	m.app.Cfg.Set(t.key, v)
	m.saveView()
}

// flipToggle switches one Browse/Installed toggle and says what happened.
func (m *Model) flipToggle(list []toggle, key string) tea.Cmd {
	t := findToggle(list, key)
	m.setToggle(t, !m.shown(t))
	m.cur[m.tab] = 0
	m.recompute()
	return m.showToast(toastInfo, t.label+": "+t.label2(m.app.Cfg.Get(t.key, t.def)))
}

func (m *Model) saveView() {
	if err := m.app.Cfg.Save(); err == nil {
		m.diskStamp = m.app.DiskStamp()
	}
}

func sortConfigKey(t tab) string {
	if t == tabInstalled {
		return "installed_sort"
	}
	return "browse_sort"
}

func sortFields(t tab) []sortField {
	if t == tabInstalled {
		return installedSortFields
	}
	return browseSortFields
}

func defaultSorts(m *Model, t tab) string {
	if t == tabInstalled {
		return "name"
	}
	// Older configs had a single "sort" setting.
	first := m.app.Cfg.Get("sort", "relevance")
	if first == "relevance" {
		return "relevance,downloads"
	}
	return first
}

// sorts returns the saved sort levels for a tab ("downloads,-updated").
func (m *Model) sorts(t tab) []sortKey {
	raw := m.app.Cfg.Get(sortConfigKey(t), defaultSorts(m, t))
	var out []sortKey
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		k := sortKey{field: strings.TrimPrefix(strings.TrimSpace(part), "-"), reverse: strings.HasPrefix(strings.TrimSpace(part), "-")}
		if fieldByID(t, k.field) != nil && !seen[k.field] && len(out) < maxSortKeys {
			seen[k.field] = true
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		out = []sortKey{{field: sortFields(t)[0].id}}
		if t == tabBrowse {
			out = append(out, sortKey{field: "downloads"})
		}
	}
	return out
}

func (m *Model) setSorts(t tab, keys []sortKey) {
	var parts []string
	for _, k := range keys {
		if k.reverse {
			parts = append(parts, "-"+k.field)
		} else {
			parts = append(parts, k.field)
		}
	}
	m.app.Cfg.Set(sortConfigKey(t), strings.Join(parts, ","))
	m.saveView()
	m.cur[t], m.off[t] = 0, 0
	m.recompute()
}

func fieldByID(t tab, id string) *sortField {
	for i, f := range sortFields(t) {
		if f.id == id {
			return &sortFields(t)[i]
		}
	}
	return nil
}

// Category (or section) filters: each one is shown normally, "only", or hidden.
const (
	catAny  = ""
	catOnly = "only"
	catHide = "hide"
)

func catKeys(t tab) (only, hide string) {
	if t == tabInstalled {
		return "inst_only_sections", "inst_hide_sections"
	}
	return "browse_only", "browse_hide"
}

func (m *Model) catList(key string) []string {
	var out []string
	for _, c := range strings.Split(m.app.Cfg.Get(key, ""), ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func (m *Model) catState(t tab, name string) string {
	ok, hk := catKeys(t)
	if containsStr(m.catList(ok), name) {
		return catOnly
	}
	if containsStr(m.catList(hk), name) {
		return catHide
	}
	return catAny
}

func (m *Model) setCatState(t tab, name, state string) {
	ok, hk := catKeys(t)
	only, hide := without(m.catList(ok), name), without(m.catList(hk), name)
	switch state {
	case catOnly:
		only = append(only, name)
	case catHide:
		hide = append(hide, name)
	}
	m.app.Cfg.Set(ok, strings.Join(only, ","))
	m.app.Cfg.Set(hk, strings.Join(hide, ","))
	m.saveView()
	m.cur[t], m.off[t] = 0, 0
	m.recompute()
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func without(list []string, s string) []string {
	var out []string
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// resetView puts a tab's sort and filters back to the defaults.
func (m *Model) resetView(t tab) {
	keys := []string{sortConfigKey(t)}
	ok, hk := catKeys(t)
	keys = append(keys, ok, hk)
	list := browseToggles
	if t == tabInstalled {
		list = installedToggles
	}
	for _, tg := range list {
		keys = append(keys, tg.key)
	}
	for _, k := range keys {
		delete(m.app.Cfg.UI, k)
	}
	if t == tabBrowse {
		delete(m.app.Cfg.UI, "sort")
	}
	m.saveView()
	m.cur[t], m.off[t] = 0, 0
	m.recompute()
}

// ---------- applying ----------

var tsModes = map[string]ts.SortMode{
	"relevance": ts.SortRelevance, "downloads": ts.SortDownloads, "updated": ts.SortUpdated,
	"created": ts.SortCreated, "rating": ts.SortRating, "name": ts.SortName, "size": ts.SortSize,
}

// searchOptions turns the Browse settings into catalog search options.
func (m *Model) searchOptions() ts.SearchOptions {
	keys := m.sorts(tabBrowse)
	hasRel := false
	for _, k := range keys {
		hasRel = hasRel || k.field == "relevance"
	}
	var tk []ts.SortKey
	if m.search.Value() != "" && !hasRel {
		// A typed search ranks by best match unless the sort says otherwise.
		tk = append(tk, ts.SortKey{Mode: ts.SortRelevance})
	}
	for _, k := range keys {
		tk = append(tk, ts.SortKey{Mode: tsModes[k.field], Reverse: k.reverse})
	}
	shown := func(key string) bool { return m.shown(findToggle(browseToggles, key)) }
	ok, hk := catKeys(tabBrowse)
	opt := ts.SearchOptions{
		Sorts:             tk,
		IncludeDeprecated: shown("deprecated"),
		IncludeNSFW:       shown("browse_nsfw"),
		HideLibraries:     !shown("browse_libraries"),
		OnlyCategories:    m.catList(ok),
		HideCategories:    m.catList(hk),
	}
	if !shown("browse_stale") {
		opt.UpdatedAfter = time.Now().AddDate(-1, 0, 0)
	}
	if !shown("browse_installed") {
		opt.Exclude = func(p *ts.Package) bool { return m.app.Inv.Find(p.FullName) != nil }
	}
	return opt
}

// primarySort is the first sort level that applies right now.
func (m *Model) primarySort() sortKey {
	for _, k := range m.sorts(tabBrowse) {
		if k.field != "relevance" || m.search.Value() != "" {
			return k
		}
	}
	if m.search.Value() != "" {
		return sortKey{field: "relevance"}
	}
	return sortKey{field: "downloads"}
}

// installedGroups is the Installed list after the view settings: filtered,
// sorted, and grouped or flattened.
func (m *Model) installedGroups() (groups []*core.Group, flat bool) {
	tg := func(key string) bool { return m.shown(findToggle(installedToggles, key)) }
	keep := func(e *core.Entry) bool {
		switch {
		case !tg("inst_disabled") && !e.Enabled:
			return false
		case !tg("inst_local") && !e.Managed:
			return false
		case !tg("inst_current") && !e.HasUpdate():
			return false
		}
		return true
	}
	ok, hk := catKeys(tabInstalled)
	only, hide := m.catList(ok), m.catList(hk)
	keys := m.sorts(tabInstalled)
	for _, g := range m.app.Inv.Groups() {
		if g.Library && !tg("inst_libraries") {
			continue
		}
		if containsStr(hide, g.Name) || (len(only) > 0 && !containsStr(only, g.Name)) {
			continue
		}
		c := *g
		c.Entries = nil
		for _, e := range g.Entries {
			if keep(e) {
				c.Entries = append(c.Entries, e)
			}
		}
		if len(c.Entries) > 0 {
			groups = append(groups, &c)
		}
	}
	if !tg("inst_group") {
		all := &core.Group{Name: "All mods"}
		for _, g := range groups {
			all.Entries = append(all.Entries, g.Entries...)
		}
		groups = []*core.Group{all}
		flat = true
	}
	for _, g := range groups {
		sort.SliceStable(g.Entries, func(i, j int) bool { return lessEntry(g.Entries[i], g.Entries[j], keys) })
	}
	return groups, flat
}

func lessEntry(a, b *core.Entry, keys []sortKey) bool {
	for _, k := range keys {
		c := cmpEntry(a, b, k.field)
		if k.reverse {
			c = -c
		}
		if c != 0 {
			return c < 0
		}
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}

func cmpBool(a, b bool) int {
	switch {
	case a && !b:
		return -1
	case b && !a:
		return 1
	}
	return 0
}

func cmpEntry(a, b *core.Entry, field string) int {
	switch field {
	case "name":
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	case "updates":
		return cmpBool(a.HasUpdate(), b.HasUpdate())
	case "enabled":
		return cmpBool(a.Enabled, b.Enabled)
	}
	// Package facts: mods not on Thunderstore go last either way.
	if a.Pkg == nil || b.Pkg == nil {
		return cmpBool(a.Pkg != nil, b.Pkg != nil)
	}
	return ts.Compare(a.Pkg, b.Pkg, tsModes[field])
}

// ---------- the panel ----------

type vKind int

const (
	vHead vKind = iota
	vSort
	vAddSort
	vToggle
	vCat
	vReset
)

type vItem struct {
	kind  vKind
	label string
	i     int
	name  string
	count int
}

type viewPanel struct {
	tab       tab
	cur       int
	catCounts map[string]int
}

func (m *Model) openView(t tab, jumpToCats bool) tea.Cmd {
	if t != tabInstalled {
		t = tabBrowse
		if m.app.Cat == nil {
			return m.showToast(toastWarn, "The Thunderstore catalog is still loading…")
		}
	}
	m.tab = t
	if m.mode == mSearch {
		m.search.Blur()
	}
	m.vw = &viewPanel{tab: t, catCounts: map[string]int{}}
	if t == tabBrowse {
		for _, p := range m.app.Cat.Packages {
			for _, c := range p.Categories {
				m.vw.catCounts[c]++
			}
		}
	}
	m.mode = mView
	items := m.viewItems()
	m.vw.cur = nextSelectable(items, 0, 1)
	if jumpToCats {
		for i, it := range items {
			if it.kind == vCat {
				m.vw.cur = i
				break
			}
		}
	}
	return nil
}

func (m *Model) viewItems() []vItem {
	t := m.vw.tab
	var items []vItem
	items = append(items, vItem{kind: vHead, label: "Sort by"})
	keys := m.sorts(t)
	for i := range keys {
		items = append(items, vItem{kind: vSort, i: i})
	}
	if len(keys) < maxSortKeys && len(keys) < len(sortFields(t)) {
		items = append(items, vItem{kind: vAddSort, label: "add a tie-breaker"})
	}
	items = append(items, vItem{kind: vHead, label: "Show"})
	list := browseToggles
	if t == tabInstalled {
		list = installedToggles
	}
	for i := range list {
		items = append(items, vItem{kind: vToggle, i: i})
	}
	if t == tabBrowse {
		items = append(items, vItem{kind: vHead, label: "Categories"})
		cats := m.app.Cat.Categories()
		for _, c := range cats {
			items = append(items, vItem{kind: vCat, name: c, count: m.vw.catCounts[c]})
		}
	} else {
		items = append(items, vItem{kind: vHead, label: "Sections"})
		for _, g := range m.app.Inv.Groups() {
			items = append(items, vItem{kind: vCat, name: g.Name, count: len(g.Entries)})
		}
	}
	items = append(items, vItem{kind: vHead, label: ""}, vItem{kind: vReset, label: "Reset to defaults"})
	return items
}

func nextSelectable(items []vItem, from, dir int) int {
	for i := from; i >= 0 && i < len(items); i += dir {
		if items[i].kind != vHead {
			return i
		}
	}
	return -1
}

func (m *Model) viewToggles() []toggle {
	if m.vw.tab == tabInstalled {
		return installedToggles
	}
	return browseToggles
}

// viewKey handles a key press while the panel is open.
func (m *Model) viewKey(key string) tea.Cmd {
	v := m.vw
	items := m.viewItems()
	if v.cur < 0 || v.cur >= len(items) {
		v.cur = nextSelectable(items, 0, 1)
	}
	move := func(dir, steps int) {
		for s := 0; s < steps; s++ {
			if n := nextSelectable(items, v.cur+dir, dir); n >= 0 {
				v.cur = n
			}
		}
	}
	switch key {
	case "esc", "q", "f", "c", "o":
		m.mode = mNormal
		m.vw = nil
		return nil
	case "up", "k":
		move(-1, 1)
		return nil
	case "down", "j":
		move(1, 1)
		return nil
	case "pgup":
		move(-1, 10)
		return nil
	case "pgdown":
		move(1, 10)
		return nil
	case "home", "g":
		v.cur = nextSelectable(items, 0, 1)
		return nil
	case "end", "G":
		v.cur = nextSelectable(items, len(items)-1, -1)
		return nil
	case "r", "R":
		m.resetView(v.tab)
		return m.showToast(toastOK, "Sort and filters are back to the defaults.")
	}
	it := items[v.cur]
	switch it.kind {
	case vSort:
		keys := m.sorts(v.tab)
		switch key {
		case "left", "h":
			keys[it.i].field = m.otherField(v.tab, keys, it.i, -1)
		case "right", "l":
			keys[it.i].field = m.otherField(v.tab, keys, it.i, 1)
		case " ", "enter":
			keys[it.i].reverse = !keys[it.i].reverse
		case "x", "backspace", "delete":
			if len(keys) == 1 {
				return m.showToast(toastInfo, "Keep at least one sort.")
			}
			keys = append(keys[:it.i], keys[it.i+1:]...)
		case "K", "shift+up", "[":
			if it.i == 0 {
				return nil
			}
			keys[it.i-1], keys[it.i] = keys[it.i], keys[it.i-1]
			v.cur--
		case "J", "shift+down", "]":
			if it.i == len(keys)-1 {
				return nil
			}
			keys[it.i+1], keys[it.i] = keys[it.i], keys[it.i+1]
			v.cur++
		default:
			return nil
		}
		m.setSorts(v.tab, keys)
	case vAddSort:
		if key == " " || key == "enter" {
			keys := m.sorts(v.tab)
			keys = append(keys, sortKey{field: m.otherField(v.tab, append(keys, sortKey{}), len(keys), 1)})
			m.setSorts(v.tab, keys)
			v.cur = m.sortRowIndex(len(keys) - 1)
		}
	case vToggle:
		switch key {
		case " ", "enter", "left", "right", "h", "l":
			t := m.viewToggles()[it.i]
			m.setToggle(t, !m.shown(t))
			m.cur[v.tab], m.off[v.tab] = 0, 0
			m.recompute()
		}
	case vCat:
		st := m.catState(v.tab, it.name)
		switch key {
		case " ", "enter", "right", "l":
			st = map[string]string{catAny: catOnly, catOnly: catHide, catHide: catAny}[st]
		case "left", "h":
			st = map[string]string{catAny: catHide, catHide: catOnly, catOnly: catAny}[st]
		case "s":
			st = catOnly
		case "x", "-":
			st = catHide
		case "a", "backspace":
			st = catAny
		default:
			return nil
		}
		m.setCatState(v.tab, it.name, st)
	case vReset:
		if key == " " || key == "enter" {
			m.resetView(v.tab)
			return m.showToast(toastOK, "Sort and filters are back to the defaults.")
		}
	}
	return nil
}

// otherField steps sort level i to the next field no other level uses.
func (m *Model) otherField(t tab, keys []sortKey, i, dir int) string {
	fields := sortFields(t)
	used := map[string]bool{}
	for j, k := range keys {
		if j != i {
			used[k.field] = true
		}
	}
	start := 0
	for j, f := range fields {
		if f.id == keys[i].field {
			start = j
		}
	}
	for s := 1; s <= len(fields); s++ {
		f := fields[((start+dir*s)%len(fields)+len(fields))%len(fields)]
		if !used[f.id] {
			return f.id
		}
	}
	return keys[i].field
}

func (m *Model) sortRowIndex(i int) int {
	for j, it := range m.viewItems() {
		if it.kind == vSort && it.i == i {
			return j
		}
	}
	return 0
}

// viewClick selects a row, or acts on it if it was already selected.
func (m *Model) viewClick(n int) tea.Cmd {
	items := m.viewItems()
	if n < 0 || n >= len(items) || items[n].kind == vHead {
		return nil
	}
	if m.vw.cur != n {
		m.vw.cur = n
		if items[n].kind != vToggle && items[n].kind != vCat {
			return nil
		}
	}
	return m.viewKey(" ")
}

func (m *Model) viewTitle() string {
	if m.vw.tab == tabInstalled {
		n := 0
		groups, _ := m.installedGroups()
		for _, g := range groups {
			n += len(g.Entries)
		}
		return fmt.Sprintf("Installed view · %d of %d shown", n, len(m.app.Inv.Entries))
	}
	return fmt.Sprintf("Sort & filter · %s mods match", ui.Count(len(m.results)))
}

func (m *Model) viewPanelView() string {
	v := m.vw
	_, h := m.bodySize()
	w := min(m.modalWidth(), 70)
	iw := w - 6
	items := m.viewItems()
	keys := m.sorts(v.tab)
	toggles := m.viewToggles()
	lines := make([]string, len(items))
	accent := lipgloss.NewStyle().Foreground(ui.Equipment).Bold(true)
	for i, it := range items {
		sel := i == v.cur
		nameSt := stName
		if sel {
			nameSt = stBold
		}
		switch it.kind {
		case vHead:
			if it.label == "" {
				lines[i] = ""
				continue
			}
			lines[i] = ui.Rule(strings.ToUpper(it.label), iw)
		case vSort:
			k := keys[it.i]
			f := fieldByID(v.tab, k.field)
			dir, arrow := f.best, "⇣"
			if k.reverse {
				dir, arrow = f.worst, "⇡"
			}
			num := fmt.Sprintf(" %d  ", it.i+1)
			lead := "then "
			if it.i == 0 {
				lead = ""
			}
			label := []seg{{num, stVoid}, {lead, stFaint}, {f.label, nameSt}}
			if sel {
				label = []seg{{num, stVoid}, {lead, stFaint}, {"‹ ", stVoid}, {f.label, accent}, {" ›", stVoid}}
			}
			lines[i] = m.mark(fmt.Sprintf("vw:%d", i), rowLead(iw, sel, label, []seg{{arrow + " " + dir, stDim}}, "·"))
		case vAddSort:
			lines[i] = m.mark(fmt.Sprintf("vw:%d", i), row(iw, sel, []seg{{" +  ", stVoid}, {it.label, stFaint}}, nil))
		case vToggle:
			t := toggles[it.i]
			on := m.shown(t)
			box, bst := "○", stFaint
			if on {
				box, bst = "◉", stOK
			}
			val := t.label2(m.app.Cfg.Get(t.key, t.def))
			vst := stDim
			if !on {
				vst = stWarn
			}
			lines[i] = m.mark(fmt.Sprintf("vw:%d", i), rowLead(iw, sel, []seg{{" " + box + "  ", bst}, {t.label, nameSt}}, []seg{{val, vst}}, "·"))
		case vCat:
			st := m.catState(v.tab, it.name)
			mark, mst, val, vst := "·", stFaint, ui.Count(it.count), stFaint
			switch st {
			case catOnly:
				mark, mst, val, vst = ui.GOK, stOK, "only", stOK
			case catHide:
				mark, mst, val, vst = ui.GFail, stBad, "hidden", stBad
			}
			ns := nameSt
			if st == catHide {
				ns = ns.Strikethrough(true).Foreground(ui.Muted)
			}
			lines[i] = m.mark(fmt.Sprintf("vw:%d", i), rowLead(iw, sel, []seg{{" " + mark + "  ", mst}, {it.name, ns}}, []seg{{val, vst}}, "·"))
		case vReset:
			lines[i] = m.mark(fmt.Sprintf("vw:%d", i), row(iw, sel, []seg{{" ⟲  ", stVoid}, {it.label, nameSt}}, nil))
		}
	}
	// Scroll so the selected row (and its section heading) stay in view.
	maxRows := max(6, h-12)
	start := 0
	if v.cur >= maxRows-1 {
		start = v.cur - maxRows + 2
	}
	if start > 0 && start < len(items) && items[start-1].kind == vHead && v.cur-start < maxRows-1 {
		start--
	}
	end := min(len(lines), start+maxRows)
	body := lines[start:end]
	more := ""
	if start > 0 {
		more += "⇡ more above  "
	}
	if end < len(lines) {
		more += "⇣ more below"
	}

	var hints []string
	switch it := items[max(0, v.cur)]; it.kind {
	case vSort:
		hints = []string{"←", "field", "space", "direction", "[", "up", "]", "down", "x", "remove"}
	case vAddSort, vReset:
		hints = []string{"enter", "pick"}
	case vToggle:
		hints = []string{"space", "show/hide"}
		help := toggles[it.i].help
		more = help + "   " + more
	case vCat:
		hints = []string{"space", "any › only › hidden", "s", "only", "x", "hide", "a", "any"}
	}
	hints = append(hints, "r", "reset", "esc", "close")
	foot := ui.Fainter.Render(ansi.Truncate(strings.TrimSpace(more), iw, "…"))
	content := strings.Join(body, "\n") + "\n\n" + foot + "\n" + m.hintsW(iw, hints...)
	return m.modal(m.viewTitle(), content, ui.Void, w)
}

// sortSummary is a short description of the sort, like "downloads ⇣ +1".
func (m *Model) sortSummary(t tab) string {
	keys := m.sorts(t)
	k := keys[0]
	if t == tabBrowse {
		k = m.primarySort()
	}
	f := fieldByID(t, k.field)
	if f == nil {
		return k.field
	}
	s := strings.ToLower(f.label)
	if k.reverse {
		s += " ⇡"
	} else {
		s += " ⇣"
	}
	if len(keys) > 1 {
		s += fmt.Sprintf(" +%d", len(keys)-1)
	}
	return s
}

// filterSummary says how much is being filtered, like "3 hidden · 1 only".
func (m *Model) filterSummary(t tab) (string, bool) {
	list := browseToggles
	if t == tabInstalled {
		list = installedToggles
	}
	hidden, changed := 0, false
	for _, tg := range list {
		if tg.on == "show" && !m.shown(tg) {
			hidden++
		}
		changed = changed || m.app.Cfg.Get(tg.key, tg.def) != tg.def
	}
	ok, hk := catKeys(t)
	only, hide := len(m.catList(ok)), len(m.catList(hk))
	var parts []string
	if n := hidden + hide; n > 0 {
		parts = append(parts, fmt.Sprintf("%d hidden", n))
	}
	if only > 0 {
		parts = append(parts, fmt.Sprintf("%d only", only))
	}
	if len(parts) == 0 {
		return "everything", false
	}
	return strings.Join(parts, " · "), changed || only+hide > 0
}
