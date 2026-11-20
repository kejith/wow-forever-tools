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
	CastMs     int
	GCDMs      int // 0 means the default 1500
	CooldownMs int
	DurationMs int
	Channeled  bool
	Cost       Cost
	Levels     Levels
	ClassMask  [4]uint32
	Direct     *Effect // direct damage (or direct heal for heal spells)
	Periodic   *Effect // damage/heal over time, value per tick
	IsHeal     bool
}

// Cost is what one cast takes.
type Cost struct {
	Mana     float64
	PerLevel float64
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
