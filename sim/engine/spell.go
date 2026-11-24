package engine

// Spell is one castable spell as the catalog decodes it from the client's
// DB2 tables. Base points scale with the caster level through PerLevel
// between the spell's base and max level, and spell power is added through
// the per-rank coefficient the client ships (Forever bakes the cast-time
// coefficient into EffectBonusCoefficient, verified on Shadow Bolt rank 1).
type Spell struct {
	ID         int
	BaseID     int // first rank of the chain (equals ID for unranked spells)
	Name       string
	School     School
	SchoolMask uint32 // bitmask of schools (multi-school spells take the best school's SP/crit)
	CastMs     int
	GCDMs      int // 0 means the default 1500
	CooldownMs int
	DurationMs int
	Channeled  bool
	Passive    bool
	GivesMana  bool // returns mana (energize effects)
	Cost       Cost
	Levels     Levels
	ClassMask  [4]uint32
	Direct     *Effect // direct damage (or direct heal for heal spells)
	Periodic   *Effect // damage/heal over time, value per tick
	IsHeal     bool
	// Absorb is a damage shield (Power Word: Shield): the amount it absorbs.
	// The healer metrics count it like healing; it never crits.
	Absorb *Effect
	// Energize is mana the spell returns to the caster per tick over its
	// duration (Dark Sacrifice).
	Energize *Effect
	// Area: the spell hits every enemy of the fight (Rain of Fire, Hellfire).
	Area bool
	// SelfTick is the damage a channel deals to its own caster per tick
	// (Hellfire): spell power raises it like the damage on the enemies.
	SelfTick *Effect
	// Physical melee/ranged combat properties decoded from DB2 or class Prepare.
	IsPhysical bool    // uses melee/ranged hit, crit (2x), and target armor mitigation
	IsRanged   bool    // uses ranged weapon, RAP, ranged hit/crit (Hunter shots)
	WeaponPct  float64 // weapon damage percentage (100 = 100% weapon dmg; 0 = none)
	Normalized bool    // AP weapon bonus uses normalized weapon speed (Effect 121)
	APCoeff    float64 // direct attack power scaling coefficient (e.g. Bloodthirst 0.45)
	OnSwing    bool    // replaces next white swing instead of spending GCD (Heroic Strike, Cleave)
}

// Heals reports whether the spell counts for the healer metrics: a heal or
// a damage shield.
func (sp *Spell) Heals() bool { return sp.IsHeal || sp.Absorb != nil }

// Cost is what one cast takes.
type Cost struct {
	Mana        float64
	PerLevel    float64
	Rage        float64
	Energy      float64
	ComboPoints bool
}

// Levels are the spell's level bounds from SpellLevels.
type Levels struct {
	Base  int // level the base points apply at; PerLevel scales above it
	Max   int // level the scaling stops at (0 = none)
	Spell int
}

// Effect is one decoded damage or heal effect.
type Effect struct {
	BP       float64 // base points (per tick for periodic effects)
	PerLevel float64
	Variance float64 // range is BP*(1 +/- Variance/2); expectations use the midpoint
	Coeff    float64 // spell power coefficient (per tick for periodic effects)
	PeriodMs int     // periodic effects only
	Index    int     // EffectIndex (talents may change one effect's points)
	Leech    float64 // share of the damage the caster gains as health (0 = none)
	// Crits: every tick is a spell hit of its own (the pulses of Rain of
	// Fire and Hellfire are direct damage spells), so it crits like one,
	// whatever the talents say about DoTs.
	Crits bool
}

// avgBase is the level-scaled midpoint of an effect's base value.
func (e *Effect) avgBase(level int, lv Levels) float64 {
	l := level
	if lv.Max > 0 && l > lv.Max {
		l = lv.Max
	}
	scale := float64(l - lv.Base)
	if scale < 0 {
		scale = 0
	}
	return e.BP + e.PerLevel*scale
}

// Ticks returns how many ticks the periodic effect has.
func (sp *Spell) Ticks() float64 {
	if sp.Periodic == nil || sp.Periodic.PeriodMs <= 0 || sp.DurationMs <= 0 {
		return 0
	}
	return float64(sp.DurationMs) / float64(sp.Periodic.PeriodMs)
}
