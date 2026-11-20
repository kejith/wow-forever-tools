// Package engine evaluates a character's rotation against a target and
// returns role metrics (Stage 1: deterministic expectations; resolve_cast
// takes an optional rng as the seam for the later Monte-Carlo stage).
//
// Numbers that are Forever server-side and unverified carry their source in
// sim/data/*.json or a comment; the hit table below comes from Forever's own
// Camelot/PaperDollFrameStats.lua (via scripts/upgrades.py).
package engine

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
// character, and the floor that always remains. Source: Forever's
// Camelot/PaperDollFrameStats.lua (same constants as scripts/upgrades.py).
var SpellMiss = map[int]float64{0: 4, 1: 5, 2: 6, 3: 17}

const MinMiss = 1.0

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
// spell text by the Python side (base values, no spell-power scaling: the
// Forever scaling is unmeasured, see sim/data/overrides.json).
type LifeTap struct {
	HealthPerCast float64 `json:"healthPerCast"`
	ManaPerCast   float64 `json:"manaPerCast"`
}

// State is the character at the moment of evaluation: snapshot sheet totals
// (gear and buffs included) plus the talent modifiers that the sheet does
// not show. Copy it with ApplyDelta to rate an item swap.
type State struct {
	Level      int
	SP         [NumSchools]float64 // bonus spell damage per school (sheet totals)
	Healing    float64
	Crit       [NumSchools]float64 // spell crit percent per school
	Hit        float64             // spell hit percent from gear/talents on the sheet
	Intellect  float64
	Spirit     float64
	MP5        float64 // mana per 5 s that works while casting (gear)
	HP5        float64
	HealthMax  float64
	ManaMax    float64
	CritPerInt float64 // spell crit percent per point of intellect
	ManaPerInt float64
	HPPerSta   float64
	HastePct   float64
	RangedCrit float64
	Wand       Wand
	LifeTap    LifeTap
	Mods       Mods
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
		case "crit":
			for _, sc := range magicSchools {
				s.Crit[sc] += v
			}
		case "int":
			s.Intellect += v
			s.ManaMax += v * s.ManaPerInt
			for _, sc := range magicSchools {
				s.Crit[sc] += v * s.CritPerInt
			}
		case "sta":
			s.HealthMax += v * s.HPPerSta
		case "spi":
			s.Spirit += v
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
