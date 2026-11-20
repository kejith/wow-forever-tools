// Package warlock is the first class module: the damage rotations of a
// leveling/dungeon Warlock. A rotation is a priority list the engine turns
// into a steady-state cycle: keep the DoTs up, fill with one spell, convert
// health to mana with Life Tap, wand when nothing is affordable. Which
// filler and which optional spells pay is not decided here: every
// combination the character can cast is a candidate, and the engine keeps
// the one with the most damage.
package warlock

import (
	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

// First ranks of the rotation spells (stable Classic ids, present in the
// Forever tables; Wrack is Forever's Affliction talent).
const (
	ShadowBolt   = 686
	Corruption   = 172
	CurseOfAgony = 980   // "Bane of Agony" in Forever
	SiphonLife   = 18265 // talent-granted
	Immolate     = 348
	DeathCoil    = 6789
	DrainLife    = 689
	Wrack        = 1316697 // talent-granted, channeled
	LifeTap      = 1454
	DemonSkin    = 687
	DemonArmor   = 706
)

// auraHealthRegenInCombat is the aura of Demon Skin/Armor that regenerates
// health in combat (points per 5 seconds).
const auraHealthRegenInCombat = 161

// DefaultMetric is what a Warlock optimizes for.
const DefaultMetric = "dps"

// Rotations builds the candidate priority lists from what the character
// actually knows: the three DoTs always, Immolate and Death Coil each with
// and without, and one of the fillers Wrack, Shadow Bolt or Drain Life, the
// first two also with Drain Life standing by.
func Rotations(cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	var dots []engine.Action
	for _, base := range []int{CurseOfAgony, Corruption, SiphonLife} {
		if sp := best(base); sp != nil {
			dots = append(dots, engine.Action{Kind: "dot", Spell: sp})
		}
	}
	// Each optional spell doubles the candidates: without it and with it.
	options := [][]engine.Action{nil}
	for _, opt := range []struct {
		kind string
		base int
	}{{"dot", Immolate}, {"cooldown", DeathCoil}} {
		sp := best(opt.base)
		if sp == nil {
			continue
		}
		for _, o := range options[:len(options):len(options)] {
			options = append(options, append(append([]engine.Action(nil), o...), engine.Action{Kind: opt.kind, Spell: sp}))
		}
	}
	var fillers []*engine.Spell
	for _, base := range []int{Wrack, ShadowBolt, DrainLife} {
		if sp := best(base); sp != nil {
			fillers = append(fillers, sp)
		}
	}
	if len(fillers) == 0 {
		fillers = []*engine.Spell{nil}
	}
	// Drain Life can also stand by next to another filler, for the time the
	// health is too low for Life Tap. Those candidates come last: where the
	// standby spell adds nothing, the plain rotation stays the best.
	standby := best(DrainLife)
	var out [][]engine.Action
	for _, recover := range []*engine.Spell{nil, standby} {
		for _, filler := range fillers {
			if recover != nil && (filler == nil || filler.ID == recover.ID) {
				continue
			}
			for _, opt := range options {
				actions := append(append([]engine.Action(nil), dots...), opt...)
				if filler != nil {
					actions = append(actions, engine.Action{Kind: "filler", Spell: filler})
				}
				if recover != nil {
					actions = append(actions, engine.Action{Kind: "recover", Spell: recover})
				}
				out = append(out, append(actions, engine.Action{Kind: "wand"}))
			}
		}
		if standby == nil {
			break
		}
	}
	return out
}

// Choices is everything a rotation can be built from, for the rotation
// guide: every DoT is optional there, because a short fight does not pay
// for all of them, and Drain Life can stand by for the moments when the
// health is too low for Life Tap.
func Choices(cat *catalog.Catalog, known map[int]bool, level int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{Tail: []engine.Action{{Kind: "wand"}}, Recover: best(DrainLife)}
	for _, opt := range []struct {
		kind string
		base int
	}{{"dot", CurseOfAgony}, {"dot", Corruption}, {"dot", SiphonLife}, {"dot", Immolate}, {"cooldown", DeathCoil}} {
		if sp := best(opt.base); sp != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: opt.kind, Spell: sp})
		}
	}
	for _, base := range []int{Wrack, ShadowBolt, DrainLife} {
		if sp := best(base); sp != nil {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	return c
}

// ArmorRegen is the health per 5 seconds the character's armor spell gives
// in combat (Demon Armor, else Demon Skin) and the spell it comes from. The
// model takes the armor as always active.
func ArmorRegen(cat *catalog.Catalog, known map[int]bool, level int) (float64, *engine.Spell) {
	for _, base := range []int{DemonArmor, DemonSkin} {
		sp := cat.BestRank(base, known, level)
		if sp == nil {
			continue
		}
		for _, a := range cat.Auras[sp.ID] {
			if a.Aura == auraHealthRegenInCombat {
				return a.BP, sp
			}
		}
		return 0, sp
	}
	return 0, nil
}
