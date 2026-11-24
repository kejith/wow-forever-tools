// Package enginebundle builds run.Input bundles from WoWToolsForever snapshots and holds
// class/spec metadata (talent trees, role detection, supported engine metrics).
package enginebundle

import "github.com/kejith/wow-tools-forever/sim/internal/sv"

// Metrics are the classes the engine models and their metrics (sim/run must agree). The
// first metric is the default for a damage spec.
var Metrics = map[string][]string{
	"WARLOCK": {"dps"},
	"PRIEST":  {"hps", "ttoom", "dps"},
	"MAGE":    {"dps"},
	"DRUID":   {"hps", "ttoom", "dps"},
	"SHAMAN":  {"hps", "ttoom", "dps"},
	"PALADIN": {"hps", "ttoom", "dps"},
	"WARRIOR": {"dps"},
	"ROGUE":   {"dps"},
	"HUNTER":  {"dps"},
}

// Trees are the talent trees per class in Forever's TalentTab.OrderIndex order.
var Trees = map[string][]string{
	"WARRIOR": {"arms", "fury", "protection"},
	"PALADIN": {"holy", "protection", "retribution"},
	"HUNTER":  {"beast_mastery", "marksmanship", "survival"},
	"ROGUE":   {"assassination", "combat", "subtlety"},
	"PRIEST":  {"discipline", "holy", "shadow"},
	"SHAMAN":  {"elemental", "enhancement", "restoration"},
	"MAGE":    {"arcane", "fire", "frost"},
	"WARLOCK": {"affliction", "demonology", "destruction"},
	"DRUID":   {"balance", "feral", "restoration"},
}

// BearFormSpells are Druid Bear Form and Dire Bear Form spell IDs.
var BearFormSpells = map[int]bool{5487: true, 9634: true}

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

// specGroups returns the talent tab entries for a snapshot. Forever stores all three trees
// as trait groups inside talents.class[1].groups; older snapshots stored one entry per tree.
func specGroups(char *sv.Table) []*sv.Table {
	trees := char.Tab("talents").Tab("class").Tables()
	if len(trees) == 0 {
		return nil
	}
	if len(trees) == 1 {
		if groups := trees[0].Tab("groups").Tables(); len(groups) > 0 {
			return groups
		}
	}
	return trees
}

// bestSpecIndex returns the index of the talent tree with the most points spent (-1 if none).
func bestSpecIndex(char *sv.Table) (int, *sv.Table) {
	groups := specGroups(char)
	if len(groups) == 0 {
		return -1, nil
	}
	best, bestSpent, any := 0, 0.0, false
	for i, g := range groups {
		spent, _ := g.Num("spent")
		if spent > 0 {
			any = true
		}
		if i == 0 || spent > bestSpent {
			best, bestSpent = i, spent
		}
	}
	if !any {
		return -1, nil
	}
	return best, groups[best]
}

// SpecOf returns the canonical lowercase spec slug of the tree with the most talent points
// (ties pick the first tree), or "" if no talent points are spent.
func SpecOf(char *sv.Table) string {
	classFile := char.Tab("character").Str("classFile")
	names := Trees[classFile]
	if len(names) == 0 {
		return ""
	}
	best, _ := bestSpecIndex(char)
	if best < 0 || best >= len(names) {
		return ""
	}
	return names[best]
}

// SpecName returns the localized name of the character's main talent tree as exported from the game,
// or "" if no talent points are spent.
func SpecName(char *sv.Table) string {
	_, g := bestSpecIndex(char)
	if g == nil {
		return ""
	}
	return g.Str("name")
}

// RoleOf classifies a class and spec into one of "caster", "healer", "melee", "ranged", or "tank".
// For feral druids, Bear/Dire Bear form (when exported) maps to "tank", otherwise "melee".
func RoleOf(classFile, spec string, formSpellID int) string {
	switch classFile {
	case "WARRIOR":
		if spec == "protection" {
			return "tank"
		}
		return "melee"
	case "PALADIN":
		switch spec {
		case "holy":
			return "healer"
		case "protection":
			return "tank"
		default:
			return "melee"
		}
	case "HUNTER":
		return "ranged"
	case "ROGUE":
		return "melee"
	case "PRIEST":
		if spec == "shadow" {
			return "caster"
		}
		return "healer"
	case "SHAMAN":
		switch spec {
		case "restoration":
			return "healer"
		case "enhancement":
			return "melee"
		default:
			return "caster"
		}
	case "MAGE", "WARLOCK":
		return "caster"
	case "DRUID":
		switch spec {
		case "restoration":
			return "healer"
		case "feral":
			if BearFormSpells[formSpellID] {
				return "tank"
			}
			return "melee"
		default:
			return "caster"
		}
	default:
		return "caster"
	}
}

// RoleForChar returns RoleOf for a full character snapshot.
func RoleForChar(char *sv.Table) string {
	ch := char.Tab("character")
	formID, _ := ch.Tab("stats").Tab("form").Num("spellID")
	return RoleOf(ch.Str("classFile"), SpecOf(char), int(formID))
}

// DefaultMetric returns "hps" for healing specs and "dps" for all other specs.
func DefaultMetric(classFile, spec string) string {
	if RoleOf(classFile, spec, 0) == "healer" {
		return "hps"
	}
	return "dps"
}
