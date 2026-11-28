// Package warrior models the Warrior: Arms and Fury melee DPS rotations
// (Auto-Attacks with MH/OH, Mortal Strike, Bloodthirst, Whirlwind, Overpower,
// Rend, Heroic Strike, Slam, and Cleave against multiple enemies).
package warrior

import (
	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

// First ranks of Warrior DPS abilities (Forever DB2 IDs).
const (
	HeroicStrike = 78
	Rend         = 772
	Overpower    = 7384
	Cleave       = 845
	Slam         = 1240193
	Whirlwind    = 1680
	Bloodthirst  = 23881
	MortalStrike = 12294
	ThunderClap  = 6343
)

// Prepare configures Warrior physical scaling, AoE flags, and fallback cooldowns.
func Prepare(cat *catalog.Catalog) {
	cat.PrepareOnce(prepare)
}

func prepare(cat *catalog.Catalog) {
	for _, id := range cat.Chains[Bloodthirst] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			sp.APCoeff = 0.45
			if sp.CooldownMs == 0 {
				sp.CooldownMs = 6000
			}
		}
	}
	for _, id := range cat.Chains[MortalStrike] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			if sp.CooldownMs == 0 {
				sp.CooldownMs = 6000
			}
		}
	}
	for _, id := range cat.Chains[Whirlwind] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			sp.Area = true
			sp.AreaMax = 4
			if sp.CooldownMs == 0 {
				sp.CooldownMs = 10000
			}
		}
	}
	for _, id := range cat.Chains[Overpower] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			if sp.CooldownMs == 0 {
				sp.CooldownMs = 5000
			}
		}
	}
	for _, id := range cat.Chains[Cleave] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			sp.Area = true
			sp.AreaMax = 2
		}
	}
	for _, id := range cat.Chains[Rend] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			sp.APCoeff = 0.15
		}
	}
}

// AreaSpells returns Warrior multi-target abilities.
func AreaSpells(cat *catalog.Catalog, known map[int]bool, level int) []*engine.Spell {
	var out []*engine.Spell
	for _, base := range []int{Whirlwind, Cleave} {
		if sp := cat.BestRank(base, known, level); sp != nil && sp.Area {
			out = append(out, sp)
		}
	}
	return out
}

// Choices returns candidate Arms/Fury DPS rotation building blocks.
func Choices(cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{}
	for _, base := range []int{MortalStrike, Bloodthirst, Whirlwind} {
		if sp := best(base); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
		}
	}
	if sp := best(Rend); sp != nil && sp.Periodic != nil {
		c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
	}
	for _, base := range []int{HeroicStrike, Slam} {
		if sp := best(base); sp != nil && sp.Direct != nil {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	if enemies > 1 {
		if sp := best(Cleave); sp != nil && sp.Direct != nil {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	c.Tail = append(c.Tail, engine.Action{Kind: "autoattack"})
	return c
}

// Rotations returns single-target Warrior DPS rotations.
func Rotations(cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	return Choices(cat, known, level, 1).Rotations()
}
