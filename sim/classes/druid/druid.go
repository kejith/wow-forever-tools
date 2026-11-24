// Package druid models the Druid: Balance caster DPS (Wrath, Starfire, Moonfire,
// Insect Swarm, Hurricane) and Restoration healing (Healing Touch, Regrowth,
// Rejuvenation, Wild Growth, Innervate). Phase 3 adds Feral Cat melee DPS.
package druid

import (
	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

// First ranks of Druid caster and healer spells (Forever DB2 IDs).
const (
	Wrath        = 5176
	Starfire     = 2912
	Moonfire     = 8921
	InsectSwarm  = 5570
	Hurricane    = 16914
	HealingTouch = 5185
	Regrowth     = 8936
	Rejuvenation  = 774
	WildGrowth    = 408120
	Innervate     = 29166
	Claw          = 1082
	Shred         = 5221
	Rake          = 1822
	Rip           = 1079
	FerociousBite = 1328618
)

const auraPeriodicDummy = 226

var healChains = []int{HealingTouch, Regrowth}

// Prepare links Hurricane with its periodic AoE pulse spell.
func Prepare(cat *catalog.Catalog) {
	cat.PrepareOnce(prepare)
}

func prepare(cat *catalog.Catalog) {
	for _, id := range cat.Chains[Hurricane] {
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
	// Feral Cat abilities: 1.0s GCD, physical scaling, and 5-CP finishers.
	for _, base := range []int{Claw, Shred, Rake} {
		for _, id := range cat.Chains[base] {
			if sp := cat.Spells[id]; sp != nil {
				sp.IsPhysical = true
				if sp.GCDMs == 0 {
					sp.GCDMs = 1000
				}
			}
		}
	}
	for _, id := range cat.Chains[Rip] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
			sp.APCoeff = 0.24
			if sp.DurationMs <= 0 {
				sp.DurationMs = 12000
			}
			if sp.GCDMs == 0 {
				sp.GCDMs = 1000
			}
		}
	}
	for _, id := range cat.Chains[FerociousBite] {
		if sp := cat.Spells[id]; sp != nil {
			sp.IsPhysical = true
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
}

func innervate(cat *catalog.Catalog, known map[int]bool, level int) *engine.Action {
	sp := cat.BestRank(Innervate, known, level)
	if sp == nil {
		return nil
	}
	// Innervate gives +400% spirit regen (4x extra) over 20 s.
	return &engine.Action{Kind: "mana", Spell: sp, ManaRegenMult: 4}
}

// AreaSpells are the best known ranks of Druid caster AoE spells.
func AreaSpells(cat *catalog.Catalog, known map[int]bool, level int) []*engine.Spell {
	if sp := cat.BestRank(Hurricane, known, level); sp != nil && sp.Area {
		return []*engine.Spell{sp}
	}
	return nil
}

// ChoicesForSpec returns Balance or Feral Cat damage rotation candidates.
func ChoicesForSpec(spec string, cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	if spec == "feral" {
		best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
		c := engine.Choices{}
		for _, base := range []int{Rake, Rip} {
			if sp := best(base); sp != nil && sp.Periodic != nil {
				c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
			}
		}
		if sp := best(FerociousBite); sp != nil && sp.Direct != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: "cooldown", Spell: sp, CycleSec: 12})
		}
		for _, base := range []int{Shred, Claw} {
			if sp := best(base); sp != nil && sp.Direct != nil {
				c.Fillers = append(c.Fillers, sp)
			}
		}
		c.Tail = append(c.Tail, engine.Action{Kind: "autoattack"})
		return c
	}
	return Choices(cat, known, level, enemies)
}

// RotationsForSpec returns Druid rotation candidates for the given spec and metric.
func RotationsForSpec(spec, metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	if metric == "dps" {
		return ChoicesForSpec(spec, cat, known, level, 1).Rotations()
	}
	return Rotations(metric, cat, known, level)
}

// Choices returns the Balance damage rotation candidates.
func Choices(cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{}
	for _, base := range []int{Moonfire, InsectSwarm} {
		if sp := best(base); sp != nil && (sp.Direct != nil || sp.Periodic != nil) {
			c.Optional = append(c.Optional, engine.Action{Kind: "dot", Spell: sp})
		}
	}
	for _, base := range []int{Starfire, Wrath} {
		if sp := best(base); sp != nil && sp.Direct != nil {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	if enemies > 1 {
		if sp := best(Hurricane); sp != nil && sp.Periodic != nil {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	if inn := innervate(cat, known, level); inn != nil {
		c.Tail = append(c.Tail, *inn)
	}
	return c
}

// HealSources returns every known Druid HoT and direct heal rank for the demand planner.
func HealSources(cat *catalog.Catalog, known map[int]bool, level, targets int) ([]engine.HealSource, *engine.Action) {
	var out []engine.HealSource
	for _, rej := range cat.KnownRanks(Rejuvenation, known, level) {
		if rej.Periodic != nil {
			out = append(out, engine.HealSource{Action: engine.Action{Kind: "hot", Spell: rej}, Targets: targets, Group: Rejuvenation})
		}
	}
	for _, wg := range cat.KnownRanks(WildGrowth, known, level) {
		if wg.Periodic != nil {
			out = append(out, engine.HealSource{Action: engine.Action{Kind: "hot", Spell: wg}, Targets: targets, Group: WildGrowth})
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
	return out, innervate(cat, known, level)
}

// Rotations returns the Druid rotation candidates for the given metric.
func Rotations(metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	if metric == "dps" {
		return Choices(cat, known, level, 1).Rotations()
	}
	inn := innervate(cat, known, level)
	var rots [][]engine.Action
	add := func(prefix []engine.Action, filler *engine.Spell) {
		r := append([]engine.Action{}, prefix...)
		r = append(r, engine.Action{Kind: "heal", Spell: filler})
		if inn != nil {
			r = append(r, *inn)
		}
		rots = append(rots, r)
	}
	var prefixes [][]engine.Action
	prefixes = append(prefixes, nil)
	for _, rej := range cat.KnownRanks(Rejuvenation, known, level) {
		if rej.Periodic != nil {
			prefixes = append(prefixes, []engine.Action{{Kind: "hot", Spell: rej}})
		}
	}
	if wg := cat.BestRank(WildGrowth, known, level); wg != nil && wg.Periodic != nil {
		prefixes = append(prefixes, []engine.Action{{Kind: "hot", Spell: wg}})
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
