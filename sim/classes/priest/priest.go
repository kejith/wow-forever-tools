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
	Smite           = 585
	ShadowWordPain  = 589
	MindBlast       = 8092
	MindFlay        = 15407 // talent-granted, channeled
	HolyFire        = 14914 // a cast with a direct hit and a short DoT
	DevouringPlague = 2944  // a DoT whose cooldown equals its duration
	Renew           = 139
	LesserHeal      = 2050
	Heal            = 2054
	GreaterHeal     = 2060
	FlashHeal       = 2061
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
		// Shadow Word: Pain and Mind Blast always; Holy Fire and Devouring
		// Plague each with and without; Mind Flay or Smite as the filler.
		// The engine keeps the combination with the most damage.
		best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
		options := [][]engine.Action{nil}
		for _, base := range []int{HolyFire, DevouringPlague} {
			sp := best(base)
			if sp == nil {
				continue
			}
			for _, o := range options[:len(options):len(options)] {
				options = append(options, append(append([]engine.Action(nil), o...), engine.Action{Kind: "dot", Spell: sp}))
			}
		}
		var fillers []*engine.Spell
		for _, base := range []int{MindFlay, Smite} {
			if sp := best(base); sp != nil {
				fillers = append(fillers, sp)
			}
		}
		if len(fillers) == 0 {
			fillers = []*engine.Spell{nil}
		}
		var out [][]engine.Action
		for _, filler := range fillers {
			for _, opt := range options {
				var actions []engine.Action
				if sp := best(ShadowWordPain); sp != nil {
					actions = append(actions, engine.Action{Kind: "dot", Spell: sp})
				}
				actions = append(actions, opt...)
				if sp := best(MindBlast); sp != nil {
					actions = append(actions, engine.Action{Kind: "cooldown", Spell: sp})
				}
				if filler != nil {
					actions = append(actions, engine.Action{Kind: "filler", Spell: filler})
				}
				out = append(out, append(actions, engine.Action{Kind: "wand"}))
			}
		}
		return out
	}

	// Healer: keep Renew up, fill with one single-target heal rank. In
	// Forever each of the four heals is its own chain; should a table link
	// two of them anyway (explicit SupercedesSpell), a rank still becomes
	// one candidate at most.
	renew := cat.BestRank(Renew, known, level)
	var out [][]engine.Action
	seen := map[int]bool{}
	for _, base := range healChains {
		for _, rank := range cat.KnownRanks(base, known, level) {
			if seen[rank.ID] {
				continue
			}
			seen[rank.ID] = true
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
