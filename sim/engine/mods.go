package engine

import "math"

// SpellModOp is the client's SpellModOp for aura 107/108 talent modifiers.
// The listed ones are those the engine applies (Understood); talents with
// another op show up in the coverage report instead of being dropped
// silently.
type SpellModOp int

const (
	OpDamage     SpellModOp = 0  // direct damage and healing
	OpDuration   SpellModOp = 1  // duration of the periodic effect
	OpPoints0    SpellModOp = 3  // base points of effect 0
	OpRange      SpellModOp = 5  // no influence on the metrics
	OpCrit       SpellModOp = 7  // crit chance
	OpAllPoints  SpellModOp = 8  // base points of every effect
	OpPushback   SpellModOp = 9  // no influence: the model takes no damage
	OpCastTime   SpellModOp = 10 // cast time
	OpCooldown   SpellModOp = 11 // cooldown
	OpPoints1    SpellModOp = 12 // base points of effect 1
	OpCost       SpellModOp = 14 // mana cost
	OpCritDamage SpellModOp = 15 // crit damage bonus
	OpHit        SpellModOp = 16 // hit chance
	OpPeriodic   SpellModOp = 22 // periodic damage and healing
	OpPoints2    SpellModOp = 23 // base points of effect 2
)

// Understood reports whether the engine applies a spell modifier op.
func Understood(op SpellModOp) bool {
	switch op {
	case OpDamage, OpDuration, OpPoints0, OpCrit, OpAllPoints, OpCastTime, OpCooldown,
		OpPoints1, OpCost, OpCritDamage, OpHit, OpPeriodic, OpPoints2:
		return true
	}
	return false
}

// pointsOp is the op that changes the base points of one effect index.
func pointsOp(index int) SpellModOp {
	switch index {
	case 0:
		return OpPoints0
	case 1:
		return OpPoints1
	case 2:
		return OpPoints2
	}
	return -1
}

// SpellMod is one decoded talent modifier: flat (aura 107) or percent
// (aura 108), applied to the spells whose class mask intersects Mask.
type SpellMod struct {
	Op    SpellModOp
	Pct   bool
	Value float64
	Mask  [4]uint32
}

// Matches reports whether the modifier applies to a spell.
func (m SpellMod) Matches(sp *Spell) bool { return maskMatch(sp.ClassMask, m.Mask) }

func maskMatch(spell, mod [4]uint32) bool {
	for i := range spell {
		if spell[i]&mod[i] != 0 {
			return true
		}
	}
	return false
}

// DotStack raises the damage (and the health drained) of some spells for
// every one of the caster's DoTs on the target: Soul Siphon, Power in Light.
// The model takes the DoTs the rotation maintains as active.
type DotStack struct {
	PctPerDot float64
	Max       int          // most DoTs that count
	Affects   map[int]bool // base ids of the spells that gain
	Counts    map[int]bool // base ids of the DoTs that count
}

// ChannelHaste makes the caster's DoTs tick faster while a spell is
// channeled (Wrack). The DoT keeps its duration (confirmed in the game), so
// the faster ticks are extra damage; see the note in sim/data/overrides.json.
type ChannelHaste struct {
	Channel int          // base id of the channeled spell
	Pct     float64      // how much faster the DoTs tick
	Affects map[int]bool // base ids of the DoTs
}

// KillProc is a buff the caster gains with a chance when they kill a target
// (Spirit Tap): more spirit, and more of the mana regeneration continues
// while casting. How often it is up depends on the kills the fight assumes
// (Fight.KillsPerMinute).
type KillProc struct {
	ChancePct       float64
	DurationSec     float64
	SpiritPct       float64 // percent more spirit while it is up
	CastingRegenPct float64 // percent of the regeneration that continues while casting, on top of the talents
}

// uptime is the share of the fight the buff is up: with n kills falling into
// one buff duration, the chance that at least one of them triggered it.
func (p KillProc) uptime(fight Fight) float64 {
	n := p.DurationSec * fight.KillsPerMinute / 60
	chance := p.ChancePct / 100
	if n <= 0 || chance <= 0 {
		return 0
	}
	if chance > 1 {
		chance = 1
	}
	if n < 1 {
		return chance * n
	}
	return 1 - math.Pow(1-chance, n)
}

// Mods are the talent effects that the character sheet does NOT already
// include. Sheet-visible auras (flat stats, flat bonus damage, spell crit,
// hit) must not land here, or they would count twice: the snapshot has them.
type Mods struct {
	SchoolDamagePct [NumSchools]float64 // aura 79: percent damage done per school
	SchoolCostPct   [NumSchools]float64 // aura 72: percent mana cost per school (negative = cheaper)
	SpellMods       []SpellMod          // aura 107/108
	DotStacks       []DotStack
	ChannelHastes   []ChannelHaste
	KillProcs       []KillProc
	DotCritSchools  uint32  // school mask whose DoTs may crit (e.g. Pandemic)
	LifeTapPct      float64 // Improved Life Tap: percent more mana per tap
	HealingPct      float64 // percent more healing done
	CastingRegenPct float64 // Meditation: percent of spirit regeneration that continues while casting
	WandPct         float64 // Wand Specialization: percent more wand damage
	// Spiritual Guidance: spell damage and healing per point of spirit. The
	// sheet has them for the spirit the character wears; the engine applies
	// them to spirit changes from items.
	SPPerSpirit   float64
	HealPerSpirit float64
	// Percent more of an attribute (aura 137, e.g. Mental Strength): in the
	// sheet for worn gear, applied to item changes.
	IntellectPct float64
	SpiritPct    float64
	StaminaPct   float64
}

// flat sums the flat modifiers of an op for a spell.
func (m *Mods) flat(op SpellModOp, sp *Spell) float64 {
	var v float64
	for _, mod := range m.SpellMods {
		if mod.Op == op && !mod.Pct && maskMatch(sp.ClassMask, mod.Mask) {
			v += mod.Value
		}
	}
	return v
}

// pct multiplies the percent modifiers of an op for a spell (1.0 = none).
func (m *Mods) pct(op SpellModOp, sp *Spell) float64 {
	v := 1.0
	for _, mod := range m.SpellMods {
		if mod.Op == op && mod.Pct && maskMatch(sp.ClassMask, mod.Mask) {
			v *= 1 + mod.Value/100
		}
	}
	return v
}

// points applies the base-point modifiers of one effect: the client changes
// the effect's base value, the spell power bonus comes on top afterwards.
func (m *Mods) points(base float64, e *Effect, sp *Spell) float64 {
	op := pointsOp(e.Index)
	base += m.flat(OpAllPoints, sp)
	base *= m.pct(OpAllPoints, sp)
	if op >= 0 {
		base += m.flat(op, sp)
		base *= m.pct(op, sp)
	}
	return base
}

// dotMayCrit reports whether periodic damage of a school crits.
func (m *Mods) dotMayCrit(school School) bool {
	return m.DotCritSchools&(1<<uint(school)) != 0
}

// dotStackPct is the damage factor a spell gains from the DoTs the rotation
// keeps up (1.0 = none). dots are the base ids of those DoTs.
func (m *Mods) dotStackPct(sp *Spell, dots map[int]bool) float64 {
	v := 1.0
	for _, ds := range m.DotStacks {
		if !ds.Affects[sp.BaseID] {
			continue
		}
		n := 0
		for base := range dots {
			if ds.Counts[base] {
				n++
			}
		}
		if ds.Max > 0 && n > ds.Max {
			n = ds.Max
		}
		v *= 1 + ds.PctPerDot*float64(n)/100
	}
	return v
}

// channelHaste is how much faster a DoT ticks while `channel` is channeled
// (0 = not at all).
func (m *Mods) channelHaste(channel, dot *Spell) float64 {
	if channel == nil || dot == nil {
		return 0
	}
	var v float64
	for _, ch := range m.ChannelHastes {
		if ch.Channel == channel.BaseID && ch.Affects[dot.BaseID] {
			v += ch.Pct
		}
	}
	return v
}
