// Package mage models the Mage: Arcane, Fire, and Frost damage rotations
// including single-target nukes, multi-target AoE (Blizzard, Arcane Explosion,
// Flamestrike, Cone of Cold, Blast Wave), Mage Armor regeneration, and Evocation.
package mage

import (
	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

// First ranks of Mage rotation spells (Forever DB2 IDs).
const (
	Fireball        = 133
	Frostbolt       = 116
	FireBlast       = 2136
	ArcaneMissiles  = 5143
	ArcaneExplosion = 1449
	Flamestrike     = 2120
	Blizzard        = 10
	Evocation       = 12051
	ArcaneBlast     = 400574
	IceLance        = 1312002
	Pyroblast       = 11366
	Scorch          = 2948
	ArcaneWrath     = 1326079
	ConeOfCold      = 120
	BlastWave       = 11113
	FrostfireBolt   = 401502
	MageArmor       = 6117
)

const (
	auraPeriodicTrigger  = 23
	auraPeriodicDummy    = 226
	auraModManaRegenCast = 134
)

// Prepare links channeled and ground-effect Mage spells (Arcane Missiles, Blizzard,
// Flamestrike) with their separate pulse damage spells and flags AoE spells.
func Prepare(cat *catalog.Catalog) {
	cat.PrepareOnce(prepare)
}

func prepare(cat *catalog.Catalog) {
	// Arcane Missiles: aura 23 triggers the missile damage spell every tick.
	for _, id := range cat.Chains[ArcaneMissiles] {
		sp := cat.Spells[id]
		if sp == nil || sp.Periodic != nil {
			continue
		}
		for _, a := range cat.Auras[id] {
			pulse := cat.Spells[a.Trigger]
			if a.Aura != auraPeriodicTrigger || pulse == nil || pulse.Direct == nil {
				continue
			}
			raw := cat.RawEffects[id][a.Index]
			period := 1000
			if raw != nil && raw.PeriodMs > 0 {
				period = raw.PeriodMs
			}
			tick := *pulse.Direct
			tick.PeriodMs, tick.Crits = period, true
			sp.Periodic = &tick
			break
		}
	}
	// Blizzard: dummy aura 226 pulses every 1 s; damage spell shares name, class mask, and spell level.
	linkAreaPulse(cat, Blizzard, false)
	// Flamestrike: direct hit + ground DoT ticks from matching pulse spell (overlapping class mask).
	linkAreaPulse(cat, Flamestrike, true)

	for _, base := range []int{ArcaneExplosion, Flamestrike, ConeOfCold, BlastWave} {
		for _, id := range cat.Chains[base] {
			if sp := cat.Spells[id]; sp != nil {
				sp.Area = true
			}
		}
	}
}

func masksOverlap(a, b [4]uint32) bool {
	for i := range a {
		if a[i]&b[i] != 0 {
			return true
		}
	}
	return false
}

func linkAreaPulse(cat *catalog.Catalog, base int, allowDirect bool) {
	for _, id := range cat.Chains[base] {
		sp := cat.Spells[id]
		if sp == nil || sp.Periodic != nil || (!allowDirect && sp.Direct != nil) || sp.DurationMs <= 0 || cat.Name(id) == "" {
			continue
		}
		period := 0
		for _, a := range cat.Auras[id] {
			if raw := cat.RawEffects[id][a.Index]; a.Aura == auraPeriodicDummy && raw != nil {
				period = raw.PeriodMs
			}
		}
		var pulse *engine.Spell
		for otherID, other := range cat.Spells {
			if otherID == id || other.Direct == nil || !masksOverlap(other.ClassMask, sp.ClassMask) ||
				other.Levels.Spell != sp.Levels.Spell || cat.Name(otherID) != cat.Name(id) {
				continue
			}
			if pulse == nil || otherID < pulse.ID {
				pulse = other
			}
		}
		if pulse == nil || period <= 0 {
			continue
		}
		tick := *pulse.Direct
		tick.PeriodMs, tick.Crits = period, true
		sp.Periodic, sp.Area = &tick, true
	}
}

// ArmorRegen returns the casting mana regeneration percentage from Mage Armor if known.
func ArmorRegen(cat *catalog.Catalog, known map[int]bool, level int) (float64, *engine.Spell) {
	sp := cat.BestRank(MageArmor, known, level)
	if sp == nil {
		return 0, nil
	}
	for _, a := range cat.Auras[sp.ID] {
		if a.Aura == auraModManaRegenCast && a.BP > 0 {
			return a.BP, sp
		}
	}
	return 0, sp
}

func evocation(cat *catalog.Catalog, known map[int]bool, level int) *engine.Action {
	sp := cat.BestRank(Evocation, known, level)
	if sp == nil {
		return nil
	}
	// Evocation grants +1500% spirit regen (15x extra) over its 8 s channel.
	return &engine.Action{Kind: "mana", Spell: sp, ManaRegenMult: 15}
}

// AreaSpells are the best known ranks of AoE spells offered only against several enemies.
func AreaSpells(cat *catalog.Catalog, known map[int]bool, level int) []*engine.Spell {
	var out []*engine.Spell
	for _, base := range []int{Blizzard, ArcaneExplosion, Flamestrike, ConeOfCold, BlastWave} {
		if sp := cat.BestRank(base, known, level); sp != nil && sp.Area {
			out = append(out, sp)
		}
	}
	return out
}

// Choices returns the candidate building blocks for Mage rotations.
func Choices(cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{}
	for _, opt := range []struct {
		kind string
		base int
	}{
		{"cooldown", FireBlast},
		{"cooldown", IceLance},
		{"cooldown", ArcaneWrath},
	} {
		if sp := best(opt.base); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: opt.kind, Spell: sp})
		}
	}
	if enemies > 1 {
		if sp := best(Flamestrike); sp != nil && (sp.Direct != nil || sp.Periodic != nil) {
			c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
		}
		for _, base := range []int{ConeOfCold, BlastWave} {
			if sp := best(base); sp != nil && sp.Direct != nil {
				c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
			}
		}
	}
	for _, base := range []int{Frostbolt, Fireball, FrostfireBolt, ArcaneBlast, ArcaneMissiles, Scorch} {
		if sp := best(base); sp != nil && (sp.Direct != nil || sp.Periodic != nil) {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	if enemies > 1 {
		for _, base := range []int{Blizzard, ArcaneExplosion} {
			if sp := best(base); sp != nil && (sp.Direct != nil || sp.Periodic != nil) {
				c.Fillers = append(c.Fillers, sp)
			}
		}
	}
	if evo := evocation(cat, known, level); evo != nil {
		c.Tail = append(c.Tail, *evo)
	}
	c.Tail = append(c.Tail, engine.Action{Kind: "wand"})
	return c
}

// Rotations returns every single-target candidate rotation for the Mage.
func Rotations(cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	return Choices(cat, known, level, 1).Rotations()
}
