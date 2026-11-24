// Package catalog turns the client's DB2 tables into the engine's spell
// model for one class: castable spells with their decoded damage and heal
// effects, rank chains, the raw aura effects (for the talent decoder) and
// the assumed spellbook when the addon has not exported one.
package catalog

import (
	"fmt"
	"sort"
	"sync"

	"github.com/kejith/wow-tools-forever/sim/db2"
	"github.com/kejith/wow-tools-forever/sim/engine"
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
	Index   int // EffectIndex: the key of the talent's per-rank values
	Aura    int
	BP      float64
	Misc0   int
	Misc1   int
	Trigger int
	Mask    [4]uint32
}

// Catalog is the decoded game data for one class.
type Catalog struct {
	ClassID int
	Spells  map[int]*engine.Spell
	// Auras holds the raw aura effects of every spell of the client, not only
	// the class's: several talents (Meditation, Wand Specialization) are not
	// in the class's spell family.
	Auras map[int][]Aura
	// RawEffects holds the aura effects of the class's spells the catalog
	// does not decode into damage or healing, by spell and effect index
	// (with base points, per-level scaling and period): a class module reads
	// what only it understands, e.g. the health Dark Sacrifice costs.
	RawEffects map[int]map[int]*engine.Effect
	// DurationMs is the duration of every spell of the client, also the
	// buffs outside the class's family that a talent triggers (Spirit Tap).
	DurationMs map[int]int
	BaseOf     map[int]int   // spell id -> first rank of its chain
	Chains     map[int][]int // first rank -> every spell of the chain, lowest rank first
	// TalentPoints are the exact values of a talent per rank from the trait
	// tables: talent spell id -> effect index -> rank -> value. Forever has
	// one spell per talent, whose SpellEffect row holds a single rank only.
	TalentPoints map[int]map[int]map[int]float64
	// HasTraits says whether the trait tables were in the cache.
	HasTraits bool
	// skillSpells are the class's spells from SkillLineAbility, with the
	// level each becomes available (for the assumed spellbook).
	skillSpells map[int]int
	// granted marks the skill spells nobody trains (AcquireMethod 3:
	// talent casts and alternative versions of a rank).
	granted map[int]bool
	rankOf  map[int]int    // spell id -> rank number (1 = first rank)
	names   map[int]string // every spell's name in the client's language
	prepMu  sync.Mutex
	prepped bool
}

// PrepareOnce runs fn at most once on this catalog (used by class modules
// such as warlock.Prepare so a cached catalog is mutated safely once).
func (c *Catalog) PrepareOnce(fn func(*Catalog)) {
	c.prepMu.Lock()
	defer c.prepMu.Unlock()
	if c.prepped {
		return
	}
	fn(c)
	c.prepped = true
}

type catKey struct {
	classID int
	tables  [14]*db2.Table
}

var (
	catMu   sync.Mutex
	catMemo = map[catKey]*Catalog{}
)

// Name is a spell's name in the client's language ("" without the table),
// also for spells outside the class's family (some talents).
func (c *Catalog) Name(spellID int) string { return c.names[spellID] }

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

	auraPeriodicDamage   = 3
	auraPeriodicHeal     = 8
	auraPeriodicEnergize = 24
	auraPeriodicLeech    = 53
	auraSchoolAbsorb     = 69

	attrPassive = 0x40 // SpellMisc.Attributes_0: SPELL_ATTR0_PASSIVE
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
	traitDefs, err1 := db2.Load(cacheDir, "TraitDefinition", build, locale)
	traitPoints, err2 := db2.Load(cacheDir, "TraitDefinitionEffectPoints", build, locale)
	traitCurves, err3 := db2.Load(cacheDir, "CurvePoint", build, locale)
	if err1 != nil || err2 != nil || err3 != nil {
		traitDefs, traitPoints, traitCurves = nil, nil, nil
	}

	ck := catKey{
		classID: classID,
		tables: [14]*db2.Table{
			classOpt, misc, effects, castTimes, durations, power, levels, cooldowns, sla,
			names, refNames, traitDefs, traitPoints, traitCurves,
		},
	}
	catMu.Lock()
	if cached := catMemo[ck]; cached != nil {
		catMu.Unlock()
		return cached, nil
	}
	catMu.Unlock()

	// Index the lookup tables (resolving column indices once per table).
	castBase := make(map[int]int, castTimes.Len())
	cCastID, cCastBase := castTimes.Col("ID"), castTimes.Col("Base")
	for i := 0; i < castTimes.Len(); i++ {
		r := castTimes.Row(i)
		castBase[r.IntAt(cCastID)] = r.IntAt(cCastBase)
	}
	durBase := make(map[int]int, durations.Len())
	cDurID, cDurVal := durations.Col("ID"), durations.Col("Duration")
	for i := 0; i < durations.Len(); i++ {
		r := durations.Row(i)
		durBase[r.IntAt(cDurID)] = r.IntAt(cDurVal)
	}
	type pow struct {
		mana, perLevel float64
		rage, energy   float64
		combo          bool
	}
	powerOf := make(map[int]pow, power.Len())
	cPowSpell, cPowOrder := power.Col("SpellID"), power.Col("OrderIndex")
	cPowMana, cPowPerLvl, cPowType := power.Col("ManaCost"), power.Col("ManaCostPerLevel"), power.Col("PowerType")
	for i := 0; i < power.Len(); i++ {
		r := power.Row(i)
		pt := 0
		if cPowType >= 0 {
			pt = r.IntAt(cPowType)
		}
		id := r.IntAt(cPowSpell)
		p := powerOf[id]
		switch pt {
		case 0: // Mana
			if p.mana == 0 || r.IntAt(cPowOrder) == 0 {
				p.mana = r.FloatAt(cPowMana)
				p.perLevel = r.FloatAt(cPowPerLvl)
			}
		case 1: // Rage (stored in tenths in DB2: 150 = 15 rage)
			p.rage = r.FloatAt(cPowMana) / 10
		case 3: // Energy
			p.energy = r.FloatAt(cPowMana)
		case 4: // Combo points
			p.combo = true
		default:
			continue
		}
		powerOf[id] = p
	}
	levelsOf := make(map[int]engine.Levels, levels.Len())
	cLvDiff, cLvSpell := levels.Col("DifficultyID"), levels.Col("SpellID")
	cLvBase, cLvMax, cLvReq := levels.Col("BaseLevel"), levels.Col("MaxLevel"), levels.Col("SpellLevel")
	for i := 0; i < levels.Len(); i++ {
		r := levels.Row(i)
		if r.IntAt(cLvDiff) != 0 {
			continue
		}
		levelsOf[r.IntAt(cLvSpell)] = engine.Levels{
			Base: r.IntAt(cLvBase), Max: r.IntAt(cLvMax), Spell: r.IntAt(cLvReq),
		}
	}
	type cds struct{ recovery, gcd int }
	cdOf := make(map[int]cds, cooldowns.Len())
	cCdDiff, cCdSpell := cooldowns.Col("DifficultyID"), cooldowns.Col("SpellID")
	cCdRec, cCdCat, cCdGCD := cooldowns.Col("RecoveryTime"), cooldowns.Col("CategoryRecoveryTime"), cooldowns.Col("StartRecoveryTime")
	for i := 0; i < cooldowns.Len(); i++ {
		r := cooldowns.Row(i)
		if r.IntAt(cCdDiff) != 0 {
			continue
		}
		rec := r.IntAt(cCdRec)
		if c := r.IntAt(cCdCat); c > rec {
			rec = c
		}
		cdOf[r.IntAt(cCdSpell)] = cds{recovery: rec, gcd: r.IntAt(cCdGCD)}
	}
	nameOf := map[int]string{}
	if names != nil {
		nameOf = make(map[int]string, names.Len())
		cNID, cNVal := names.Col("ID"), names.Col("Name_lang")
		for i := 0; i < names.Len(); i++ {
			r := names.Row(i)
			nameOf[r.IntAt(cNID)] = r.StrAt(cNVal)
		}
	}
	chainName := nameOf
	if refNames != nil && refNames != names {
		chainName = make(map[int]string, refNames.Len())
		cNID, cNVal := refNames.Col("ID"), refNames.Col("Name_lang")
		for i := 0; i < refNames.Len(); i++ {
			r := refNames.Row(i)
			chainName[r.IntAt(cNID)] = r.StrAt(cNVal)
		}
	}

	// The class's spells by SpellClassOptions.
	cat := &Catalog{
		ClassID:      classID,
		Spells:       map[int]*engine.Spell{},
		Auras:        map[int][]Aura{},
		RawEffects:   map[int]map[int]*engine.Effect{},
		DurationMs:   map[int]int{},
		TalentPoints: map[int]map[int]map[int]float64{},
		BaseOf:       map[int]int{},
		Chains:       map[int][]int{},
		skillSpells:  map[int]int{},
		granted:      map[int]bool{},
		rankOf:       map[int]int{},
		names:        nameOf,
	}
	cOptSet, cOptSpell := classOpt.Col("SpellClassSet"), classOpt.Col("SpellID")
	cOptM0, cOptM1, cOptM2, cOptM3 := classOpt.Col("SpellClassMask_0"), classOpt.Col("SpellClassMask_1"),
		classOpt.Col("SpellClassMask_2"), classOpt.Col("SpellClassMask_3")
	for i := 0; i < classOpt.Len(); i++ {
		r := classOpt.Row(i)
		if r.IntAt(cOptSet) != set {
			continue
		}
		id := r.IntAt(cOptSpell)
		cat.Spells[id] = &engine.Spell{
			ID:     id,
			BaseID: id,
			Name:   nameOf[id],
			ClassMask: [4]uint32{
				r.Uint32At(cOptM0), r.Uint32At(cOptM1),
				r.Uint32At(cOptM2), r.Uint32At(cOptM3),
			},
		}
	}

	// Timing, school, cost and levels.
	cMDiff, cMSpell := misc.Col("DifficultyID"), misc.Col("SpellID")
	cMDur, cMCast, cMSchool := misc.Col("DurationIndex"), misc.Col("CastingTimeIndex"), misc.Col("SchoolMask")
	cMA0, cMA1 := misc.Col("Attributes_0"), misc.Col("Attributes_1")
	for i := 0; i < misc.Len(); i++ {
		r := misc.Row(i)
		if r.IntAt(cMDiff) != 0 {
			continue
		}
		id := r.IntAt(cMSpell)
		d := durBase[r.IntAt(cMDur)]
		if d > 0 {
			cat.DurationMs[id] = d
		}
		sp := cat.Spells[id]
		if sp == nil {
			continue
		}
		sp.CastMs = castBase[r.IntAt(cMCast)]
		sp.DurationMs = d
		smask := r.IntAt(cMSchool)
		sp.SchoolMask = uint32(smask)
		sp.School = schoolOf(smask)
		sp.Channeled = r.IntAt(cMA1)&(0x4|0x40) != 0 // SPELL_ATTR1_IS_(SELF_)CHANNELLED
		sp.Passive = r.IntAt(cMA0)&attrPassive != 0
	}
	for id, sp := range cat.Spells {
		if p, ok := powerOf[id]; ok {
			sp.Cost = engine.Cost{Mana: p.mana, PerLevel: p.perLevel, Rage: p.rage, Energy: p.energy, ComboPoints: p.combo}
		}
		if lv, ok := levelsOf[id]; ok {
			sp.Levels = lv
		}
		if cd, ok := cdOf[id]; ok {
			sp.CooldownMs = cd.recovery
			sp.GCDMs = cd.gcd
		}
	}

	// Effects: decoded damage/heal parts of the class's spells, and the raw
	// auras of every spell for the talents.
	cEDiff, cESpell := effects.Col("DifficultyID"), effects.Col("SpellID")
	cEEff, cEAura, cEIdx := effects.Col("Effect"), effects.Col("EffectAura"), effects.Col("EffectIndex")
	cEBP, cEMisc0, cEMisc1, cETrig := effects.Col("EffectBasePointsF"), effects.Col("EffectMiscValue_0"),
		effects.Col("EffectMiscValue_1"), effects.Col("EffectTriggerSpell")
	cEM0, cEM1, cEM2, cEM3 := effects.Col("EffectSpellClassMask_0"), effects.Col("EffectSpellClassMask_1"),
		effects.Col("EffectSpellClassMask_2"), effects.Col("EffectSpellClassMask_3")
	cEPerLvl, cEVar, cECoef := effects.Col("EffectRealPointsPerLevel"), effects.Col("Variance"), effects.Col("EffectBonusCoefficient")
	cEPer, cEAmp := effects.Col("EffectAuraPeriod"), effects.Col("EffectAmplitude")
	for i := 0; i < effects.Len(); i++ {
		r := effects.Row(i)
		if r.IntAt(cEDiff) != 0 {
			continue
		}
		id := r.IntAt(cESpell)
		effect, aura := r.IntAt(cEEff), r.IntAt(cEAura)
		rawAura := func() {
			cat.Auras[id] = append(cat.Auras[id], Aura{
				Index:   r.IntAt(cEIdx),
				Aura:    aura,
				BP:      r.FloatAt(cEBP),
				Misc0:   r.IntAt(cEMisc0),
				Misc1:   r.IntAt(cEMisc1),
				Trigger: r.IntAt(cETrig),
				Mask: [4]uint32{
					r.Uint32At(cEM0), r.Uint32At(cEM1),
					r.Uint32At(cEM2), r.Uint32At(cEM3),
				},
			})
		}
		sp := cat.Spells[id]
		if sp == nil {
			if effect == effApplyAura {
				rawAura()
			}
			continue
		}
		eff := &engine.Effect{
			Index:    r.IntAt(cEIdx),
			BP:       r.FloatAt(cEBP),
			PerLevel: r.FloatAt(cEPerLvl),
			Variance: r.FloatAt(cEVar),
			Coeff:    r.FloatAt(cECoef),
			PeriodMs: r.IntAt(cEPer),
		}
		// A leech returns its damage as health, times the effect's
		// multiplier (1 for Drain Life, Siphon Life and Death Coil).
		leech := func() {
			eff.Leech = r.FloatAt(cEAmp)
			if eff.Leech <= 0 {
				eff.Leech = 1
			}
		}
		switch effect {
		case effSchoolDamage:
			sp.Direct = eff
			if sp.School == engine.Physical {
				sp.IsPhysical = true
			}
		case 17, 58: // WEAPON_DAMAGE_NOSCHOOL, WEAPON_DAMAGE
			sp.Direct = eff
			sp.IsPhysical = true
			if sp.WeaponPct == 0 {
				sp.WeaponPct = 100
			}
		case 121: // NORMALIZED_WEAPON_DMG
			sp.Direct = eff
			sp.IsPhysical = true
			sp.Normalized = true
			if sp.WeaponPct == 0 {
				sp.WeaponPct = 100
			}
		case 31: // WEAPON_PERCENT_DAMAGE
			sp.IsPhysical = true
			if eff.BP > 0 {
				sp.WeaponPct = eff.BP
			}
			if sp.Direct == nil {
				sp.Direct = &engine.Effect{Index: eff.Index}
			}
		case effHealthLeech:
			leech()
			sp.Direct = eff
		case effHeal:
			sp.Direct = eff
			sp.IsHeal = true
		case effEnergize:
			sp.GivesMana = true
		case effApplyAura:
			switch aura {
			case auraPeriodicDamage:
				sp.Periodic = eff
			case auraPeriodicLeech:
				leech()
				sp.Periodic = eff
			case auraPeriodicHeal:
				sp.Periodic = eff
				sp.IsHeal = true
			default:
				if aura == auraSchoolAbsorb {
					sp.Absorb = eff
				}
				if aura == auraPeriodicEnergize {
					sp.GivesMana = true
					sp.Energize = eff
				}
				if cat.RawEffects[id] == nil {
					cat.RawEffects[id] = map[int]*engine.Effect{}
				}
				cat.RawEffects[id][eff.Index] = eff
				rawAura()
			}
		}
	}
	if traitDefs != nil {
		cat.loadTraitsFrom(traitDefs, traitPoints, traitCurves)
	}

	// Skill spells and the explicit rank links from SkillLineAbility.
	supersedes := map[int]int{} // spell id -> the rank below it
	cSlaSpell, cSlaSup, cSlaAcq := sla.Col("Spell"), sla.Col("SupercedesSpell"), sla.Col("AcquireMethod")
	for i := 0; i < sla.Len(); i++ {
		r := sla.Row(i)
		id := r.IntAt(cSlaSpell)
		sp := cat.Spells[id]
		if sp == nil {
			continue
		}
		if prev := r.IntAt(cSlaSup); prev != 0 && cat.Spells[prev] != nil {
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
		if r.IntAt(cSlaAcq) != acquireGranted {
			cat.granted[id] = false
		} else if _, seen := cat.granted[id]; !seen {
			cat.granted[id] = true
		}
	}
	cat.buildChains(supersedes, chainName)

	catMu.Lock()
	if existing := catMemo[ck]; existing != nil {
		catMu.Unlock()
		return existing, nil
	}
	catMemo[ck] = cat
	catMu.Unlock()
	return cat, nil
}

// loadTraitsFrom reads the per-rank talent values from preloaded trait tables.
func (c *Catalog) loadTraitsFrom(defs, points, curves *db2.Table) {
	spellOf := make(map[int]int, defs.Len()) // trait definition -> talent spell
	cDefID, cDefSpell := defs.Col("ID"), defs.Col("SpellID")
	for i := 0; i < defs.Len(); i++ {
		r := defs.Row(i)
		if id := r.IntAt(cDefSpell); id != 0 {
			spellOf[r.IntAt(cDefID)] = id
		}
	}
	type target struct{ spell, effect int }
	curveOf := map[int][]target{}
	cPtDef, cPtOp := points.Col("TraitDefinitionID"), points.Col("OperationType")
	cPtCurve, cPtIdx := points.Col("CurveID"), points.Col("EffectIndex")
	for i := 0; i < points.Len(); i++ {
		r := points.Row(i)
		spell := spellOf[r.IntAt(cPtDef)]
		if spell == 0 || r.IntAt(cPtOp) != 0 { // 0 = the curve sets the value
			continue
		}
		curveOf[r.IntAt(cPtCurve)] = append(curveOf[r.IntAt(cPtCurve)], target{spell, r.IntAt(cPtIdx)})
	}
	cCrvID, cCrvP0, cCrvP1 := curves.Col("CurveID"), curves.Col("Pos_0"), curves.Col("Pos_1")
	for i := 0; i < curves.Len(); i++ {
		r := curves.Row(i)
		for _, t := range curveOf[r.IntAt(cCrvID)] {
			byEffect := c.TalentPoints[t.spell]
			if byEffect == nil {
				byEffect = map[int]map[int]float64{}
				c.TalentPoints[t.spell] = byEffect
			}
			if byEffect[t.effect] == nil {
				byEffect[t.effect] = map[int]float64{}
			}
			byEffect[t.effect][int(r.FloatAt(cCrvP0)+0.5)] = r.FloatAt(cCrvP1)
		}
	}
	c.HasTraits = true
}

// TalentValue is the value of a talent effect at a rank: the trait tables'
// exact number when they have one (exact true), else the SpellEffect base
// points the caller passes in.
func (c *Catalog) TalentValue(spellID, effectIndex, rank int, basePoints float64) (value float64, exact bool) {
	if v, ok := c.TalentPoints[spellID][effectIndex][rank]; ok {
		return v, true
	}
	return basePoints, false
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
