// Package itemsets loads WoW ItemSet and ItemSetSpell DB2 tables from the shared cache
// and decodes set bonuses into canonical stat deltas (for both the combat engine and the
// weight scale) as well as identifying spell-modifying set bonuses for the combat engine.
package itemsets

import (
	"fmt"
	"sort"
	"sync"

	"github.com/kejith/wow-tools-forever/sim/db2"
)

// Forever's standard rating-to-percent conversion factors for Aura 189 (MOD_RATING).
const (
	hitRatingPerPct  = 10.0
	critRatingPerPct = 14.0
)

// Effect and aura IDs from SpellEffect.db2 relevant to item set bonuses.
const (
	effApplyAura          = 6
	effApplyAreaAuraParty = 35

	auraModDamageDone      = 13
	auraModResistance      = 22
	auraModStat            = 29
	auraModSkill           = 30
	auraModParry           = 47
	auraModDodge           = 49
	auraModBlock           = 51
	auraModCrit            = 52
	auraModHit             = 54
	auraModSpellHit        = 55
	auraModSpellCrit       = 57
	auraModCastSpeed       = 65
	auraModSpellCritSchool = 71
	auraModCostSchoolPct   = 72
	auraModDamagePctDone   = 79
	auraModPowerRegen      = 85
	auraModAttackPower     = 99
	auraAddFlatModifier    = 107
	auraAddPctModifier     = 108
	auraModRangedAP        = 124
	auraModManaRegenCast   = 134
	auraModHealingDone     = 135
	auraModHealthRegen     = 161
	auraModRating          = 189
	auraModBlockValue      = 274
	auraModCritAll         = 290
	auraModMeleeRangedHste = 342
	auraModBonusArmor      = 674

	skillDefense = 95

	// Rating bitmasks for Aura 189 (MOD_RATING).
	crHitMelee   = 1 << 5
	crHitRanged  = 1 << 6
	crHitSpell   = 1 << 7
	crCritMelee  = 1 << 8
	crCritRanged = 1 << 9
	crCritSpell  = 1 << 10
)

// Bonus is one threshold bonus of an item set.
type Bonus struct {
	SetID         int
	SetName       string
	Threshold     int
	SpellID       int
	SpellName     string
	SpellClassSet int
	// Stats holds sheet-visible stat bonuses in canonical engine delta keys:
	// spDamage, healing, spHoly..spArcane, str, agi, sta, int, spi,
	// meleeAp, rap, meleeHit, spellHit, meleeCrit, spellCrit, spellCritHoly..Arcane,
	// crit, haste, meleeHaste, mp5, hp5, armor, defense, dodge, parry, block, blockValue.
	Stats map[string]float64
	// HasNonSheetMods is true when the bonus spell carries non-sheet engine auras
	// (aura 72, 79, 107, 108, 134) that the combat engine decodes into engine.Mods.
	HasNonSheetMods bool
	// HasUnmodeled is true when the bonus spell has proc/dummy/creature-specific effects
	// that cannot be expressed as static stats or spell modifiers.
	HasUnmodeled bool
}

// Set is one WoW ItemSet with its member items and threshold bonuses.
type Set struct {
	ID                int
	Name              string
	RequiredSkill     int
	RequiredSkillRank int
	Items             []int
	Bonuses           []Bonus // sorted by Threshold, then SpellID
}

// ActiveBonus is a bonus currently active for a given set of equipped items.
type ActiveBonus struct {
	Bonus
	Equipped int
	Total    int
}

// SetSummary describes a set for which the character wears at least one piece.
type SetSummary struct {
	ID                int
	Name              string
	Equipped          int
	Total             int
	RequiredSkill     int
	RequiredSkillRank int
	SkillMet          bool
	Bonuses           []BonusState
}

// BonusState pairs a set bonus with whether it is currently active.
type BonusState struct {
	Bonus
	Active bool
}

// Catalog maps item IDs to sets and sets to their bonuses.
type Catalog struct {
	Sets      map[int]*Set
	ItemToSet map[int]int
}

// Empty returns an empty non-nil catalog.
func Empty() *Catalog {
	return &Catalog{Sets: map[int]*Set{}, ItemToSet: map[int]int{}}
}

type memoKey struct {
	tables [6]*db2.Table
}

var (
	memoMu sync.Mutex
	memo   = map[memoKey]*Catalog{}
)

// Load reads ItemSet, ItemSetSpell, SpellEffect (and optional SpellClassOptions,
// SpellName, ItemSparse) from the DB2 cache. Returns an empty catalog if the set
// tables are not present in the cache so offline runs and tests keep working.
func Load(cacheDir, build, locale string) *Catalog {
	if cacheDir == "" || build == "" {
		return Empty()
	}
	itemSet, err := db2.Load(cacheDir, "ItemSet", build, locale)
	if err != nil && locale != "enUS" {
		itemSet, err = db2.Load(cacheDir, "ItemSet", build, "enUS")
	}
	if err != nil {
		return Empty()
	}
	itemSetSpell, err := db2.Load(cacheDir, "ItemSetSpell", build, locale)
	if err != nil {
		return Empty()
	}
	spellEffect, err := db2.Load(cacheDir, "SpellEffect", build, locale)
	if err != nil {
		return Empty()
	}
	classOpt, _ := db2.Load(cacheDir, "SpellClassOptions", build, locale)
	spellName, err := db2.Load(cacheDir, "SpellName", build, locale)
	if err != nil && locale != "enUS" {
		spellName, _ = db2.Load(cacheDir, "SpellName", build, "enUS")
	}
	itemSparse, err := db2.Load(cacheDir, "ItemSparse", build, locale)
	if err != nil && locale != "enUS" {
		itemSparse, _ = db2.Load(cacheDir, "ItemSparse", build, "enUS")
	}

	key := memoKey{tables: [6]*db2.Table{itemSet, itemSetSpell, spellEffect, classOpt, spellName, itemSparse}}
	memoMu.Lock()
	if cached := memo[key]; cached != nil {
		memoMu.Unlock()
		return cached
	}
	memoMu.Unlock()

	cat := Build(itemSet, itemSetSpell, spellEffect, classOpt, spellName, itemSparse)

	memoMu.Lock()
	memo[key] = cat
	memoMu.Unlock()
	return cat
}

// Build constructs a Catalog from already-loaded DB2 tables (exported for tests).
func Build(itemSet, itemSetSpell, spellEffect, spellClassOpt, spellName, itemSparse *db2.Table) *Catalog {
	cat := Empty()
	if itemSet == nil || itemSetSpell == nil || spellEffect == nil {
		return cat
	}

	// Parse ItemSet rows.
	cID := itemSet.Col("ID")
	cName := itemSet.Col("Name_lang")
	cReqSkill := itemSet.Col("RequiredSkill")
	cReqRank := itemSet.Col("RequiredSkillRank")
	var itemCols []int
	for i := 0; i < 17; i++ {
		if c := itemSet.Col(fmt.Sprintf("ItemID_%d", i)); c >= 0 {
			itemCols = append(itemCols, c)
		}
	}
	for i := 0; i < itemSet.Len(); i++ {
		r := itemSet.Row(i)
		id := r.IntAt(cID)
		if id <= 0 {
			continue
		}
		s := &Set{
			ID:                id,
			Name:              r.StrAt(cName),
			RequiredSkill:     r.IntAt(cReqSkill),
			RequiredSkillRank: r.IntAt(cReqRank),
		}
		seen := map[int]bool{}
		for _, c := range itemCols {
			iid := r.IntAt(c)
			if iid > 0 && !seen[iid] {
				seen[iid] = true
				s.Items = append(s.Items, iid)
				cat.ItemToSet[iid] = id
			}
		}
		cat.Sets[id] = s
	}

	// Merge any items linked via ItemSparse.ItemSet not listed in ItemSet.ItemID_*.
	if itemSparse != nil {
		cSpID := itemSparse.Col("ID")
		cSpSet := itemSparse.Col("ItemSet")
		if cSpID >= 0 && cSpSet >= 0 {
			for i := 0; i < itemSparse.Len(); i++ {
				r := itemSparse.Row(i)
				setID := r.IntAt(cSpSet)
				if setID <= 0 {
					continue
				}
				iid := r.IntAt(cSpID)
				if iid <= 0 {
					continue
				}
				if _, ok := cat.ItemToSet[iid]; !ok {
					if s := cat.Sets[setID]; s != nil {
						s.Items = append(s.Items, iid)
						cat.ItemToSet[iid] = setID
					}
				}
			}
		}
	}

	// Collect all bonus SpellIDs from ItemSetSpell.
	cSpSpell := itemSetSpell.Col("SpellID")
	cSpThr := itemSetSpell.Col("Threshold")
	cSpSetID := itemSetSpell.Col("ItemSetID")
	needSpells := map[int]bool{}
	type rawBonus struct {
		setID     int
		threshold int
		spellID   int
	}
	var rawBonuses []rawBonus
	for i := 0; i < itemSetSpell.Len(); i++ {
		r := itemSetSpell.Row(i)
		setID := r.IntAt(cSpSetID)
		thr := r.IntAt(cSpThr)
		spellID := r.IntAt(cSpSpell)
		if setID <= 0 || thr <= 0 || spellID <= 0 || cat.Sets[setID] == nil {
			continue
		}
		needSpells[spellID] = true
		rawBonuses = append(rawBonuses, rawBonus{setID: setID, threshold: thr, spellID: spellID})
	}

	// SpellClassSet per needed bonus spell (optional).
	classSetOf := map[int]int{}
	if spellClassOpt != nil {
		cOptSpell := spellClassOpt.Col("SpellID")
		cOptSet := spellClassOpt.Col("SpellClassSet")
		if cOptSpell >= 0 && cOptSet >= 0 {
			for i := 0; i < spellClassOpt.Len(); i++ {
				r := spellClassOpt.Row(i)
				sid := r.IntAt(cOptSpell)
				if needSpells[sid] {
					classSetOf[sid] = r.IntAt(cOptSet)
				}
			}
		}
	}

	// SpellName per needed bonus spell (optional).
	spellNameOf := map[int]string{}
	if spellName != nil {
		cNID := spellName.Col("ID")
		cNVal := spellName.Col("Name_lang")
		if cNID >= 0 && cNVal >= 0 {
			for i := 0; i < spellName.Len(); i++ {
				r := spellName.Row(i)
				sid := r.IntAt(cNID)
				if needSpells[sid] {
					spellNameOf[sid] = r.StrAt(cNVal)
				}
			}
		}
	}

	// Decode SpellEffect rows for needed bonus spells.
	type decodedSpell struct {
		stats           map[string]float64
		hasNonSheetMods bool
		hasUnmodeled    bool
	}
	decoded := map[int]*decodedSpell{}
	cEDiff := spellEffect.Col("DifficultyID")
	cESpell := spellEffect.Col("SpellID")
	cEEff := spellEffect.Col("Effect")
	cEAura := spellEffect.Col("EffectAura")
	cEBP := spellEffect.Col("EffectBasePointsF")
	cEMisc0 := spellEffect.Col("EffectMiscValue_0")

	for i := 0; i < spellEffect.Len(); i++ {
		r := spellEffect.Row(i)
		if cEDiff >= 0 && r.IntAt(cEDiff) != 0 {
			continue
		}
		sid := r.IntAt(cESpell)
		if !needSpells[sid] {
			continue
		}
		eff := r.IntAt(cEEff)
		if eff != effApplyAura && eff != effApplyAreaAuraParty {
			continue
		}
		d := decoded[sid]
		if d == nil {
			d = &decodedSpell{stats: map[string]float64{}}
			decoded[sid] = d
		}
		aura := r.IntAt(cEAura)
		bp := r.FloatAt(cEBP)
		m0 := r.IntAt(cEMisc0)
		decodeSetAura(d.stats, &d.hasNonSheetMods, &d.hasUnmodeled, aura, bp, m0)
	}

	// Attach bonuses to sets.
	for _, rb := range rawBonuses {
		s := cat.Sets[rb.setID]
		b := Bonus{
			SetID:         rb.setID,
			SetName:       s.Name,
			Threshold:     rb.threshold,
			SpellID:       rb.spellID,
			SpellName:     spellNameOf[rb.spellID],
			SpellClassSet: classSetOf[rb.spellID],
		}
		if d := decoded[rb.spellID]; d != nil {
			if len(d.stats) > 0 {
				b.Stats = cleanStats(d.stats)
			}
			b.HasNonSheetMods = d.hasNonSheetMods
			b.HasUnmodeled = d.hasUnmodeled
		}
		s.Bonuses = append(s.Bonuses, b)
	}
	for _, s := range cat.Sets {
		sort.Slice(s.Bonuses, func(i, j int) bool {
			if s.Bonuses[i].Threshold != s.Bonuses[j].Threshold {
				return s.Bonuses[i].Threshold < s.Bonuses[j].Threshold
			}
			return s.Bonuses[i].SpellID < s.Bonuses[j].SpellID
		})
	}
	return cat
}

func decodeSetAura(stats map[string]float64, hasNonSheet, hasUnmodeled *bool, aura int, bp float64, m0 int) {
	switch aura {
	case auraModDamageDone:
		if m0 == 126 { // all 6 magic schools
			stats["spDamage"] += bp
		} else {
			if m0&(1<<1) != 0 {
				stats["spHoly"] += bp
			}
			if m0&(1<<2) != 0 {
				stats["spFire"] += bp
			}
			if m0&(1<<3) != 0 {
				stats["spNature"] += bp
			}
			if m0&(1<<4) != 0 {
				stats["spFrost"] += bp
			}
			if m0&(1<<5) != 0 {
				stats["spShadow"] += bp
			}
			if m0&(1<<6) != 0 {
				stats["spArcane"] += bp
			}
		}
	case auraModHealingDone:
		stats["healing"] += bp
	case auraModStat:
		switch m0 {
		case 0:
			stats["str"] += bp
		case 1:
			stats["agi"] += bp
		case 2:
			stats["sta"] += bp
		case 3:
			stats["int"] += bp
		case 4:
			stats["spi"] += bp
		case -1:
			stats["str"] += bp
			stats["agi"] += bp
			stats["sta"] += bp
			stats["int"] += bp
			stats["spi"] += bp
		}
	case auraModAttackPower:
		stats["meleeAp"] += bp
	case auraModRangedAP:
		stats["rap"] += bp
	case auraModHit:
		stats["meleeHit"] += bp
	case auraModSpellHit:
		stats["spellHit"] += bp
	case auraModCrit:
		stats["meleeCrit"] += bp
	case auraModSpellCrit, auraModSpellCritSchool:
		if m0 == 0 || m0 == 126 {
			stats["spellCrit"] += bp
		} else {
			if m0&(1<<1) != 0 {
				stats["spellCritHoly"] += bp
			}
			if m0&(1<<2) != 0 {
				stats["spellCritFire"] += bp
			}
			if m0&(1<<3) != 0 {
				stats["spellCritNature"] += bp
			}
			if m0&(1<<4) != 0 {
				stats["spellCritFrost"] += bp
			}
			if m0&(1<<5) != 0 {
				stats["spellCritShadow"] += bp
			}
			if m0&(1<<6) != 0 {
				stats["spellCritArcane"] += bp
			}
		}
	case auraModCritAll:
		stats["crit"] += bp
	case auraModRating:
		handled := false
		if m0&(crHitMelee|crHitRanged) != 0 {
			stats["meleeHit"] += bp / hitRatingPerPct
			handled = true
		}
		if m0&crHitSpell != 0 {
			stats["spellHit"] += bp / hitRatingPerPct
			handled = true
		}
		if m0&(crCritMelee|crCritRanged) != 0 {
			stats["meleeCrit"] += bp / critRatingPerPct
			handled = true
		}
		if m0&crCritSpell != 0 {
			stats["spellCrit"] += bp / critRatingPerPct
			handled = true
		}
		if !handled {
			*hasUnmodeled = true
		}
	case auraModCastSpeed:
		stats["haste"] += bp
	case auraModMeleeRangedHste:
		stats["meleeHaste"] += bp
	case auraModPowerRegen:
		stats["mp5"] += bp
	case auraModHealthRegen:
		stats["hp5"] += bp
	case auraModBonusArmor:
		stats["armor"] += bp
	case auraModResistance:
		if m0 == 1 { // Physical resistance = armor
			stats["armor"] += bp
		}
	case auraModSkill:
		if m0 == skillDefense {
			stats["defense"] += bp
		}
	case auraModDodge:
		stats["dodge"] += bp
	case auraModParry:
		stats["parry"] += bp
	case auraModBlock:
		stats["block"] += bp
	case auraModBlockValue:
		stats["blockValue"] += bp
	case auraModCostSchoolPct, auraModDamagePctDone, auraAddFlatModifier, auraAddPctModifier, auraModManaRegenCast:
		*hasNonSheet = true
	default:
		// Utility-only auras (threat, speed, resists) are ignored; procs/dummies mark unmodeled.
		switch aura {
		case 10, 14, 31, 58, 123, 129, 130, 232:
			// utility: no impact on DPS/HPS metrics
		default:
			*hasUnmodeled = true
		}
	}
}

func cleanStats(m map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, v := range m {
		if v != 0 {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SkillMet reports whether a character's skill ranks (skillID -> rank) satisfy the set's requirement.
// If skills is nil, sets with a RequiredSkill are treated as not met.
func (s *Set) SkillMet(skills map[int]int) bool {
	if s.RequiredSkill <= 0 {
		return true
	}
	return skills[s.RequiredSkill] >= s.RequiredSkillRank
}

// SetOf returns the Set an item belongs to, or nil if it is not part of a set.
func (c *Catalog) SetOf(itemID int64) *Set {
	if c == nil || itemID <= 0 {
		return nil
	}
	if sid, ok := c.ItemToSet[int(itemID)]; ok {
		return c.Sets[sid]
	}
	return nil
}

// Counts returns the number of unique equipped items per set ID.
func (c *Catalog) Counts(itemIDs []int64) map[int]int {
	counts := map[int]int{}
	if c == nil {
		return counts
	}
	seen := make(map[int64]bool, len(itemIDs))
	for _, iid := range itemIDs {
		if iid <= 0 || seen[iid] {
			continue
		}
		seen[iid] = true
		if sid, ok := c.ItemToSet[int(iid)]; ok {
			counts[sid]++
		}
	}
	return counts
}

// ActiveBonuses returns all set bonuses active for the given equipped item IDs and skill ranks,
// ordered deterministically by SetID, Threshold, SpellID.
func (c *Catalog) ActiveBonuses(itemIDs []int64, skills map[int]int) []ActiveBonus {
	if c == nil || len(c.Sets) == 0 {
		return nil
	}
	counts := c.Counts(itemIDs)
	if len(counts) == 0 {
		return nil
	}
	setIDs := make([]int, 0, len(counts))
	for sid := range counts {
		setIDs = append(setIDs, sid)
	}
	sort.Ints(setIDs)
	var out []ActiveBonus
	for _, sid := range setIDs {
		s := c.Sets[sid]
		if s == nil || !s.SkillMet(skills) {
			continue
		}
		n := counts[sid]
		for _, b := range s.Bonuses {
			if n >= b.Threshold {
				out = append(out, ActiveBonus{Bonus: b, Equipped: n, Total: len(s.Items)})
			}
		}
	}
	return out
}

// Summary returns all sets for which at least one item is equipped, ordered by SetID.
func (c *Catalog) Summary(itemIDs []int64, skills map[int]int) []SetSummary {
	if c == nil || len(c.Sets) == 0 {
		return nil
	}
	counts := c.Counts(itemIDs)
	if len(counts) == 0 {
		return nil
	}
	setIDs := make([]int, 0, len(counts))
	for sid := range counts {
		setIDs = append(setIDs, sid)
	}
	sort.Ints(setIDs)
	out := make([]SetSummary, 0, len(setIDs))
	for _, sid := range setIDs {
		s := c.Sets[sid]
		if s == nil {
			continue
		}
		n := counts[sid]
		met := s.SkillMet(skills)
		bs := make([]BonusState, len(s.Bonuses))
		for i, b := range s.Bonuses {
			bs[i] = BonusState{Bonus: b, Active: met && n >= b.Threshold}
		}
		out = append(out, SetSummary{
			ID:                s.ID,
			Name:              s.Name,
			Equipped:          n,
			Total:             len(s.Items),
			RequiredSkill:     s.RequiredSkill,
			RequiredSkillRank: s.RequiredSkillRank,
			SkillMet:          met,
			Bonuses:           bs,
		})
	}
	return out
}

type bonusKey struct {
	setID     int
	threshold int
	spellID   int
}

// DiffBonuses computes which bonuses are gained and which are lost when moving from base to next.
func DiffBonuses(base, next []ActiveBonus) (gained, lost []ActiveBonus) {
	baseMap := make(map[bonusKey]ActiveBonus, len(base))
	for _, b := range base {
		baseMap[bonusKey{b.SetID, b.Threshold, b.SpellID}] = b
	}
	nextMap := make(map[bonusKey]ActiveBonus, len(next))
	for _, b := range next {
		k := bonusKey{b.SetID, b.Threshold, b.SpellID}
		nextMap[k] = b
		if _, ok := baseMap[k]; !ok {
			gained = append(gained, b)
		}
	}
	for _, b := range base {
		k := bonusKey{b.SetID, b.Threshold, b.SpellID}
		if _, ok := nextMap[k]; !ok {
			lost = append(lost, b)
		}
	}
	return gained, lost
}

// DeltaStats sums engine stat deltas for gained bonuses minus lost bonuses.
func DeltaStats(gained, lost []ActiveBonus) map[string]float64 {
	if len(gained) == 0 && len(lost) == 0 {
		return nil
	}
	d := map[string]float64{}
	for _, b := range gained {
		for k, v := range b.Stats {
			d[k] += v
		}
	}
	for _, b := range lost {
		for k, v := range b.Stats {
			d[k] -= v
		}
	}
	return cleanStats(d)
}

// NonSheetSpellIDs returns the sorted unique SpellIDs of active bonuses that carry non-sheet engine mods.
func NonSheetSpellIDs(bonuses []ActiveBonus) []int {
	if len(bonuses) == 0 {
		return nil
	}
	seen := map[int]bool{}
	var ids []int
	for _, b := range bonuses {
		if b.HasNonSheetMods && !seen[b.SpellID] {
			seen[b.SpellID] = true
			ids = append(ids, b.SpellID)
		}
	}
	sort.Ints(ids)
	return ids
}

// AllSpellIDs returns the sorted unique SpellIDs of all active bonuses (for coverage reporting).
func AllSpellIDs(bonuses []ActiveBonus) []int {
	if len(bonuses) == 0 {
		return nil
	}
	seen := map[int]bool{}
	var ids []int
	for _, b := range bonuses {
		if !seen[b.SpellID] {
			seen[b.SpellID] = true
			ids = append(ids, b.SpellID)
		}
	}
	sort.Ints(ids)
	return ids
}

// ScaleStats converts active bonuses into weight-scale stat keys.
// physical selects melee/ranged hit & crit instead of spell hit & crit; topSchool (e.g. "shadow", "holy")
// picks any school-specific spell crit bonuses for caster/healer scales.
func ScaleStats(bonuses []ActiveBonus, physical bool, topSchool string) map[string]float64 {
	if len(bonuses) == 0 {
		return nil
	}
	out := map[string]float64{}
	for _, b := range bonuses {
		for k, v := range b.Stats {
			switch k {
			case "spDamage":
				out["sp"] += v
				out["sp_all"] += v
				out["sp_holy"] += v
				out["sp_fire"] += v
				out["sp_nature"] += v
				out["sp_frost"] += v
				out["sp_shadow"] += v
				out["sp_arcane"] += v
			case "healing":
				out["healing"] += v
			case "spHoly":
				out["sp_holy"] += v
			case "spFire":
				out["sp_fire"] += v
			case "spNature":
				out["sp_nature"] += v
			case "spFrost":
				out["sp_frost"] += v
			case "spShadow":
				out["sp"] += v
				out["sp_shadow"] += v
			case "spArcane":
				out["sp_arcane"] += v
			case "str":
				out["strength"] += v
			case "agi":
				out["agility"] += v
			case "sta":
				out["stamina"] += v
			case "int":
				out["intellect"] += v
			case "spi":
				out["spirit"] += v
			case "meleeAp":
				out["ap"] += v
			case "rap":
				out["rap"] += v
			case "mp5":
				out["mp5"] += v
			case "hp5":
				out["hp5"] += v
			case "armor":
				out["armor"] += v
			case "defense":
				out["defense"] += v
			case "dodge":
				out["dodge"] += v
			case "parry":
				out["parry"] += v
			case "block":
				out["block"] += v
			case "blockValue":
				out["block_value"] += v
			case "meleeHit":
				if physical {
					out["hit"] += v
				}
			case "spellHit":
				if !physical {
					out["hit"] += v
				}
			case "meleeCrit":
				if physical {
					out["crit"] += v
				}
			case "spellCrit":
				if !physical {
					out["crit"] += v
				}
			case "crit":
				out["crit"] += v
			case "spellCritHoly":
				if !physical && (topSchool == "" || topSchool == "holy") {
					out["crit"] += v
				}
			case "spellCritFire":
				if !physical && topSchool == "fire" {
					out["crit"] += v
				}
			case "spellCritNature":
				if !physical && topSchool == "nature" {
					out["crit"] += v
				}
			case "spellCritFrost":
				if !physical && topSchool == "frost" {
					out["crit"] += v
				}
			case "spellCritShadow":
				if !physical && topSchool == "shadow" {
					out["crit"] += v
				}
			case "spellCritArcane":
				if !physical && topSchool == "arcane" {
					out["crit"] += v
				}
			}
		}
	}
	return cleanStats(out)
}
