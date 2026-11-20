// Package priest models the Priest: a sustained healing cycle for Holy and
// Discipline (the engine picks the heal rank that pays best under the mana
// budget) and a Shadow damage rotation.
package priest

import (
	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

// First ranks of the rotation spells (stable Classic ids, present in the
// Forever tables).
const (
	Smite          = 585
	ShadowWordPain = 589
	MindBlast      = 8092
	MindFlay       = 15407 // talent-granted, channeled
	Renew          = 139
	LesserHeal     = 2050
	Heal           = 2054
	GreaterHeal    = 2060
	FlashHeal      = 2061
)

// healChains are the single-target heals a priest can fill with; every known
// rank of each chain is its own rotation candidate, and the engine keeps the
// one whose sustained healing under the mana budget is highest. That is how
// downranking enters the model without a Forever-specific penalty rule (none
// is measured yet, see CLAUDE.md).
var healChains = []int{LesserHeal, Heal, GreaterHeal, FlashHeal}

// Rotations returns the rotation candidates for a metric.
func Rotations(metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	if metric == "dps" {
		var actions []engine.Action
		add := func(kind string, base int) {
			if sp := cat.BestRank(base, known, level); sp != nil {
				actions = append(actions, engine.Action{Kind: kind, Spell: sp})
			}
		}
		add("dot", ShadowWordPain)
		add("cooldown", MindBlast)
		if sp := cat.BestRank(MindFlay, known, level); sp != nil {
			actions = append(actions, engine.Action{Kind: "filler", Spell: sp})
		} else {
			add("filler", Smite)
		}
		actions = append(actions, engine.Action{Kind: "wand"})
		return [][]engine.Action{actions}
	}

	// Healer: keep Renew up, fill with one single-target heal rank.
	renew := cat.BestRank(Renew, known, level)
	var out [][]engine.Action
	for _, base := range healChains {
		for _, rank := range cat.KnownRanks(base, known, level) {
			var actions []engine.Action
			if renew != nil {
				actions = append(actions, engine.Action{Kind: "hot", Spell: renew})
			}
			actions = append(actions, engine.Action{Kind: "heal", Spell: rank})
			out = append(out, actions)
		}
	}
	if out == nil && renew != nil {
		out = [][]engine.Action{{{Kind: "hot", Spell: renew}}}
	}
	return out
}
