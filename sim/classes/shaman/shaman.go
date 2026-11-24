// Package shaman models the Shaman: Elemental caster DPS (Lightning Bolt, Chain
// Lightning, Lava Burst, Shocks) and Restoration healing (Healing Wave, Lesser
// Healing Wave, Chain Heal, Riptide, Mana Tide Totem). Phase 3 adds Enhancement melee.
package shaman

import (
	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

// First ranks of Shaman caster and healer spells (Forever DB2 IDs).
const (
	LightningBolt     = 403
	ChainLightning    = 421
	FlameShock        = 8050
	EarthShock        = 8042
	FrostShock        = 8056
	LavaBurst         = 408490
	HealingWave       = 331
	LesserHealingWave = 8004
	ChainHeal         = 1064
	Riptide           = 408521
	ManaTideTotem     = 16190
	Stormstrike       = 17364
)

// Prepare configures Enhancement melee scaling.
func Prepare(cat *catalog.Catalog) {
	cat.PrepareOnce(prepare)
}

func prepare(cat *catalog.Catalog) {
	for _, id := range cat.Chains[Stormstrike] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			if sp.CooldownMs == 0 {
				sp.CooldownMs = 8000
			}
		}
	}
}

var healChains = []int{HealingWave, LesserHealingWave, ChainHeal}

func manaTide(cat *catalog.Catalog, known map[int]bool, level int) *engine.Action {
	sp := cat.BestRank(ManaTideTotem, known, level)
	if sp == nil {
		return nil
	}
	// Mana Tide Totem restores 24% of max mana over 12 s on a 5 min cooldown.
	return &engine.Action{Kind: "mana", Spell: sp, ManaPctMax: 0.24}
}

// ChoicesForSpec returns Elemental or Enhancement damage rotation candidates.
func ChoicesForSpec(spec string, cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	if spec == "enhancement" {
		best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
		c := engine.Choices{}
		if sp := best(Stormstrike); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
		}
		if sp := best(FlameShock); sp != nil && (sp.Direct != nil || sp.Periodic != nil) {
			c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
		}
		if sp := best(EarthShock); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
		}
		c.Tail = append(c.Tail, engine.Action{Kind: "autoattack"})
		return c
	}
	return Choices(cat, known, level, enemies)
}

// RotationsForSpec returns Shaman rotation candidates for the given spec and metric.
func RotationsForSpec(spec, metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	if metric == "dps" {
		return ChoicesForSpec(spec, cat, known, level, 1).Rotations()
	}
	return Rotations(metric, cat, known, level)
}

// Choices returns the Elemental damage rotation candidates.
func Choices(cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{}
	if sp := best(FlameShock); sp != nil && (sp.Direct != nil || sp.Periodic != nil) {
		c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
	}
	for _, base := range []int{LavaBurst, ChainLightning, EarthShock} {
		if sp := best(base); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp})
		}
	}
	if sp := best(LightningBolt); sp != nil && sp.Direct != nil {
		c.Fillers = append(c.Fillers, sp)
	}
	if tide := manaTide(cat, known, level); tide != nil {
		c.Tail = append(c.Tail, *tide)
	}
	return c
}

// HealSources returns every known Shaman HoT and direct heal rank for the demand planner.
func HealSources(cat *catalog.Catalog, known map[int]bool, level, targets int) ([]engine.HealSource, *engine.Action) {
	var out []engine.HealSource
	for _, rip := range cat.KnownRanks(Riptide, known, level) {
		if rip.Periodic != nil {
			out = append(out, engine.HealSource{Action: engine.Action{Kind: "hot", Spell: rip}, Targets: targets, Group: Riptide})
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
	return out, manaTide(cat, known, level)
}

// Rotations returns the Shaman rotation candidates for the given metric.
func Rotations(metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	if metric == "dps" {
		return Choices(cat, known, level, 1).Rotations()
	}
	tide := manaTide(cat, known, level)
	var rots [][]engine.Action
	add := func(prefix []engine.Action, filler *engine.Spell) {
		r := append([]engine.Action{}, prefix...)
		r = append(r, engine.Action{Kind: "heal", Spell: filler})
		if tide != nil {
			r = append(r, *tide)
		}
		rots = append(rots, r)
	}
	var prefixes [][]engine.Action
	prefixes = append(prefixes, nil)
	for _, rip := range cat.KnownRanks(Riptide, known, level) {
		if rip.Periodic != nil {
			prefixes = append(prefixes, []engine.Action{{Kind: "hot", Spell: rip}})
		}
	}
	seen := map[int]bool{}
	for _, base := range healChains {
		for _, rank := range cat.KnownRanks(base, known, level) {
			if seen[rank.ID] || rank.Direct == nil {
				continue
			}
			seen[rank.ID] = true
			for _, p := range prefixes {
				add(p, rank)
			}
		}
	}
	return rots
}
