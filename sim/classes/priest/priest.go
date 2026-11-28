// Package priest models the Priest: a sustained healing cycle for Holy and
// Discipline (the engine picks the heal rank that pays best under the mana
// budget, with or without Power Word: Shield) and a Shadow damage rotation.
// Dark Sacrifice trades health for mana in both when the mana runs short.
package priest

import (
	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
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
	PowerWordShield = 17
	WeakenedSoul    = 6788    // the debuff that keeps a target from being shielded again
	DarkSacrifice   = 1277324 // Forever: health over time for mana
	HolyNova        = 15237   // instant AoE damage
	MindSear        = 413259  // Forever: channeled Shadow AoE
)

const auraPeriodicTrigger = 23

// Prepare links Mind Sear with its periodic AoE trigger spell and flags Holy Nova as AoE.
func Prepare(cat *catalog.Catalog) {
	cat.PrepareOnce(prepare)
}

func prepare(cat *catalog.Catalog) {
	for _, id := range cat.Chains[MindSear] {
		sp := cat.Spells[id]
		if sp == nil || sp.Periodic != nil {
			continue
		}
		for _, a := range cat.Auras[id] {
			pulse := cat.Spells[a.Trigger]
			if a.Aura != auraPeriodicTrigger || pulse == nil || pulse.Direct == nil {
				continue
			}
			period := 1000
			if raw := cat.RawEffects[id][a.Index]; raw != nil && raw.PeriodMs > 0 {
				period = raw.PeriodMs
			}
			tick := *pulse.Direct
			tick.PeriodMs, tick.Crits = period, true
			sp.Periodic, sp.Area, sp.Channeled = &tick, true, true
			break
		}
	}
	for _, id := range cat.Chains[HolyNova] {
		if sp := cat.Spells[id]; sp != nil && sp.Direct != nil {
			sp.Area = true
		}
	}
}

// AreaSpells are the best known ranks of Priest AoE spells offered only against several enemies.
func AreaSpells(cat *catalog.Catalog, known map[int]bool, level int) []*engine.Spell {
	var out []*engine.Spell
	for _, base := range []int{MindSear, HolyNova} {
		if sp := cat.BestRank(base, known, level); sp != nil && sp.Area {
			out = append(out, sp)
		}
	}
	return out
}

// darkSacrificeManaPerSpirit: the spell text adds the caster's spirit to the
// mana it returns ("to gain ${$o2+$SPI} Mana").
const darkSacrificeManaPerSpirit = 1

// darkSacrifice is the "mana" action: the periodic energize is decoded into
// the spell, the health it costs is the dummy effect at index 0.
func darkSacrifice(cat *catalog.Catalog, known map[int]bool, level int) *engine.Action {
	sp := cat.BestRank(DarkSacrifice, known, level)
	if sp == nil || sp.Energize == nil {
		return nil
	}
	return &engine.Action{Kind: "mana", Spell: sp, HealthCost: cat.RawEffects[sp.ID][0],
		ManaPerSpirit: darkSacrificeManaPerSpirit}
}

// shield is Power Word: Shield as a rotation entry. The model heals one
// target, so Weakened Soul sets the pace (its duration, 15 sec), whatever
// the spell's own cooldown is.
func shield(cat *catalog.Catalog, known map[int]bool, level int) *engine.Action {
	return shieldOf(cat, cat.BestRank(PowerWordShield, known, level))
}

func shieldOf(cat *catalog.Catalog, sp *engine.Spell) *engine.Action {
	if sp == nil || sp.Absorb == nil {
		return nil
	}
	return &engine.Action{Kind: "cooldown", Spell: sp, CycleSec: WeakenedSoulSec(cat)}
}

// WeakenedSoulSec is how long a target cannot be shielded again.
func WeakenedSoulSec(cat *catalog.Catalog) float64 {
	if weakened := float64(cat.DurationMs[WeakenedSoul]) / 1000; weakened > 0 {
		return weakened
	}
	return 15
}

// healChains are the single-target heals a priest can fill with; every known
// rank of each chain is its own rotation candidate, and the engine keeps the
// one whose sustained healing under the mana budget is highest. That is how
// downranking enters the model; what an outgrown rank loses of its healing
// bonus is in the spell's numbers (engine.RankBonusShare).
var healChains = []int{LesserHeal, Heal, GreaterHeal, FlashHeal}

// Choices is everything a damage rotation can be built from, for the
// rotation guide: every DoT and Mind Blast with and without, Mind Flay or
// Smite as the filler, plus Mind Sear and Holy Nova against several enemies.
func Choices(cat *catalog.Catalog, known map[int]bool, level, enemies int) engine.Choices {
	best := func(base int) *engine.Spell { return cat.BestRank(base, known, level) }
	c := engine.Choices{}
	for _, opt := range []struct {
		kind string
		base int
	}{{"dot", ShadowWordPain}, {"dot", HolyFire}, {"dot", DevouringPlague}, {"cooldown", MindBlast}} {
		if sp := best(opt.base); sp != nil {
			c.Optional = append(c.Optional, engine.Action{Kind: opt.kind, Spell: sp})
		}
	}
	if enemies > 1 {
		for _, base := range []int{MindSear, HolyNova} {
			if sp := best(base); sp != nil && sp.Area && (sp.Direct != nil || sp.Periodic != nil) {
				c.Fillers = append(c.Fillers, sp)
			}
		}
	}
	for _, base := range []int{MindFlay, Smite} {
		if sp := best(base); sp != nil {
			c.Fillers = append(c.Fillers, sp)
		}
	}
	if mana := darkSacrifice(cat, known, level); mana != nil {
		c.Tail = append(c.Tail, *mana)
	}
	c.Tail = append(c.Tail, engine.Action{Kind: "wand"})
	return c
}

// HealSources is what a healer can meet a demand for healing with: every
// known rank of Renew and of Power Word: Shield on up to that many targets
// (a target carries one Renew and one shield at a time, whatever the rank:
// a low rank of Renew keeps most of the healing bonus for a fraction of the
// mana), every known rank of the single-target heals, and Dark Sacrifice
// for the mana (nil if unknown). Heals that reach several targets at once
// (Prayer of Healing, Holy Nova) are not in the model.
func HealSources(cat *catalog.Catalog, known map[int]bool, level, targets int) ([]engine.HealSource, *engine.Action) {
	var out []engine.HealSource
	for _, renew := range cat.KnownRanks(Renew, known, level) {
		out = append(out, engine.HealSource{Action: engine.Action{Kind: "hot", Spell: renew}, Targets: targets, Group: Renew})
	}
	for _, rank := range cat.KnownRanks(PowerWordShield, known, level) {
		if pws := shieldOf(cat, rank); pws != nil {
			out = append(out, engine.HealSource{Action: *pws, Targets: targets, Group: PowerWordShield})
		}
	}
	seen := map[int]bool{}
	for _, base := range healChains {
		for _, rank := range cat.KnownRanks(base, known, level) {
			if !seen[rank.ID] {
				seen[rank.ID] = true
				out = append(out, engine.HealSource{Action: engine.Action{Kind: "heal", Spell: rank}})
			}
		}
	}
	return out, darkSacrifice(cat, known, level)
}

// Rotations returns the rotation candidates for a metric.
func Rotations(metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	mana := darkSacrifice(cat, known, level)
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
				if mana != nil {
					actions = append(actions, *mana)
				}
				out = append(out, append(actions, engine.Action{Kind: "wand"}))
			}
		}
		return out
	}

	// Healer: fill with one single-target heal rank, each with and without
	// Power Word: Shield on the target whenever Weakened Soul allows, and
	// with every known rank of Renew kept up or none (a low rank keeps most
	// of the healing bonus for a fraction of the mana; the highest one may
	// cost more per point than the heal). In Forever each of the four heals
	// is its own chain; should a table link two of them anyway (explicit
	// SupercedesSpell), a rank still becomes one candidate at most.
	var prefixes [][]engine.Action
	shields := [][]engine.Action{nil}
	if pws := shield(cat, known, level); pws != nil {
		shields = append(shields, []engine.Action{*pws})
	}
	renews := append([]*engine.Spell{nil}, cat.KnownRanks(Renew, known, level)...)
	for _, s := range shields {
		for _, renew := range renews {
			prefix := append([]engine.Action(nil), s...)
			if renew != nil {
				prefix = append(prefix, engine.Action{Kind: "hot", Spell: renew})
			}
			prefixes = append(prefixes, prefix)
		}
	}
	var out [][]engine.Action
	for _, prefix := range prefixes {
		before := len(out)
		seen := map[int]bool{}
		start := func() []engine.Action {
			return append([]engine.Action(nil), prefix...)
		}
		finish := func(actions []engine.Action) {
			if mana != nil {
				actions = append(actions, *mana)
			}
			out = append(out, actions)
		}
		for _, base := range healChains {
			for _, rank := range cat.KnownRanks(base, known, level) {
				if seen[rank.ID] {
					continue
				}
				seen[rank.ID] = true
				finish(append(start(), engine.Action{Kind: "heal", Spell: rank}))
			}
		}
		if len(out) == before && len(prefix) > 0 {
			finish(start())
		}
	}
	return out
}
