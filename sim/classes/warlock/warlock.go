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
	// A curse and a bane do not exclude each other in Forever, so Curse of
	// the Elements goes next to Bane of Agony.
	CurseOfElements = 440892
	SearingPain     = 5676
	SoulFire        = 6353 // one-minute cooldown; costs a Soul Shard, which the model does not count
	DrainSoul       = 1120
)

// auraDamageTakenPct is the aura of a debuff that raises the damage its
// target takes: percent in the base points, school mask in the first misc
// value (Curse of the Elements).
const auraDamageTakenPct = 87

// fillerBases are the spells that can fill the time between the DoTs, in
// the order they are tried.
var fillerBases = []int{Wrack, ShadowBolt, DrainLife, SearingPain, DrainSoul}

// debuff is the action that keeps a damage-taken debuff on the enemy, if
// the spell is one.
func debuff(cat *catalog.Catalog, sp *engine.Spell) (engine.Action, bool) {
	if sp == nil {
		return engine.Action{}, false
	}
	for _, a := range cat.Auras[sp.ID] {
		if a.Aura == auraDamageTakenPct && a.BP > 0 && sp.DurationMs > 0 {
			return engine.Action{Kind: "debuff", Spell: sp, TakenPct: a.BP, TakenMask: uint32(a.Misc0)}, true
		}
	}
	return engine.Action{}, false
}

// optional are the spells a rotation can do with or without, in priority
// order: the curse first, so that everything after it lands under it.
func optional(cat *catalog.Catalog, best func(int) *engine.Spell, bases ...int) []engine.Action {
	var out []engine.Action
	if curse, ok := debuff(cat, best(CurseOfElements)); ok {
		out = append(out, curse)
	}
	kinds := map[int]string{CurseOfAgony: "dot", Corruption: "dot", SiphonLife: "dot", Immolate: "dot",
		DeathCoil: "cooldown", SoulFire: "cooldown"}
	for _, base := range bases {
		if sp := best(base); sp != nil {
			out = append(out, engine.Action{Kind: kinds[base], Spell: sp})
		}
	}
	return out
}

// auraHealthRegenInCombat is the aura of Demon Skin/Armor that regenerates
// health in combat (points per 5 seconds).
const auraHealthRegenInCombat = 161

// DefaultMetric is what a Warlock optimizes for.
const DefaultMetric = "dps"

// Rotations builds the candidate priority lists from what the character
// actually knows: the three DoTs always; Curse of the Elements, Immolate,
// Death Coil and Soul Fire each with and without; and one of the fillers,
// each of them but Drain Life also with Drain Life standing by.
func Rotations(cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	var dots []engine.Action
	for _, base := range []int{CurseOfAgony, Corruption, SiphonLife} {
		if sp := best(base); sp != nil {
			dots = append(dots, engine.Action{Kind: "dot", Spell: sp})
		}
	}
	// Each optional spell doubles the candidates: without it and with it.
	// The curse goes in front of the DoTs, the others behind them.
	type choice struct{ front, back []engine.Action }
	options := []choice{{}}
	for _, opt := range optional(cat, best, Immolate, DeathCoil, SoulFire) {
		for _, o := range options[:len(options):len(options)] {
			with := choice{append([]engine.Action(nil), o.front...), append([]engine.Action(nil), o.back...)}
			if opt.Kind == "debuff" {
				with.front = append(with.front, opt)
			} else {
				with.back = append(with.back, opt)
			}
			options = append(options, with)
		}
	}
	var fillers []*engine.Spell
	for _, base := range fillerBases {
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
				actions := append(append(append([]engine.Action(nil), opt.front...), dots...), opt.back...)
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
	c.Optional = optional(cat, best, CurseOfAgony, Corruption, SiphonLife, Immolate, DeathCoil, SoulFire)
	for _, base := range fillerBases {
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
