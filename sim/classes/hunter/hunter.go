// Package hunter models the Hunter: Beast Mastery, Marksmanship, and Survival
// ranged DPS rotations (Auto Shot, Aimed Shot, Multi-Shot, Sniper Shot, Arcane
// Shot, Serpent Sting, plus pet damage).
package hunter

import (
	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

// First ranks of Hunter ranged abilities (Forever DB2 IDs).
const (
	SerpentSting = 1978
	ArcaneShot   = 3044
	MultiShot    = 2643
	AimedShot    = 19434
	SniperShot   = 1310687
	Volley       = 1510
)

const auraPeriodicDummy = 226

// Prepare flags Hunter shots as ranged physical/magic attacks with proper cooldowns
// and links Volley with its periodic AoE pulse spell.
func Prepare(cat *catalog.Catalog) {
	cat.PrepareOnce(prepare)
}

func prepare(cat *catalog.Catalog) {
	rangedCDs := map[int]int{
		AimedShot:  6000,
		MultiShot:  10000,
		ArcaneShot: 6000,
		SniperShot: 15000,
	}
	for base, defCD := range rangedCDs {
		for _, id := range cat.Chains[base] {
			if sp := cat.Spells[id]; sp != nil {
				sp.IsPhysical = true
				sp.IsRanged = true
				if sp.CooldownMs == 0 {
					sp.CooldownMs = defCD
				}
				if base == MultiShot {
					sp.Area = true
					sp.AreaMax = 3
				}
				if base == ArcaneShot {
					sp.APCoeff = 0.15
				}
			}
		}
	}
	for _, id := range cat.Chains[SerpentSting] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			sp.IsRanged = true
			sp.APCoeff = 0.10
		}
	}
	for _, id := range cat.Chains[Volley] {
		sp := cat.Spells[id]
		if sp == nil || sp.Direct != nil || sp.Periodic != nil || sp.DurationMs <= 0 || cat.Name(id) == "" {
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
			if otherID == id || other.Direct == nil || other.ClassMask != sp.ClassMask ||
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

// AreaSpells returns Hunter multi-target abilities.
func AreaSpells(cat *catalog.Catalog, known map[int]bool, level int) []*engine.Spell {
	var out []*engine.Spell
	for _, base := range []int{Volley, MultiShot} {
		if sp := cat.BestRank(base, known, level); sp != nil && sp.Area {
			out = append(out, sp)
		}
	}
	return out
}

// Choices returns Hunter ranged DPS rotation candidates.
func Choices(cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{}
	for _, base := range []int{AimedShot, MultiShot, SniperShot, ArcaneShot} {
		if sp := best(base); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
		}
	}
	if sp := best(SerpentSting); sp != nil && sp.Periodic != nil {
		c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
	}
	if enemies > 1 {
		if sp := best(Volley); sp != nil && sp.Area && sp.Periodic != nil {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	// Auto Shot and Pet run continuously between cooldown shots.
	c.Tail = append(c.Tail, engine.Action{Kind: "autoshot"}, engine.Action{Kind: "pet"})
	return c
}

// Rotations returns single-target Hunter DPS rotations.
func Rotations(cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	return Choices(cat, known, level, 1).Rotations()
}
