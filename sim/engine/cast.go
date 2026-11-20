package engine

import "math/rand"

// CastResult is what one cast of a spell is worth. With rng == nil every
// field is an expectation; with an rng it is one sampled outcome (the seam
// for the Monte-Carlo stage).
type CastResult struct {
	Damage      float64
	Healing     float64
	ManaCost    float64
	ExecuteSec  float64 // time the cast occupies (max of cast time and GCD)
	DurationSec float64 // periodic effects: duration after modifiers
}

const defaultGCDMs = 1500

// hitChance returns the chance (0..1) that a spell lands.
func hitChance(sp *Spell, st *State, tgt Target) float64 {
	miss, ok := SpellMiss[tgt.LevelDelta]
	if !ok {
		miss = SpellMiss[3]
	}
	miss -= st.Hit
	miss -= st.Mods.HitFlatByBase[sp.BaseID]
	if miss < MinMiss {
		miss = MinMiss
	}
	if miss > 100 {
		miss = 100
	}
	return 1 - miss/100
}

// ResolveCast evaluates one cast of a spell against the target.
// Heals use the healing bonus and never miss; damage rolls the spell hit
// table. Partial resists are not modeled in Stage 1 (noted in the docs).
func ResolveCast(sp *Spell, st *State, tgt Target, rng *rand.Rand) CastResult {
	r := CastResult{}

	// Time: talents may shorten the cast; haste shortens it further. The GCD
	// is not haste-scaled (Classic rule, unverified for Forever).
	castMs := float64(sp.CastMs) + st.Mods.flat(OpCastTime, sp)
	castMs *= st.Mods.pct(OpCastTime, sp)
	if castMs < 0 {
		castMs = 0
	}
	castSec := castMs / 1000 / (1 + st.HastePct/100)
	gcdMs := sp.GCDMs
	if gcdMs <= 0 {
		gcdMs = defaultGCDMs
	}
	gcdSec := float64(gcdMs) / 1000
	r.ExecuteSec = castSec
	if gcdSec > r.ExecuteSec {
		r.ExecuteSec = gcdSec
	}
	if sp.Channeled && sp.DurationMs > 0 { // a channel occupies its full duration (not haste-scaled in Classic)
		if chanSec := float64(sp.DurationMs) / 1000; chanSec > r.ExecuteSec {
			r.ExecuteSec = chanSec
		}
	}

	// Cost.
	cost := sp.Cost.Mana + st.Mods.flat(OpCost, sp)
	cost *= st.Mods.pct(OpCost, sp)
	if cost < 0 {
		cost = 0
	}
	r.ManaCost = cost

	// Power and modifiers.
	power := st.SP[sp.School]
	if sp.IsHeal {
		power = st.Healing
	}
	dmgPct := (1 + st.Mods.SchoolDamagePct[sp.School]/100) * st.Mods.pct(OpDamage, sp)
	if sp.IsHeal { // talent spell mods (e.g. Spiritual Healing) hit heals through the same op
		dmgPct = (1 + st.Mods.HealingPct/100) * st.Mods.pct(OpDamage, sp)
	}
	crit := st.Crit[sp.School] + st.Mods.flat(OpCrit, sp)
	if crit < 0 {
		crit = 0
	}
	hc := 1.0
	if !sp.IsHeal {
		hc = hitChance(sp, st, tgt)
	}

	land := hc
	if rng != nil {
		if rng.Float64() < hc {
			land = 1
		} else {
			land = 0
		}
	}

	// Direct portion.
	if sp.Direct != nil {
		avg := sp.Direct.avgBase(st.Level, sp.Levels)
		if rng != nil && sp.Direct.Variance > 0 {
			span := avg * sp.Direct.Variance
			avg = avg - span/2 + rng.Float64()*span
		}
		val := (avg + st.Mods.flat(OpDamage, sp) + sp.Direct.Coeff*power) * dmgPct
		critFactor := 1 + crit/100*SpellCritBonus
		if rng != nil {
			critFactor = 1.0
			if rng.Float64() < crit/100 {
				critFactor = 1 + SpellCritBonus
			}
		}
		val *= critFactor * land
		if sp.IsHeal {
			r.Healing += val
		} else {
			r.Damage += val
		}
	}

	// Periodic portion: binary application (the whole DoT/HoT misses or
	// lands), value per tick; DoTs crit only when a talent says so.
	if sp.Periodic != nil && sp.DurationMs > 0 {
		durMs := float64(sp.DurationMs) + st.Mods.flat(OpDuration, sp)
		durMs *= st.Mods.pct(OpDuration, sp)
		r.DurationSec = durMs / 1000
		ticks := durMs / float64(sp.Periodic.PeriodMs)
		tick := (sp.Periodic.avgBase(st.Level, sp.Levels) + sp.Periodic.Coeff*power) * dmgPct
		total := ticks * tick
		if !sp.IsHeal && st.Mods.dotMayCrit(sp.School) {
			total *= 1 + crit/100*SpellCritBonus
		}
		total *= land
		if sp.IsHeal {
			r.Healing += total
		} else {
			r.Damage += total
		}
	}
	return r
}
