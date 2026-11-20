package engine

import (
	"math"
	"math/rand"
)

// CastResult is what one cast of a spell is worth. With rng == nil every
// field is an expectation; with an rng it is one sampled outcome.
type CastResult struct {
	Damage      float64
	Healing     float64
	ManaCost    float64
	ExecuteSec  float64 // time the cast occupies (max of cast time and GCD)
	CooldownSec float64 // cooldown after modifiers
	DurationSec float64 // periodic effects: duration after modifiers
	Leech       float64 // health the caster gains from the damage (Drain Life, Siphon Life, Death Coil)
}

const defaultGCDMs = 1500

// hitChance returns the chance (0..1) that a spell lands.
func hitChance(sp *Spell, st *State, tgt Target) float64 {
	miss, ok := SpellMiss[tgt.LevelDelta]
	if !ok {
		miss = SpellMiss[3]
	}
	miss -= st.Hit
	miss -= st.Mods.flat(OpHit, sp)
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
	CastSec     float64 // cast time or channel length; 0 for an instant
	GCDSec      float64
	CooldownSec float64
	ManaCost    float64
	HitChance   float64 // 0..1; 1 for heals
	CritPct     float64 // chance in percent
	CritBonus   float64 // extra damage factor on a crit (0.5 for spells)
	IsHeal      bool
	// Direct portion (0 when absent): the modded average and the width of
	// its range as a share of that average.
	DirectAvg      float64
	DirectVariance float64
	// Periodic portion (Ticks 0 when absent): modded value per tick; DotCrit
	// says whether ticks crit (weighed in per tick, not rolled).
	TickValue   float64
	TickBase    float64 // the tick before the crit expectation: what a tooltip shows
	Ticks       float64
	PeriodSec   float64
	DurationSec float64
	// Share of the damage that returns as health (0 = none).
	DirectLeech float64
	TickLeech   float64
}

// Context is what a spell's numbers depend on beyond the character: the
// DoTs the rotation keeps on the target (base ids), which some talents
// count (Soul Siphon). nil means none.
type Context struct {
	Dots map[int]bool
}

// ContextOf reads the context from a rotation.
func ContextOf(actions []Action) *Context {
	ctx := &Context{Dots: map[int]bool{}}
	for _, a := range actions {
		if a.Kind == "dot" && a.Spell != nil {
			ctx.Dots[a.Spell.BaseID] = true
		}
	}
	return ctx
}

// RankBonusShare is the share of the healing bonus an outgrown heal rank
// still gets: 5 % less per caster level beyond twelve levels above the last
// level its base points grow at (SpellLevels.MaxLevel), down to none.
//
// Measured on a level-27 priest's tooltips with 124 healing: Lesser Heal
// rank 1 (max level 3) 0.39, rank 2 (max level 9) 0.71, Renew rank 1 (max
// level 13) 0.90, every rank with a max level of 15 or more in full. Most
// ranks stop growing five levels after they are learned, which gives the
// rule as players state it: 1 - (level - 17 - learned at) * 0.05. Damage
// spells keep their full bonus whatever the rank (Corruption rank 1 at
// level 51, Smite rank 1 at 27), and so does the shield. A spell without a
// max level (talent spells) has no ranks to outgrow.
func RankBonusShare(level int, lv Levels) float64 {
	if lv.Max <= 0 {
		return 1
	}
	share := 1 - float64(level-lv.Max-rankGraceLevels)*rankLossPerLevel
	return math.Max(0, math.Min(1, share))
}

const (
	rankGraceLevels  = 12
	rankLossPerLevel = 0.05
)

// Shape computes a spell's cast shape for this state and target, outside
// any rotation.
func Shape(sp *Spell, st *State, tgt Target) CastShape {
	return ShapeIn(sp, st, tgt, nil)
}

// CooldownSec is a spell's cooldown after the talents (Soul Warding).
func CooldownSec(sp *Spell, st *State) float64 {
	ms := float64(sp.CooldownMs) + st.Mods.flat(OpCooldown, sp)
	ms *= st.Mods.pct(OpCooldown, sp)
	if ms < 0 {
		ms = 0
	}
	return ms / 1000
}

// ShapeIn computes a spell's cast shape within a rotation.
func ShapeIn(sp *Spell, st *State, tgt Target, ctx *Context) CastShape {
	s := CastShape{IsHeal: sp.IsHeal, CritBonus: SpellCritBonus * st.Mods.pct(OpCritDamage, sp)}

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
	s.CastSec = castSec
	if sp.Channeled && sp.DurationMs > 0 { // a channel occupies its full duration (not haste-scaled in Classic)
		s.CastSec = float64(sp.DurationMs) / 1000
		if s.CastSec > s.ExecuteSec {
			s.ExecuteSec = s.CastSec
		}
	}
	s.CooldownSec = CooldownSec(sp, st)

	// Cost.
	cost := sp.Cost.Mana + st.Mods.flat(OpCost, sp)
	cost *= st.Mods.pct(OpCost, sp) * (1 + st.Mods.SchoolCostPct[sp.School]/100)
	if cost < 0 {
		cost = 0
	}
	s.ManaCost = cost

	// Power and modifiers. The client keeps direct and periodic amounts
	// apart: SpellModOp 0 changes the direct part, op 22 the periodic one
	// (Shadow Mastery carries both, each with its own spell mask).
	power := st.SP[sp.School]
	schoolPct := 1 + st.Mods.SchoolDamagePct[sp.School]/100
	if sp.IsHeal {
		power = st.Healing * RankBonusShare(st.Level, sp.Levels)
		schoolPct = 1 + st.Mods.HealingPct/100
	} else if ctx != nil {
		schoolPct *= st.Mods.dotStackPct(sp, ctx.Dots)
	}
	directPct := schoolPct * st.Mods.pct(OpDamage, sp)
	tickPct := schoolPct * st.Mods.pct(OpPeriodic, sp)
	s.CritPct = st.Crit[sp.School] + st.Mods.flat(OpCrit, sp)
	if s.CritPct < 0 {
		s.CritPct = 0
	}
	s.HitChance = 1.0
	if !sp.IsHeal {
		s.HitChance = hitChance(sp, st, tgt)
	}

	if sp.Direct != nil {
		base := st.Mods.points(sp.Direct.avgBase(st.Level, sp.Levels), sp.Direct, sp)
		s.DirectAvg = (base + st.Mods.flat(OpDamage, sp) + sp.Direct.Coeff*power) * directPct
		// The range comes from the base points alone: the bonus from spell
		// power is the same on every cast (the tooltips show it: Shadow Bolt
		// rank 7 spans 18 points at 148 base and 0.114 variance).
		if s.DirectAvg > 0 {
			s.DirectVariance = sp.Direct.Variance * base * directPct / s.DirectAvg
		}
		s.DirectLeech = sp.Direct.Leech
	}
	if sp.Absorb != nil && sp.Direct == nil {
		// A shield: its amount plus the healing bonus; talents change the
		// base points (Improved Power Word: Shield). It cannot crit or miss,
		// and "healing done" percentages do not apply to it.
		base := st.Mods.points(sp.Absorb.avgBase(st.Level, sp.Levels), sp.Absorb, sp)
		s.DirectAvg = base + sp.Absorb.Coeff*st.Healing
		s.IsHeal, s.HitChance, s.CritPct = true, 1, 0
	}
	if sp.Periodic != nil && sp.DurationMs > 0 {
		durMs := float64(sp.DurationMs) + st.Mods.flat(OpDuration, sp)
		durMs *= st.Mods.pct(OpDuration, sp)
		s.DurationSec = durMs / 1000
		s.PeriodSec = float64(sp.Periodic.PeriodMs) / 1000
		if s.PeriodSec > 0 {
			s.Ticks = s.DurationSec / s.PeriodSec
		}
		base := st.Mods.points(sp.Periodic.avgBase(st.Level, sp.Levels), sp.Periodic, sp)
		s.TickValue = (base + sp.Periodic.Coeff*power) * tickPct
		s.TickBase = s.TickValue
		s.TickLeech = sp.Periodic.Leech
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

// SampleLanded rolls the direct part of a cast that already hit: the damage
// range and the crit. With rng == nil it is the expectation of a hit.
func (s *CastShape) SampleLanded(rng *rand.Rand) float64 {
	if s.DirectAvg == 0 {
		return 0
	}
	val := s.DirectAvg
	if rng == nil {
		return val * (1 + s.CritPct/100*s.CritBonus)
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

// ResolveCast evaluates one cast of a spell against the target, outside any
// rotation.
func ResolveCast(sp *Spell, st *State, tgt Target, rng *rand.Rand) CastResult {
	return ResolveCastIn(sp, st, tgt, nil, rng)
}

// ResolveCastIn evaluates one cast of a spell within a rotation.
// Heals use the healing bonus and never miss; damage rolls the spell hit
// table. Partial resists are not modeled in Stage 1 (noted in the docs).
func ResolveCastIn(sp *Spell, st *State, tgt Target, ctx *Context, rng *rand.Rand) CastResult {
	s := ShapeIn(sp, st, tgt, ctx)
	r := CastResult{ManaCost: s.ManaCost, ExecuteSec: s.ExecuteSec, CooldownSec: s.CooldownSec, DurationSec: s.DurationSec}

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
		r.Leech = direct*s.DirectLeech + periodic*s.TickLeech
	}
	return r
}
