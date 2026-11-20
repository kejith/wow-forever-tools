// Package catalog turns the client's DB2 tables into the engine's spell
// model for one class: castable spells with their decoded damage and heal
// effects, rank chains, the raw aura effects (for the talent decoder) and
// the assumed spellbook when the addon has not exported one.
package catalog

import (
	"fmt"
	"sort"

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
	ClassID int
	Spells  map[int]*engine.Spell
	Auras   map[int][]Aura
	BaseOf  map[int]int   // spell id -> first rank of its chain
	Chains  map[int][]int // first rank -> every spell of the chain, lowest rank first
	// skillSpells are the class's spells from SkillLineAbility, with the
	// level each becomes available (for the assumed spellbook).
	skillSpells map[int]int
	// granted marks the skill spells nobody trains (AcquireMethod 3:
	// talent casts and alternative versions of a rank).
	granted map[int]bool
	rankOf  map[int]int // spell id -> rank number (1 = first rank)
}

// ReferenceLocale is the locale whose spell names group the rank chains
// (scripts/simengine.py downloads that SpellName table next to the client's).
const ReferenceLocale = "enUS"

// acquireGranted is SkillLineAbility.AcquireMethod for spells that come from
// a talent or another effect instead of the trainer.
const acquireGranted = 3

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
	_ = nameErr // a missing locale file leaves the labels empty
	// The rank chains are grouped by the English name where the cache has
	// it: a translation may name the ranks of one spell differently (the
	// German client calls Smite rank 1 "Heilige Pein" and the higher ranks
	// "Goettliche Pein").
	refNames := names
	if locale != ReferenceLocale {
		if t, err := db2.Load(cacheDir, "SpellName", build, ReferenceLocale); err == nil {
			refNames = t
		}
	}

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
	chainName := nameOf
	if refNames != nil && refNames != names {
		chainName = map[int]string{}
		for i := 0; i < refNames.Len(); i++ {
			r := refNames.Row(i)
			chainName[r.Int("ID")] = r.Str("Name_lang")
		}
	}

	// The class's spells by SpellClassOptions.
	cat := &Catalog{
		ClassID:     classID,
		Spells:      map[int]*engine.Spell{},
		Auras:       map[int][]Aura{},
		BaseOf:      map[int]int{},
		Chains:      map[int][]int{},
		skillSpells: map[int]int{},
		granted:     map[int]bool{},
		rankOf:      map[int]int{},
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
		sp.Channeled = r.Int("Attributes_1")&(0x4|0x40) != 0 // SPELL_ATTR1_IS_(SELF_)CHANNELLED
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

	// Skill spells and the explicit rank links from SkillLineAbility.
	supersedes := map[int]int{} // spell id -> the rank below it
	for i := 0; i < sla.Len(); i++ {
		r := sla.Row(i)
		id := r.Int("Spell")
		sp := cat.Spells[id]
		if sp == nil {
			continue
		}
		if prev := r.Int("SupercedesSpell"); prev != 0 && cat.Spells[prev] != nil {
			supersedes[id] = prev
		}
		lvl := 0
		if lv, ok := levelsOf[id]; ok {
			lvl = lv.Base
		}
		if old, seen := cat.skillSpells[id]; !seen || lvl < old {
			cat.skillSpells[id] = lvl
		}
		// Granted only if no row of the spell says it is trained.
		if r.Int("AcquireMethod") != acquireGranted {
			cat.granted[id] = false
		} else if _, seen := cat.granted[id]; !seen {
			cat.granted[id] = true
		}
	}
	cat.buildChains(supersedes, chainName)
	return cat, nil
}

// buildChains groups the class's spells into rank chains.
//
// Forever leaves SkillLineAbility.SupercedesSpell at 0 for most class spells
// (every rank of Heal, Renew, Shadow Bolt ...), so the link alone finds only
// the first rank. The ranks of one spell share their name and their spell
// family mask, and that is what groups them here; the explicit links still
// count where the table has them (they also join renamed ranks). Within a
// chain the level a rank is learned at gives the order. names are the
// reference names by spell id (English where the cache has them).
func (c *Catalog) buildChains(supersedes map[int]int, names map[int]string) {
	parent := map[int]int{}
	var find func(int) int
	find = func(x int) int {
		p, ok := parent[x]
		if !ok || p == x {
			return x
		}
		root := find(p)
		parent[x] = root
		return root
	}
	union := func(a, b int) {
		if ra, rb := find(a), find(b); ra != rb {
			parent[ra] = rb
		}
	}
	for id, prev := range supersedes {
		union(id, prev)
	}
	type family struct {
		name string
		mask [4]uint32
	}
	first := map[family]int{}
	for id := range c.skillSpells {
		name := names[id]
		if name == "" {
			continue // without the name table only the explicit links are left
		}
		key := family{name, c.Spells[id].ClassMask}
		if other, ok := first[key]; ok {
			union(id, other)
		} else {
			first[key] = id
		}
	}

	members := map[int][]int{}
	for id := range c.Spells {
		root := find(id)
		members[root] = append(members[root], id)
	}
	learnedAt := func(id int) int {
		lv := c.Spells[id].Levels
		if lv.Spell > 0 {
			return lv.Spell
		}
		return lv.Base
	}
	for _, ids := range members {
		sort.Slice(ids, func(i, j int) bool {
			a, b := ids[i], ids[j]
			if la, lb := learnedAt(a), learnedAt(b); la != lb {
				return la < lb
			}
			if c.granted[a] != c.granted[b] {
				return !c.granted[a] // the trained version of a rank comes first
			}
			return a < b
		})
		// The rank number counts the trained ranks; an alternative version
		// of a rank (same level) shares its number. A chain nobody trains
		// (a talent cast with ranks) counts its own levels.
		trained := false
		for _, id := range ids {
			if !c.granted[id] {
				trained = true
				break
			}
		}
		base, rank, lastLevel := 0, 0, -1
		for _, id := range ids {
			if trained && c.granted[id] {
				continue
			}
			if base == 0 {
				base = id
			}
			if lvl := learnedAt(id); lvl != lastLevel {
				rank, lastLevel = rank+1, lvl
			}
			c.rankOf[id] = rank
		}
		for _, id := range ids {
			if _, ok := c.rankOf[id]; ok {
				continue
			}
			n := 1 // the trained rank at or below this version's level
			for _, other := range ids {
				if !c.granted[other] && learnedAt(other) <= learnedAt(id) {
					n = c.rankOf[other]
				}
			}
			c.rankOf[id] = n
		}
		c.Chains[base] = ids
		for _, id := range ids {
			c.BaseOf[id] = base
			c.Spells[id].BaseID = base
		}
	}
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
// Siphon Life are their own talent spell). Spells the table marks as granted
// are left out: they are talent casts or alternative versions of a rank.
func (c *Catalog) AssumedSpellbook(level int, talents map[int]int) map[int]bool {
	known := map[int]bool{}
	for id, reqLevel := range c.skillSpells {
		if reqLevel <= level && !c.granted[id] {
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
	ranks := c.KnownRanks(baseID, known, level)
	if len(ranks) == 0 {
		return nil
	}
	return ranks[len(ranks)-1]
}

// RankNumber returns a spell's rank within its chain (1 = first rank), the
// number the game shows as "Rank N". 0 for a spell the catalog does not have.
func (c *Catalog) RankNumber(spellID int) int {
	return c.rankOf[spellID]
}

// KnownRanks returns every known rank of a spell chain the character's
// level allows, lowest first (healers pick the most efficient one). baseID
// may be any spell of the chain.
func (c *Catalog) KnownRanks(baseID int, known map[int]bool, level int) []*engine.Spell {
	var out []*engine.Spell
	for _, id := range c.Chains[c.BaseOf[baseID]] {
		if sp := c.Spells[id]; known[id] && sp.Levels.Base <= level {
			out = append(out, sp)
		}
	}
	return out
}
