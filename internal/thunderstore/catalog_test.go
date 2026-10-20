package thunderstore

import (
	"strings"
	"testing"
	"time"
)

func TestParseDep(t *testing.T) {
	full, v := ParseDep("RiskofThunder-R2API_Core-5.3.0")
	if full != "RiskofThunder-R2API_Core" || v != "5.3.0" {
		t.Fatalf("got %s %s", full, v)
	}
	o, n := SplitFullName("RiskofThunder-R2API_Core")
	if o != "RiskofThunder" || n != "R2API_Core" {
		t.Fatalf("got %s %s", o, n)
	}
	if Normalize("Risk_Of-Options 2") != "riskofoptions2" {
		t.Fatal("normalize")
	}
}

func TestSearchMultiSortAndCategories(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	c := &Catalog{Packages: []*Package{
		{Name: "Alpha", Downloads: 10, Rating: 5, Updated: day(1), Categories: []string{"Items"}},
		{Name: "Bravo", Downloads: 10, Rating: 9, Updated: day(2), Categories: []string{"Skins"}},
		{Name: "Charlie", Downloads: 50, Rating: 1, Updated: day(3), Categories: []string{"Items", "Libraries"}},
		{Name: "Delta", Downloads: 10, Rating: 9, Updated: day(4), Categories: []string{"Survivors"}},
	}}
	names := func(ps []*Package) string {
		var s []string
		for _, p := range ps {
			s = append(s, p.Name)
		}
		return strings.Join(s, ",")
	}
	// Downloads first, then rating, then oldest update first.
	got := names(c.Search("", SearchOptions{Sorts: []SortKey{{Mode: SortDownloads}, {Mode: SortRating}, {Mode: SortUpdated, Reverse: true}}}))
	if got != "Charlie,Bravo,Delta,Alpha" {
		t.Fatalf("multi sort: %s", got)
	}
	got = names(c.Search("", SearchOptions{Sorts: []SortKey{{Mode: SortName, Reverse: true}}}))
	if got != "Delta,Charlie,Bravo,Alpha" {
		t.Fatalf("name Z-A: %s", got)
	}
	got = names(c.Search("", SearchOptions{Sorts: []SortKey{{Mode: SortName}}, OnlyCategories: []string{"Items", "Survivors"}, HideLibraries: true}))
	if got != "Alpha,Delta" {
		t.Fatalf("only + hide libraries: %s", got)
	}
	got = names(c.Search("", SearchOptions{Sorts: []SortKey{{Mode: SortName}}, HideCategories: []string{"Items", "Skins"}}))
	if got != "Delta" {
		t.Fatalf("hide categories: %s", got)
	}
	got = names(c.Search("", SearchOptions{Sorts: []SortKey{{Mode: SortName}}, UpdatedAfter: day(2), Exclude: func(p *Package) bool { return p.Name == "Delta" }}))
	if got != "Bravo,Charlie" {
		t.Fatalf("updated after + exclude: %s", got)
	}
}
