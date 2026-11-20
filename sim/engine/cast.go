package engine

import "math/rand"

// CastResult is what one cast of a spell is worth. With rng == nil every
// field is an expectation; with an rng it is one sampled outcome.
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

// CastShape is one spell's numbers against a target, all modifiers applied:
// what the deterministic cycle and the Monte-Carlo loop share. Direct values
// are pre-crit and pre-hit; the caller rolls or weighs those.
type CastShape struct {
	ExecuteSec  float64
	GCDSec      float64
	CooldownSec float64
	ManaCost    float64
	HitChance   float64 // 0..1; 1 for heals
	CritPct     float64 // chance in percent
	CritBonus   float64 // extra damage factor on a crit (0.5 for spells)
	IsHeal      bool
	// Direct portion (0 when absent): the modded average and its variance.
	DirectAvg      float64
	DirectVariance float64
	// Periodic portion (Ticks 0 when absent): modded value per tick; DotCrit
	// says whether ticks crit (weighed in per tick, not rolled).
	TickValue   float64
	Ticks       float64
	PeriodSec   float64
	DurationSec float64
}

// Shape computes a spell's cast shape for this state and target.
func Shape(sp *Spell, st *State, tgt Target) CastShape {
	s := CastShape{IsHeal: sp.IsHeal, CritBonus: SpellCritBonus}

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
	s.GCDSec = float64(gcdMs) / 1000
	s.ExecuteSec = castSec
	if s.GCDSec > s.ExecuteSec {
		s.ExecuteSec = s.GCDSec
	}
	if sp.Channeled && sp.DurationMs > 0 { // a channel occupies its full duration (not haste-scaled in Classic)
		if chanSec := float64(sp.DurationMs) / 1000; chanSec > s.ExecuteSec {
			s.ExecuteSec = chanSec
		}
	}
	s.CooldownSec = float64(sp.CooldownMs) / 1000

	// Cost.
	cost := sp.Cost.Mana + st.Mods.flat(OpCost, sp)
	cost *= st.Mods.pct(OpCost, sp)
	if cost < 0 {
		cost = 0
	}
	s.ManaCost = cost

	// Power and modifiers.
	power := st.SP[sp.School]
	dmgPct := (1 + st.Mods.SchoolDamagePct[sp.School]/100) * st.Mods.pct(OpDamage, sp)
	if sp.IsHeal { // talent spell mods (e.g. Spiritual Healing) hit heals through the same op
		power = st.Healing
		dmgPct = (1 + st.Mods.HealingPct/100) * st.Mods.pct(OpDamage, sp)
	}
	s.CritPct = st.Crit[sp.School] + st.Mods.flat(OpCrit, sp)
	if s.CritPct < 0 {
		s.CritPct = 0
	}
	s.HitChance = 1.0
	if !sp.IsHeal {
		s.HitChance = hitChance(sp, st, tgt)
	}

	if sp.Direct != nil {
		s.DirectAvg = (sp.Direct.avgBase(st.Level, sp.Levels) + st.Mods.flat(OpDamage, sp) + sp.Direct.Coeff*power) * dmgPct
		s.DirectVariance = sp.Direct.Variance
	}
	if sp.Periodic != nil && sp.DurationMs > 0 {
		durMs := float64(sp.DurationMs) + st.Mods.flat(OpDuration, sp)
		durMs *= st.Mods.pct(OpDuration, sp)
		s.DurationSec = durMs / 1000
		s.PeriodSec = float64(sp.Periodic.PeriodMs) / 1000
		if s.PeriodSec > 0 {
			s.Ticks = s.DurationSec / s.PeriodSec
		}
		s.TickValue = (sp.Periodic.avgBase(st.Level, sp.Levels) + sp.Periodic.Coeff*power) * dmgPct
		// DoTs crit only when a talent says so; the crit is weighed into the
		// tick value (per-tick rolls would barely move the variance).
		if !sp.IsHeal && st.Mods.dotMayCrit(sp.School) {
			s.TickValue *= 1 + s.CritPct/100*s.CritBonus
		}
	}
	return s
}

// SampleDirect rolls one direct hit of the shape: damage (or healing) after
// the hit, variance and crit rolls. With rng == nil it is the expectation.
func (s *CastShape) SampleDirect(rng *rand.Rand) float64 {
	if s.DirectAvg == 0 {
		return 0
	}
	val := s.DirectAvg
	if rng != nil {
		if rng.Float64() >= s.HitChance {
			return 0
		}
		if s.DirectVariance > 0 {
			span := val * s.DirectVariance
			val = val - span/2 + rng.Float64()*span
		}
		if rng.Float64() < s.CritPct/100 {
			val *= 1 + s.CritBonus
		}
		return val
	}
	return val * (1 + s.CritPct/100*s.CritBonus) * s.HitChance
}

// ResolveCast evaluates one cast of a spell against the target.
// Heals use the healing bonus and never miss; damage rolls the spell hit
// table. Partial resists are not modeled in Stage 1 (noted in the docs).
func ResolveCast(sp *Spell, st *State, tgt Target, rng *rand.Rand) CastResult {
	s := Shape(sp, st, tgt)
	r := CastResult{ManaCost: s.ManaCost, ExecuteSec: s.ExecuteSec, DurationSec: s.DurationSec}

	direct := s.SampleDirect(rng)

	// Periodic portion: binary application (the whole DoT/HoT misses or
	// lands), value per tick.
	periodic := 0.0
	if s.Ticks > 0 {
		land := s.HitChance
		if rng != nil {
			if rng.Float64() < s.HitChance {
				land = 1
			} else {
				land = 0
			}
		}
		periodic = s.Ticks * s.TickValue * land
	}

	if s.IsHeal {
		r.Healing = direct + periodic
	} else {
		r.Damage = direct + periodic
	}
	return r
}
