package game

import "testing"

func TestDestination(t *testing.T) {
	cases := []struct {
		in, want string
		cfg, ok  bool
	}{
		{"plugins/Foo/Foo.dll", "BepInEx/plugins/A-Foo/Foo/Foo.dll", false, true},
		{"Foo.dll", "BepInEx/plugins/A-Foo/Foo.dll", false, true},
		{"manifest.json", "BepInEx/plugins/A-Foo/manifest.json", false, true},
		{`plugins\Foo\Foo.dll`, "BepInEx/plugins/A-Foo/Foo/Foo.dll", false, true},
		{"BepInEx/plugins/Foo.dll", "BepInEx/plugins/A-Foo/Foo.dll", false, true},
		{"patchers/P/P.dll", "BepInEx/patchers/A-Foo/P/P.dll", false, true},
		{"config/foo.cfg", "BepInEx/config/foo.cfg", true, true},
		{"Foo.mm.dll", "BepInEx/monomod/A-Foo/Foo.mm.dll", false, true},
		{"assets/x.png", "BepInEx/plugins/A-Foo/assets/x.png", false, true},
		{"../evil.dll", "", false, false},
		{"plugins/../../evil.dll", "", false, false},
		{"__MACOSX/._Foo.dll", "", false, false},
		{"plugins/.DS_Store", "", false, false},
	}
	for _, c := range cases {
		got, cfg, ok := Destination("A-Foo", c.in)
		if got != c.want || cfg != c.cfg || ok != c.ok {
			t.Errorf("Destination(%q) = %q,%v,%v; want %q,%v,%v", c.in, got, cfg, ok, c.want, c.cfg, c.ok)
		}
	}
}

func TestLogRegexes(t *testing.T) {
	line := "[Error  :   BepInEx] Could not load [R2API 5.2.0] because it has missing dependencies: ___riskofthunder.RoR2BepInExPack, com.foo.bar (v1.2.0 or newer)"
	m := reMissing.FindStringSubmatch(line)
	if m == nil || m[1] != "R2API" || m[2] != "5.2.0" {
		t.Fatalf("missing-deps line not parsed: %v", m)
	}
	if m := reLoading.FindStringSubmatch("[Info   :   BepInEx] Loading [Risk Of Options 2.8.4]"); m == nil || m[1] != "Risk Of Options" || m[2] != "2.8.4" {
		t.Fatalf("loading line not parsed: %v", m)
	}
}
