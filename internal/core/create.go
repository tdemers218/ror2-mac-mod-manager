package core

import (
	"strings"

	"ror2mm/internal/devkit"
)

// fallback versions if the catalog is not loaded (Thunderstore needs a version).
var fallbackDeps = map[string]string{"bbepis-BepInExPack": "5.4.2121"}

// TemplateDeps returns manifest dependency strings (Owner-Name-x.y.z) for a template.
func (a *App) TemplateDeps(t devkit.Template) []string {
	names := append([]string{"bbepis-BepInExPack"}, t.Deps...)
	var out []string
	for _, n := range names {
		v := fallbackDeps[n]
		if a.Cat != nil {
			if p := a.Cat.Get(n); p != nil && p.Latest() != nil {
				v = p.Latest().Number
			}
		}
		if v != "" {
			out = append(out, n+"-"+v)
		}
	}
	return out
}

// TemplatePlan plans installing what a template needs into the game, so the
// new project builds and runs.
func (a *App) TemplatePlan(t devkit.Template) (*Plan, error) {
	var reqs []Request
	for _, d := range t.Deps {
		if a.Inv.Find(d) == nil {
			reqs = append(reqs, Request{FullName: d})
		}
	}
	plan, err := a.PlanInstall(reqs)
	if err != nil {
		return nil, err
	}
	for _, s := range plan.Steps {
		if s.Reason == "requested" {
			s.Reason = "needed by the " + strings.ToLower(t.Label) + " template"
			s.AsDep = true
		}
	}
	return plan, nil
}
