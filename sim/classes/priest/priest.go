// Package priest models the Priest: a sustained healing cycle for Holy and
// Discipline (the engine picks the heal rank that pays best under the mana
// budget, with or without Power Word: Shield) and a Shadow damage rotation.
// Dark Sacrifice trades health for mana in both when the mana runs short.
package priest

import (
	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/engine"
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
)

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
	sp := cat.BestRank(PowerWordShield, known, level)
	if sp == nil || sp.Absorb == nil {
		return nil
	}
	weakened := float64(cat.DurationMs[WeakenedSoul]) / 1000
	if weakened <= 0 {
		weakened = 15
	}
	return &engine.Action{Kind: "cooldown", Spell: sp, CycleSec: weakened}
}

// healChains are the single-target heals a priest can fill with; every known
// rank of each chain is its own rotation candidate, and the engine keeps the
// one whose sustained healing under the mana budget is highest. That is how
// downranking enters the model; what an outgrown rank loses of its healing
// bonus is in the spell's numbers (engine.RankBonusShare).
var healChains = []int{LesserHeal, Heal, GreaterHeal, FlashHeal}

// Choices is everything a damage rotation can be built from, for the
// rotation guide: every DoT and Mind Blast with and without, Mind Flay or
// Smite as the filler.
func Choices(cat *catalog.Catalog, known map[int]bool, level int) engine.Choices {
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

// HealSources is what a healer can meet a demand for healing with: Renew and
// Power Word: Shield on up to that many targets (their best ranks), every
// known rank of the single-target heals, and Dark Sacrifice for the mana
// (nil if unknown). Heals that reach several targets at once (Prayer of
// Healing, Holy Nova) are not in the model.
func HealSources(cat *catalog.Catalog, known map[int]bool, level, targets int) ([]engine.HealSource, *engine.Action) {
	var out []engine.HealSource
	if renew := cat.BestRank(Renew, known, level); renew != nil {
		out = append(out, engine.HealSource{Action: engine.Action{Kind: "hot", Spell: renew}, Targets: targets})
	}
	if pws := shield(cat, known, level); pws != nil {
		out = append(out, engine.HealSource{Action: *pws, Targets: targets})
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

	// Healer: keep Renew up, fill with one single-target heal rank, each
	// with and without Power Word: Shield on the target whenever Weakened
	// Soul allows. In Forever each of the four heals is its own chain; should
	// a table link two of them anyway (explicit SupercedesSpell), a rank
	// still becomes one candidate at most.
	renew := cat.BestRank(Renew, known, level)
	prefixes := [][]engine.Action{nil}
	if pws := shield(cat, known, level); pws != nil {
		prefixes = append(prefixes, []engine.Action{*pws})
	}
	var out [][]engine.Action
	for _, prefix := range prefixes {
		before := len(out)
		seen := map[int]bool{}
		start := func() []engine.Action {
			actions := append([]engine.Action(nil), prefix...)
			if renew != nil {
				actions = append(actions, engine.Action{Kind: "hot", Spell: renew})
			}
			return actions
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
		if len(out) == before && (renew != nil || len(prefix) > 0) {
			finish(start())
		}
	}
	return out
}
