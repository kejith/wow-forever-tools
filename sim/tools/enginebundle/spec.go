// Package enginebundle is the Go side of scripts/simengine.py: which classes the combat engine models, their
// metrics and the spec a character plays. The bundle itself (building run.Input from a snapshot) follows in
// phase 2 of the migration (notes/go-migration.md).
package enginebundle

import "github.com/kejith/wow-tools-forever/sim/internal/sv"

// Metrics are the classes the engine models and their metrics (simengine.METRICS; sim/run must agree). The
// first metric is the default for a damage spec.
var Metrics = map[string][]string{"WARLOCK": {"dps"}, "PRIEST": {"hps", "ttoom", "dps"}}

// Trees are the talent trees per class in the client's tab order (simengine.TREES).
var Trees = map[string][]string{"WARLOCK": {"affliction", "demonology", "destruction"},
	"PRIEST": {"discipline", "holy", "shadow"}}

// Supported tells whether the engine models the class.
func Supported(classFile string) bool { _, ok := Metrics[classFile]; return ok }

// HasMetric tells whether the engine has this metric for the class (healing.supported with "hps",
// damage.supported with "dps").
func HasMetric(classFile, metric string) bool {
	for _, m := range Metrics[classFile] {
		if m == metric {
			return true
		}
	}
	return false
}

// SpecOf is simengine.spec_of: the tree with the most talent points (the first of equal ones), "" without
// talents.
func SpecOf(char *sv.Table) string {
	classFile := char.Tab("character").Str("classFile")
	names := Trees[classFile]
	trees := char.Tab("talents").Tab("class").Tables()
	if len(names) == 0 || len(trees) == 0 {
		return ""
	}
	best, bestSpent, any := 0, 0.0, false
	for i, t := range trees {
		spent, _ := t.Num("spent")
		if spent != 0 {
			any = true
		}
		if i == 0 || spent > bestSpent {
			best, bestSpent = i, spent
		}
	}
	if !any || best >= len(names) {
		return ""
	}
	return names[best]
}

// DefaultMetric is simengine.default_metric: a priest who is not Shadow heals, everyone else deals damage.
func DefaultMetric(classFile, spec string) string {
	if classFile == "PRIEST" && spec != "shadow" {
		return "hps"
	}
	return "dps"
}
