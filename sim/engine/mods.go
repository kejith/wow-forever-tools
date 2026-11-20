package engine

// SpellModOp is the client's SpellModOp for aura 107/108 talent modifiers
// (the subset the engine understands; others are ignored).
type SpellModOp int

const (
	OpDamage   SpellModOp = 0
	OpDuration SpellModOp = 1
	OpCrit     SpellModOp = 7
	OpCastTime SpellModOp = 10
	OpCooldown SpellModOp = 11
	OpCost     SpellModOp = 14
)

// SpellMod is one decoded talent modifier: flat (aura 107) or percent
// (aura 108), applied to the spells whose class mask intersects Mask.
type SpellMod struct {
	Op    SpellModOp
	Pct   bool
	Value float64
	Mask  [4]uint32
}

func maskMatch(spell, mod [4]uint32) bool {
	for i := range spell {
		if spell[i]&mod[i] != 0 {
			return true
		}
	}
	return false
}

// Mods are the talent effects that the character sheet does NOT already
// include. Sheet-visible auras (flat stats, flat bonus damage, spell crit)
// must not land here, or they would count twice: the snapshot has them.
type Mods struct {
	SchoolDamagePct [NumSchools]float64 // aura 79: percent damage done per school
	SpellMods       []SpellMod          // aura 107/108
	HitFlatByBase   map[int]float64     // base spell id -> extra hit percent (e.g. Suppression)
	DotCritSchools  uint32              // school mask whose DoTs may crit (e.g. Pandemic)
	LifeTapPct      float64             // Improved Life Tap: percent more mana per tap
	HealingPct      float64             // percent more healing done
	CastingRegenPct float64             // Meditation: percent of spirit regeneration that continues while casting
	WandPct         float64             // Wand Specialization: percent more wand damage
	SPPerSpirit     float64             // Spiritual Guidance: spell power and healing per point of spirit
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

// dotMayCrit reports whether periodic damage of a school crits.
func (m *Mods) dotMayCrit(school School) bool {
	return m.DotCritSchools&(1<<uint(school)) != 0
}
