// Package rogue models the Rogue: Assassination, Combat, and Subtlety melee DPS
// rotations (Dual-Wield Auto-Attacks, Sinister Strike, Backstab, Hemorrhage,
// Ghostly Strike, Shank, Rupture, and Eviscerate under the 10 energy/s budget).
package rogue

import (
	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

// First ranks of Rogue DPS abilities (Forever DB2 IDs).
const (
	SinisterStrike = 1752
	Backstab       = 53
	Hemorrhage     = 16511
	GhostlyStrike  = 14278
	Shank          = 1329283
	Eviscerate     = 2098
	Rupture        = 1943
	SliceAndDice   = 5171
)

// Prepare configures Rogue physical scaling and finisher cycles (5 combo points).
func Prepare(cat *catalog.Catalog) {
	cat.PrepareOnce(prepare)
}

func prepare(cat *catalog.Catalog) {
	for _, id := range cat.Chains[Eviscerate] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			// 5-combo-point Eviscerate scales with ~5x per-point base + 15% AP.
			sp.APCoeff = 0.15
			if sp.Direct != nil {
				d := *sp.Direct
				d.BP *= 4.5
				d.PerLevel *= 4.5
				sp.Direct = &d
			}
			if sp.GCDMs == 0 {
				sp.GCDMs = 1000
			}
		}
	}
	for _, id := range cat.Chains[Rupture] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			sp.APCoeff = 0.24
			if sp.DurationMs <= 8000 {
				sp.DurationMs = 16000 // 5-combo-point duration
			}
			if sp.GCDMs == 0 {
				sp.GCDMs = 1000
			}
		}
	}
	for _, base := range []int{SinisterStrike, Backstab, Hemorrhage, GhostlyStrike, Shank} {
		for _, id := range cat.Chains[base] {
			if sp := cat.Spells[id]; sp != nil {
				sp.IsPhysical = true
				if sp.GCDMs == 0 {
					sp.GCDMs = 1000
				}
			}
		}
	}
}

// SliceAndDiceHaste returns the passive melee haste bonus if Slice and Dice is known.
func SliceAndDiceHaste(cat *catalog.Catalog, known map[int]bool, level int) (float64, *engine.Spell) {
	sp := cat.BestRank(SliceAndDice, known, level)
	if sp == nil {
		return 0, nil
	}
	// Rank 1 gives 20% melee attack speed, Rank 2 gives 30% (modeled as ~90% uptime).
	pct := 18.0
	if cat.RankNumber(sp.ID) >= 2 {
		pct = 27.0
	}
	return pct, sp
}

// Choices returns Rogue DPS rotation candidates.
func Choices(cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{}
	if sp := best(Rupture); sp != nil && sp.Periodic != nil {
		c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
	}
	if sp := best(Eviscerate); sp != nil && sp.Direct != nil {
		// 5-CP finisher spent roughly every 12 s of builder cadence.
		c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp, CycleSec: 12})
	}
	for _, base := range []int{GhostlyStrike, Shank} {
		if sp := best(base); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
		}
	}
	for _, base := range []int{SinisterStrike, Backstab, Hemorrhage} {
		if sp := best(base); sp != nil && sp.Direct != nil {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	c.Tail = append(c.Tail, engine.Action{Kind: "autoattack"})
	return c
}

// Rotations returns single-target Rogue DPS rotations.
func Rotations(cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	return Choices(cat, known, level, 1).Rotations()
}
