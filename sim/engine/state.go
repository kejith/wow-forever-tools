// Package engine evaluates a character's rotation against a target and
// returns role metrics (Stage 1: deterministic expectations; resolve_cast
// takes an optional rng as the seam for the later Monte-Carlo stage).
//
// Numbers that are Forever server-side and unverified carry their source in
// sim/data/*.json or a comment; the hit table below comes from Forever's own
// Camelot/PaperDollFrameStats.lua (via scripts/upgrades.py).
package engine

import "math"

// School indexes the seven damage schools (mask bit = 1 << School).
type School int

const (
	Physical School = iota
	Holy
	Fire
	Nature
	Frost
	Shadow
	Arcane
	NumSchools
)

var schoolNames = [NumSchools]string{"physical", "holy", "fire", "nature", "frost", "shadow", "arcane"}

// SchoolByName maps the canonical lowercase name to the index (-1 if unknown).
func SchoolByName(name string) School {
	for i, n := range schoolNames {
		if n == name {
			return School(i)
		}
	}
	return -1
}

// Name returns the canonical lowercase school name.
func (s School) Name() string {
	if s < 0 || s >= NumSchools {
		return "unknown"
	}
	return schoolNames[s]
}

// Spell miss chance in percent against a target 0..3 levels above the
// character. Source: Forever's Camelot/PaperDollFrameStats.lua (same
// constants as scripts/upgrades.py). No miss remains once the hit covers
// it (Classic keeps 1 %): a combat log of 2026-11-20 has no miss in 557
// casts and 645 area pulses on enemies up to the character's level, with
// 5 % hit.
var SpellMiss = map[int]float64{0: 4, 1: 5, 2: 6, 3: 17}

const MinMiss = 0.0

// Crit bonus damage: spells +50%, melee/ranged +100% (Classic rules).
const (
	SpellCritBonus = 0.5
	WhiteCritBonus = 1.0
)

// Wand is the equipped wand as the snapshot reports it.
type Wand struct {
	DPS    float64 `json:"dps"`
	Speed  float64 `json:"speed"`
	School string  `json:"school"`
	Skill  float64 `json:"skill"` // weapon skill (Wands)
}

// LifeTap is the character's known Life Tap rank, numbers parsed from the
// spell text by the Python side: the amounts at the sheet's spirit. Spell
// power does not change them (combat log of 2026-11-20: the same mana per
// cast at 209 and at 231 spell power).
type LifeTap struct {
	HealthPerCast float64 `json:"healthPerCast"`
	ManaPerCast   float64 `json:"manaPerCast"`
}

// lifeTapPerSpirit is how much one point of spirit adds to the health a
// Life Tap takes and the mana it gives. Source: the spell's description in
// the client's Spell table, "Converts ($m1+$SPI*1) Health into ... Mana".
// The combat log of 2026-11-20 agrees: with Improved Life Tap 2/2 a cast
// gave 510 mana at 120 spirit, 538 with 23 more and 552 with 35 more.
const lifeTapPerSpirit = 1.0

// Weapon represents an equipped melee or ranged weapon.
type Weapon struct {
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Speed     float64 `json:"speed"`
	NormSpeed float64 `json:"normSpeed,omitempty"` // 1.7 dagger, 2.4 1H, 3.3 2H, 2.8 ranged
	DPS       float64 `json:"dps"`
	Skill     float64 `json:"skill"`
}

// Avg returns the weapon's average base hit damage.
func (w Weapon) Avg() float64 {
	if w.Min > 0 || w.Max > 0 {
		return (w.Min + w.Max) / 2
	}
	if w.DPS > 0 && w.Speed > 0 {
		return w.DPS * w.Speed
	}
	return 0
}

// State is the character at the moment of evaluation: snapshot sheet totals
// (gear and buffs included) plus the talent modifiers that the sheet does
// not show. Copy it with ApplyDelta to rate an item swap.
type State struct {
	ClassFile  string
	Spec       string
	Level      int
	SP         [NumSchools]float64 // bonus spell damage per school (sheet totals)
	Healing    float64
	Crit       [NumSchools]float64 // spell crit percent per school
	Hit        float64             // spell hit percent from gear/talents on the sheet
	Strength   float64
	Agility    float64
	Intellect  float64
	Spirit     float64
	AttackPower       float64
	RangedAttackPower float64
	FeralAP           float64
	MeleeCrit         float64
	MeleeHit          float64
	MeleeHastePct     float64
	RangedHastePct    float64
	CritPerAgi        float64
	APPerStr          float64
	APPerAgi          float64
	RAPPerAgi         float64
	MP5        float64 // mana per 5 s that works while casting (gear)
	HP5        float64
	HealthMax  float64
	ManaMax    float64
	CritPerInt float64 // spell crit percent per point of intellect
	ManaPerInt float64
	HPPerSta   float64
	HastePct   float64
	RangedCrit float64
	// Mana regeneration while not casting, per second: base + spirit * factor.
	// The per-class constants are Classic approximations (see run.buildState);
	// talents decide how much of it continues while casting (Mods.CastingRegenPct).
	RegenBasePS       float64
	RegenSpiritFactor float64
	Wand              Wand
	MainHand          Weapon
	OffHand           Weapon
	Ranged            Weapon
	PetDPS            float64
	LifeTap           LifeTap
	Mods              Mods
}

// RegenPS is the full mana regeneration per second outside the five-second rule.
func (st *State) RegenPS() float64 {
	return st.RegenBasePS + st.Spirit*st.RegenSpiritFactor
}

// Regen is the mana regeneration per second over a fight: while casting
// (gear mp5 plus the share of the spirit regeneration the talents let
// through) and while idle (outside the five-second rule). Buffs from kills
// (Spirit Tap) count for the share of the fight they are up.
func (st *State) Regen(fight Fight) (casting, idle float64) {
	full := st.RegenPS()
	share := math.Min(100, st.Mods.CastingRegenPct) / 100
	casting, idle = full*share, full
	for _, p := range st.Mods.KillProcs {
		up := p.uptime(fight)
		if up <= 0 {
			continue
		}
		boosted := st.RegenBasePS + st.Spirit*(1+p.SpiritPct/100)*st.RegenSpiritFactor
		boostedShare := math.Min(100, st.Mods.CastingRegenPct+p.CastingRegenPct) / 100
		casting += up * (boosted*boostedShare - full*share)
		idle += up * (boosted - full)
	}
	return st.MP5/5 + casting, st.MP5/5 + idle
}

// KillProcUptime is the share of the fight (0..1) the character's kill buff
// is up; 0 without such a talent or without kills.
func (st *State) KillProcUptime(fight Fight) float64 {
	up := 0.0
	for _, p := range st.Mods.KillProcs {
		up = math.Max(up, p.uptime(fight))
	}
	return up
}

// Target is the enemy the rotation runs against.
type Target struct {
	LevelDelta int // levels above the character (0..3)
	Level      int // absolute level (character level + delta)
	Defense    float64
}

// NewTarget builds the target for a character level and delta. Defense of a
// normal mob is five times its level (Classic rule, unverified for Forever).
func NewTarget(charLevel, delta int) Target {
	lvl := charLevel + delta
	return Target{LevelDelta: delta, Level: lvl, Defense: float64(lvl * 5)}
}

// Fight is the encounter the metric is computed for.
type Fight struct {
	Seconds     float64 `json:"seconds"`
	TargetDelta int     `json:"targetDelta"`
	// KillsPerMinute is how often the character lands a killing blow, which
	// is what buffs like Spirit Tap need. Negative means automatic (run.go:
	// one kill per fight for a damage dealer, none for a healer).
	KillsPerMinute float64 `json:"killsPerMinute"`
	// Targets is how many enemies the fight has at once (0 or 1: one). A
	// damage rotation keeps its DoTs and debuffs on each of them, casts its
	// other spells at one, and its area spells hit them all. All of them
	// live for the whole fight; the metric is the damage on all together.
	Targets int `json:"targets,omitempty"`
}

// MaxTargets caps the enemies of a fight.
const MaxTargets = 10

// Enemies is the number of enemies the fight has at once.
func (f Fight) Enemies() int {
	switch {
	case f.Targets < 1:
		return 1
	case f.Targets > MaxTargets:
		return MaxTargets
	}
	return f.Targets
}

// magicSchools are all schools that spell power and spell crit apply to.
var magicSchools = []School{Holy, Fire, Nature, Frost, Shadow, Arcane}

// ApplyDelta returns a copy of the state with canonical stat deltas applied,
// including the derived chains (intellect to mana and crit, stamina to
// health). Unknown keys are ignored: stats the metric cannot see (armor,
// resistances) simply do not move it.
func (st *State) ApplyDelta(delta map[string]float64) *State {
	s := *st
	for key, v := range delta {
		if v == 0 {
			continue
		}
		switch key {
		case "sp": // "damage and healing by all spells"
			for _, sc := range magicSchools {
				s.SP[sc] += v
			}
			s.Healing += v
		case "spDamage": // "damage by all spells", no healing
			for _, sc := range magicSchools {
				s.SP[sc] += v
			}
		case "spHoly":
			s.SP[Holy] += v
		case "spFire":
			s.SP[Fire] += v
		case "spNature":
			s.SP[Nature] += v
		case "spFrost":
			s.SP[Frost] += v
		case "spShadow":
			s.SP[Shadow] += v
		case "spArcane":
			s.SP[Arcane] += v
		case "healing":
			s.Healing += v
		case "hit":
			s.Hit += v
			s.MeleeHit += v
		case "spellHit":
			s.Hit += v
		case "meleeHit":
			s.MeleeHit += v
		case "rangedHit":
			if s.ClassFile == "HUNTER" {
				s.MeleeHit += v
			}
		case "crit":
			for _, sc := range magicSchools {
				s.Crit[sc] += v
			}
			s.MeleeCrit += v
			if s.ClassFile == "HUNTER" {
				s.RangedCrit += v
			}
		case "spellCrit":
			for _, sc := range magicSchools {
				s.Crit[sc] += v
			}
		case "spellCritHoly":
			s.Crit[Holy] += v
		case "spellCritFire":
			s.Crit[Fire] += v
		case "spellCritNature":
			s.Crit[Nature] += v
		case "spellCritFrost":
			s.Crit[Frost] += v
		case "spellCritShadow":
			s.Crit[Shadow] += v
		case "spellCritArcane":
			s.Crit[Arcane] += v
		case "str":
			s.Strength += v
			apPer := s.APPerStr
			if apPer == 0 {
				if s.ClassFile == "ROGUE" || s.ClassFile == "HUNTER" {
					apPer = 1
				} else {
					apPer = 2
				}
			}
			s.AttackPower += v * apPer
		case "agi":
			s.Agility += v
			s.AttackPower += v * s.APPerAgi
			s.RangedAttackPower += v * s.RAPPerAgi
			cpa := s.CritPerAgi
			if cpa == 0 {
				switch s.ClassFile {
				case "ROGUE":
					cpa = 1.0 / 29.0
				case "HUNTER":
					cpa = 1.0 / 53.0
				default:
					cpa = 1.0 / 20.0
				}
			}
			s.MeleeCrit += v * cpa
			if s.ClassFile == "HUNTER" {
				s.RangedCrit += v * cpa
			}
		case "ap":
			s.AttackPower += v
			s.RangedAttackPower += v
		case "meleeAp":
			s.AttackPower += v
		case "rap":
			s.RangedAttackPower += v
		case "feralAp":
			s.FeralAP += v
		case "meleeHaste":
			s.MeleeHastePct += v
			s.RangedHastePct += v
		case "meleeCrit":
			s.MeleeCrit += v
			if s.ClassFile == "HUNTER" {
				s.RangedCrit += v
			}
		case "rangedCrit":
			s.RangedCrit += v
		case "weaponDps":
			if s.MainHand.Speed > 0 {
				s.MainHand.DPS += v
				deltaHit := v * s.MainHand.Speed
				s.MainHand.Min += deltaHit
				s.MainHand.Max += deltaHit
			}
		case "weaponDmg":
			if s.MainHand.Speed > 0 {
				s.MainHand.Min += v
				s.MainHand.Max += v
				s.MainHand.DPS += v / s.MainHand.Speed
			}
		case "offhandDmg":
			if s.OffHand.Speed > 0 {
				s.OffHand.Min += v
				s.OffHand.Max += v
				s.OffHand.DPS += v / s.OffHand.Speed
			}
		case "rangedDps":
			if s.Ranged.Speed > 0 {
				s.Ranged.DPS += v
				deltaHit := v * s.Ranged.Speed
				s.Ranged.Min += deltaHit
				s.Ranged.Max += deltaHit
			}
		case "rangedDmg":
			if s.Ranged.Speed > 0 {
				s.Ranged.Min += v
				s.Ranged.Max += v
				s.Ranged.DPS += v / s.Ranged.Speed
			}
		case "health":
			s.HealthMax += v
		case "mana":
			s.ManaMax += v
		case "int":
			v *= 1 + s.Mods.IntellectPct/100
			s.Intellect += v
			s.ManaMax += v * s.ManaPerInt
			for _, sc := range magicSchools {
				s.Crit[sc] += v * s.CritPerInt
			}
		case "sta":
			s.HealthMax += v * (1 + s.Mods.StaminaPct/100) * s.HPPerSta
		case "spi":
			v *= 1 + s.Mods.SpiritPct/100
			s.Spirit += v
			if p := s.Mods.SPPerSpirit; p > 0 { // Spiritual Guidance: spirit feeds spell damage
				for _, sc := range magicSchools {
					s.SP[sc] += v * p
				}
			}
			s.Healing += v * s.Mods.HealPerSpirit
			if s.LifeTap.ManaPerCast > 0 { // spirit raises what a Life Tap converts
				s.LifeTap.HealthPerCast += v * lifeTapPerSpirit
				s.LifeTap.ManaPerCast += v * lifeTapPerSpirit
			}
		case "mp5":
			s.MP5 += v
		case "hp5":
			s.HP5 += v
		case "wandDps":
			s.Wand.DPS += v
		case "haste":
			s.HastePct += v
		}
	}
	return &s
}
