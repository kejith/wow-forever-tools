// Package paladin models the Paladin: Holy healing (Holy Light, Flash of Light,
// Holy Shock, Illumination crit refunds, Blessing of Wisdom) and Holy spell damage
// (Exorcism, Hammer of Wrath, Consecration, Holy Wrath). Phase 3 adds Retribution melee.
package paladin

import (
	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

// First ranks of Paladin spells (Forever DB2 IDs).
const (
	HolyLight        = 635
	FlashOfLight     = 19750
	HolyShockHeal    = 25914
	HolyShockDmg     = 25912
	Exorcism         = 879
	HammerOfWrath    = 24275
	Consecration     = 26573
	HolyWrath        = 2812
	BlessingOfWisdom = 19742
	HolyStrike       = 679
	HammerRighteous  = 407632
)

const (
	auraPeriodicDummy = 226
	auraPeriodicMana  = 24
)

var healChains = []int{HolyLight, FlashOfLight}

// Prepare links Consecration with its periodic AoE pulse spell and flags Holy Wrath as AoE.
func Prepare(cat *catalog.Catalog) {
	cat.PrepareOnce(prepare)
}

func prepare(cat *catalog.Catalog) {
	for _, id := range cat.Chains[Consecration] {
		sp := cat.Spells[id]
		if sp == nil || sp.Periodic != nil || sp.DurationMs <= 0 || cat.Name(id) == "" {
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
		tick.PeriodMs, tick.Crits = period, false
		sp.Periodic, sp.Area = &tick, true
	}
	for _, id := range cat.Chains[HolyWrath] {
		if sp := cat.Spells[id]; sp != nil {
			sp.Area = true
		}
	}
	for _, id := range cat.Chains[HolyStrike] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			if sp.CooldownMs == 0 {
				sp.CooldownMs = 6000
			}
		}
	}
}

// AreaSpells are the best known ranks of Paladin AoE spells.
func AreaSpells(cat *catalog.Catalog, known map[int]bool, level int) []*engine.Spell {
	var out []*engine.Spell
	for _, base := range []int{Consecration, HolyWrath} {
		if sp := cat.BestRank(base, known, level); sp != nil && sp.Area {
			out = append(out, sp)
		}
	}
	return out
}

// ChoicesForSpec returns Holy or Retribution damage rotation candidates.
func ChoicesForSpec(spec string, cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	if spec == "retribution" || spec == "" {
		best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
		c := engine.Choices{}
		for _, base := range []int{HolyStrike, Exorcism, HammerOfWrath} {
			if sp := best(base); sp != nil && sp.Direct != nil {
				c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
			}
		}
		if sp := best(Consecration); sp != nil && sp.Periodic != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
		}
		if enemies > 1 {
			if sp := best(HolyWrath); sp != nil && sp.Direct != nil {
				c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
			}
		}
		c.Tail = append(c.Tail, engine.Action{Kind: "autoattack"})
		return c
	}
	return Choices(cat, known, level, enemies)
}

// RotationsForSpec returns Paladin rotation candidates for the given spec and metric.
func RotationsForSpec(spec, metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	if metric == "dps" {
		return ChoicesForSpec(spec, cat, known, level, 1).Rotations()
	}
	return Rotations(metric, cat, known, level)
}

// Choices returns the Holy spell damage rotation candidates (Phase 3 extends with Retribution melee).
func Choices(cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{}
	for _, base := range []int{Exorcism, HammerOfWrath, HolyShockDmg} {
		if sp := best(base); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
		}
	}
	if sp := best(Consecration); sp != nil && sp.Periodic != nil {
		c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
	}
	if enemies > 1 {
		if sp := best(HolyWrath); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
		}
	}
	// Ensure at least one filler exists when no melee swing is modeled yet.
	if sp := best(Exorcism); sp != nil && sp.Direct != nil {
		c.Fillers = append(c.Fillers, sp)
	} else if sp := best(Consecration); sp != nil && sp.Periodic != nil {
		c.Fillers = append(c.Fillers, sp)
	}
	return c
}

// HealSources returns every known Paladin heal rank for the demand planner.
func HealSources(cat *catalog.Catalog, known map[int]bool, level, targets int) ([]engine.HealSource, *engine.Action) {
	var out []engine.HealSource
	for _, hs := range cat.KnownRanks(HolyShockHeal, known, level) {
		if hs.Direct != nil {
			out = append(out, engine.HealSource{Action: engine.Action{Kind: "cooldown", Spell: hs}, Targets: targets, Group: HolyShockHeal})
		}
	}
	seen := map[int]bool{}
	for _, base := range healChains {
		for _, rank := range cat.KnownRanks(base, known, level) {
			if seen[rank.ID] || rank.Direct == nil {
				continue
			}
			seen[rank.ID] = true
			out = append(out, engine.HealSource{Action: engine.Action{Kind: "heal", Spell: rank}})
		}
	}
	return out, nil
}

// Rotations returns the Paladin rotation candidates for the given metric.
func Rotations(metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	if metric == "dps" {
		return Choices(cat, known, level, 1).Rotations()
	}
	var rots [][]engine.Action
	var prefixes [][]engine.Action
	prefixes = append(prefixes, nil)
	if hs := cat.BestRank(HolyShockHeal, known, level); hs != nil && hs.Direct != nil {
		prefixes = append(prefixes, []engine.Action{{Kind: "cooldown", Spell: hs}})
	}
	seen := map[int]bool{}
	for _, base := range healChains {
		for _, rank := range cat.KnownRanks(base, known, level) {
			if seen[rank.ID] || rank.Direct == nil {
				continue
			}
			seen[rank.ID] = true
			for _, p := range prefixes {
				r := append([]engine.Action{}, p...)
				r = append(r, engine.Action{Kind: "heal", Spell: rank})
				rots = append(rots, r)
			}
		}
	}
	return rots
}
