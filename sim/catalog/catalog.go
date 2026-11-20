// Package catalog turns the client's DB2 tables into the engine's spell
// model for one class: castable spells with their decoded damage and heal
// effects, rank chains, the raw aura effects (for the talent decoder) and
// the assumed spellbook when the addon has not exported one.
package catalog

import (
	"fmt"

	"github.com/kejith/wow-forever-tools/sim/db2"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

// classSet maps the game's class id to SpellClassOptions.SpellClassSet
// (the Classic spell family numbers).
var classSet = map[int]int{
	1:  4,  // Warrior
	2:  10, // Paladin
	3:  9,  // Hunter
	4:  8,  // Rogue
	5:  6,  // Priest
	7:  11, // Shaman
	8:  3,  // Mage
	9:  5,  // Warlock
	11: 7,  // Druid
}

// ClassID maps the addon's classFile to the game's class id.
var ClassID = map[string]int{
	"WARRIOR": 1, "PALADIN": 2, "HUNTER": 3, "ROGUE": 4, "PRIEST": 5,
	"SHAMAN": 7, "MAGE": 8, "WARLOCK": 9, "DRUID": 11,
}

// Aura is one raw APPLY_AURA effect of a class spell, kept for the talent
// decoder (the engine's Spell only holds the decoded damage/heal parts).
type Aura struct {
	Aura  int
	BP    float64
	Misc0 int
	Misc1 int
	Mask  [4]uint32
}

// Catalog is the decoded game data for one class.
type Catalog struct {
	ClassID  int
	Spells   map[int]*engine.Spell
	Auras    map[int][]Aura
	NextRank map[int]int // spell id -> the rank that supersedes it
	BaseOf   map[int]int // spell id -> first rank of its chain
	// skillSpells are the class's trainable spells from SkillLineAbility,
	// with the level each becomes available (for the assumed spellbook).
	skillSpells map[int]int
}

// Effect numbers and aura numbers the catalog decodes (client enums).
const (
	effSchoolDamage = 2
	effApplyAura    = 6
	effHealthLeech  = 9
	effHeal         = 10
	effEnergize     = 30

	auraPeriodicDamage = 3
	auraPeriodicHeal   = 8
	auraPeriodicLeech  = 53
)

// Build loads the tables from the cache and decodes the class's spells.
func Build(cacheDir, build, locale, classFile string) (*Catalog, error) {
	classID, ok := ClassID[classFile]
	if !ok {
		return nil, fmt.Errorf("unknown class %q", classFile)
	}
	set := classSet[classID]

	load := func(name string) (*db2.Table, error) { return db2.Load(cacheDir, name, build, locale) }

	classOpt, err := load("SpellClassOptions")
	if err != nil {
		return nil, err
	}
	misc, err := load("SpellMisc")
	if err != nil {
		return nil, err
	}
	effects, err := load("SpellEffect")
	if err != nil {
		return nil, err
	}
	castTimes, err := load("SpellCastTimes")
	if err != nil {
		return nil, err
	}
	durations, err := load("SpellDuration")
	if err != nil {
		return nil, err
	}
	power, err := load("SpellPower")
	if err != nil {
		return nil, err
	}
	levels, err := load("SpellLevels")
	if err != nil {
		return nil, err
	}
	cooldowns, err := load("SpellCooldowns")
	if err != nil {
		return nil, err
	}
	sla, err := load("SkillLineAbility")
	if err != nil {
		return nil, err
	}
	names, nameErr := load("SpellName")
	_ = nameErr // names are cosmetic; a missing locale file leaves them empty

	// Index the lookup tables.
	castBase := map[int]int{}
	for i := 0; i < castTimes.Len(); i++ {
		r := castTimes.Row(i)
		castBase[r.Int("ID")] = r.Int("Base")
	}
	durBase := map[int]int{}
	for i := 0; i < durations.Len(); i++ {
		r := durations.Row(i)
		durBase[r.Int("ID")] = r.Int("Duration")
	}
	type pow struct{ mana, perLevel float64 }
	powerOf := map[int]pow{}
	for i := 0; i < power.Len(); i++ {
		r := power.Row(i)
		id := r.Int("SpellID")
		if _, seen := powerOf[id]; seen && r.Int("OrderIndex") != 0 {
			continue
		}
		powerOf[id] = pow{mana: r.Float("ManaCost"), perLevel: r.Float("ManaCostPerLevel")}
	}
	levelsOf := map[int]engine.Levels{}
	for i := 0; i < levels.Len(); i++ {
		r := levels.Row(i)
		if r.Int("DifficultyID") != 0 {
			continue
		}
		levelsOf[r.Int("SpellID")] = engine.Levels{
			Base: r.Int("BaseLevel"), Max: r.Int("MaxLevel"), Spell: r.Int("SpellLevel"),
		}
	}
	type cds struct{ recovery, gcd int }
	cdOf := map[int]cds{}
	for i := 0; i < cooldowns.Len(); i++ {
		r := cooldowns.Row(i)
		if r.Int("DifficultyID") != 0 {
			continue
		}
		rec := r.Int("RecoveryTime")
		if c := r.Int("CategoryRecoveryTime"); c > rec {
			rec = c
		}
		cdOf[r.Int("SpellID")] = cds{recovery: rec, gcd: r.Int("StartRecoveryTime")}
	}
	nameOf := map[int]string{}
	if names != nil {
		for i := 0; i < names.Len(); i++ {
			r := names.Row(i)
			nameOf[r.Int("ID")] = r.Str("Name_lang")
		}
	}

	// The class's spells by SpellClassOptions.
	cat := &Catalog{
		ClassID:     classID,
		Spells:      map[int]*engine.Spell{},
		Auras:       map[int][]Aura{},
		NextRank:    map[int]int{},
		BaseOf:      map[int]int{},
		skillSpells: map[int]int{},
	}
	for i := 0; i < classOpt.Len(); i++ {
		r := classOpt.Row(i)
		if r.Int("SpellClassSet") != set {
			continue
		}
		id := r.Int("SpellID")
		cat.Spells[id] = &engine.Spell{
			ID:     id,
			BaseID: id,
			Name:   nameOf[id],
			ClassMask: [4]uint32{
				r.Uint32("SpellClassMask_0"), r.Uint32("SpellClassMask_1"),
				r.Uint32("SpellClassMask_2"), r.Uint32("SpellClassMask_3"),
			},
		}
	}

	// Timing, school, cost and levels.
	for i := 0; i < misc.Len(); i++ {
		r := misc.Row(i)
		sp := cat.Spells[r.Int("SpellID")]
		if sp == nil || r.Int("DifficultyID") != 0 {
			continue
		}
		sp.CastMs = castBase[r.Int("CastingTimeIndex")]
		sp.DurationMs = durBase[r.Int("DurationIndex")]
		sp.School = schoolOf(r.Int("SchoolMask"))
	}
	for id, sp := range cat.Spells {
		if p, ok := powerOf[id]; ok {
			sp.Cost = engine.Cost{Mana: p.mana, PerLevel: p.perLevel}
		}
		if lv, ok := levelsOf[id]; ok {
			sp.Levels = lv
		}
		if cd, ok := cdOf[id]; ok {
			sp.CooldownMs = cd.recovery
			sp.GCDMs = cd.gcd
		}
	}

	// Effects: decoded damage/heal parts plus raw auras for the talents.
	for i := 0; i < effects.Len(); i++ {
		r := effects.Row(i)
		sp := cat.Spells[r.Int("SpellID")]
		if sp == nil || r.Int("DifficultyID") != 0 {
			continue
		}
		eff := &engine.Effect{
			BP:       r.Float("EffectBasePointsF"),
			PerLevel: r.Float("EffectRealPointsPerLevel"),
			Variance: r.Float("Variance"),
			Coeff:    r.Float("EffectBonusCoefficient"),
			PeriodMs: r.Int("EffectAuraPeriod"),
		}
		switch r.Int("Effect") {
		case effSchoolDamage, effHealthLeech:
			sp.Direct = eff
		case effHeal:
			sp.Direct = eff
			sp.IsHeal = true
		case effApplyAura:
			switch r.Int("EffectAura") {
			case auraPeriodicDamage, auraPeriodicLeech:
				sp.Periodic = eff
			case auraPeriodicHeal:
				sp.Periodic = eff
				sp.IsHeal = true
			default:
				cat.Auras[sp.ID] = append(cat.Auras[sp.ID], Aura{
					Aura:  r.Int("EffectAura"),
					BP:    eff.BP,
					Misc0: r.Int("EffectMiscValue_0"),
					Misc1: r.Int("EffectMiscValue_1"),
					Mask: [4]uint32{
						r.Uint32("EffectSpellClassMask_0"), r.Uint32("EffectSpellClassMask_1"),
						r.Uint32("EffectSpellClassMask_2"), r.Uint32("EffectSpellClassMask_3"),
					},
				})
			}
		}
	}

	// Rank chains and trainable spells from SkillLineAbility.
	for i := 0; i < sla.Len(); i++ {
		r := sla.Row(i)
		id := r.Int("Spell")
		sp := cat.Spells[id]
		if sp == nil {
			continue
		}
		if prev := r.Int("SupercedesSpell"); prev != 0 {
			cat.NextRank[prev] = id
		}
		lvl := 0
		if lv, ok := levelsOf[id]; ok {
			lvl = lv.Base
		}
		if old, seen := cat.skillSpells[id]; !seen || lvl < old {
			cat.skillSpells[id] = lvl
		}
	}
	// Resolve each chain to its first rank.
	prevOf := map[int]int{}
	for prev, next := range cat.NextRank {
		prevOf[next] = prev
	}
	for id := range cat.Spells {
		base := id
		for depth := 0; depth < 32; depth++ {
			prev, ok := prevOf[base]
			if !ok {
				break
			}
			base = prev
		}
		cat.BaseOf[id] = base
		cat.Spells[id].BaseID = base
	}
	return cat, nil
}

// schoolOf picks the school of a mask (the lowest set bit).
func schoolOf(mask int) engine.School {
	for s := engine.Physical; s < engine.NumSchools; s++ {
		if mask&(1<<uint(s)) != 0 {
			return s
		}
	}
	return engine.Physical
}

// AssumedSpellbook returns the "all trainer ranks known" fallback: every
// trainable class spell whose level requirement the character meets, plus
// the talent spells the character has points in (talent-granted casts like
// Siphon Life are their own talent spell).
func (c *Catalog) AssumedSpellbook(level int, talents map[int]int) map[int]bool {
	known := map[int]bool{}
	for id, reqLevel := range c.skillSpells {
		if reqLevel <= level {
			known[id] = true
		}
	}
	for id := range talents {
		if c.Spells[id] != nil {
			known[id] = true
		}
	}
	return known
}

// BestRank returns the highest known rank of a spell chain that the
// character's level allows, or nil.
func (c *Catalog) BestRank(baseID int, known map[int]bool, level int) *engine.Spell {
	var best *engine.Spell
	id := baseID
	for depth := 0; depth < 32 && id != 0; depth++ {
		sp := c.Spells[id]
		if sp != nil && known[id] && sp.Levels.Base <= level {
			best = sp
		}
		id = c.NextRank[id]
	}
	return best
}
