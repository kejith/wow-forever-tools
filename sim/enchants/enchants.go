// Package enchants loads WoW SpellItemEnchantment, SpellEquippedItems, SpellEffect,
// SkillLineAbility, SpellReagents, ItemEffect, ItemXItemEffect, SpellName and ItemSparse
// DB2 tables from the shared cache. It decodes permanent and temporary enchantments into
// canonical engine stat deltas, parses active enchant IDs from item links, and catalogs
// obtainable permanent enchants (Enchanting recipes and item-applied enchants like armor
// kits, scopes, librams and ZG enchants) with slot/item matching rules.
package enchants

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kejith/wow-tools-forever/sim/db2"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/internal/tables"
)

const (
	hitRatingPerPct  = 10.0
	critRatingPerPct = 14.0

	skillEnchanting = 333

	effApplyAura          = 6
	effApplyAreaAuraParty = 35
	effEnchantItem        = 53

	// SpellItemEnchantment.Effect_* values.
	enchTypeCombatSpell = 1
	enchTypeDamage      = 2
	enchTypeEquipSpell  = 3
	enchTypeResistance  = 4
	enchTypeStat        = 5
	enchTypeUseSpell    = 7

	// SpellEffect.EffectAura IDs relevant to enchant equip spells.
	auraModDamageDone      = 13
	auraModResistance      = 22
	auraModStat            = 29
	auraModSkill           = 30
	auraModHealth          = 34
	auraModPower           = 35
	auraProcTriggerSpell   = 42
	auraProcTriggerDamage  = 43
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

	// Weapon subclass bitmasks (Item.ClassID == 2).
	subMask1H     uint32 = (1 << 0) | (1 << 4) | (1 << 7) | (1 << 13) | (1 << 15)
	subMask2H     uint32 = (1 << 1) | (1 << 5) | (1 << 6) | (1 << 8) | (1 << 10) | (1 << 17)
	subMaskRanged uint32 = (1 << 2) | (1 << 3) | (1 << 18)
	subMaskShield uint32 = 1 << 6 // Armor subclass 6 = Shield
)

// Enchant is one decoded SpellItemEnchantment row.
type Enchant struct {
	ID                int
	Name              string
	Duration          int // 0 = permanent, >0 = temporary (seconds)
	Stats             map[string]float64
	IsProc            bool
	HasNonSheetMods   bool
	HasUnmodeled      bool
	MinLevel          int
	ItemLevelMin      int
	RequiredSkillID   int
	RequiredSkillRank int
}

// Reagent is one material required to cast an Enchanting recipe.
type Reagent struct {
	ItemID int64
	Count  int
}

// Option is one obtainable permanent enchant (an Enchanting recipe or an item-applied enchant).
type Option struct {
	ID             string // "spell:<id>" or "item:<id>"
	EnchantID      int
	Enchant        *Enchant
	Source         string // "enchanting" or "item"
	SpellID        int
	SpellName      string
	ItemID         int64
	ItemName       string
	Quality        int
	ReqLevel       int
	ReqSkill       int
	ReqSkillRank   int
	AllowableClass int64
	Reagents       []Reagent
	EquipClass     int
	EquipInvMask   uint32
	EquipSubMask   uint32
	Slots          []int64
	TwoHandOnly    bool
	RangedOnly     bool
	ShieldOnly     bool
	HoldableOnly   bool
}

// Catalog holds all decoded SpellItemEnchantment entries and obtainable permanent enchant options.
type Catalog struct {
	ByID      map[int]*Enchant
	Options   []*Option
	BySlot    map[int64][]*Option
	ItemNames map[int64]string
	ItemBuy   map[int64]int64 // unit vendor buy price in copper (BuyPrice / VendorStackCount)
}

// Empty returns an initialized empty catalog.
func Empty() *Catalog {
	return &Catalog{
		ByID:      map[int]*Enchant{},
		BySlot:    map[int64][]*Option{},
		ItemNames: map[int64]string{},
		ItemBuy:   map[int64]int64{},
	}
}

var itemLinkEnchantRe = regexp.MustCompile(`item:\d+:(\d+)`)
var spellRefRe = regexp.MustCompile(`\$(\d+)[mMsS]([1-3])`)

// ParseEnchantID extracts the permanent SpellItemEnchantment ID from an equipped item record:
// prefers the explicit enchantId field exported by WoWToolsForever, falling back to parsing the
// second field of the WoW item hyperlink (`|Hitem:<itemID>:<enchantID>:...`). Returns 0 if none.
func ParseEnchantID(rec *sv.Table) int {
	if rec == nil {
		return 0
	}
	if v, ok := rec.Num("enchantId"); ok && v > 0 {
		return int(v)
	}
	link := rec.Str("link")
	if link == "" {
		return 0
	}
	m := itemLinkEnchantRe.FindStringSubmatch(link)
	if m == nil {
		return 0
	}
	id, err := strconv.Atoi(m[1])
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

type memoKey struct {
	tables [10]*db2.Table
}

var (
	memoMu sync.Mutex
	memo   = map[memoKey]*Catalog{}
)

// EnsureTables downloads the DB2 tables required for the enchant catalog if they are missing.
func EnsureTables(cacheDir, build, locale string) {
	if build == "" {
		return
	}
	if cacheDir == "" {
		cacheDir = tables.CacheDir()
	}
	for _, name := range []string{
		"SpellItemEnchantment", "SpellEquippedItems", "SpellEffect",
		"SkillLineAbility", "SpellReagents", "ItemEffect", "ItemXItemEffect",
		"SpellName", "ItemSparse",
	} {
		tables.Ensure(cacheDir, name, build, locale)
	}
	if locale != "enUS" {
		tables.Ensure(cacheDir, "SpellName", build, "enUS")
	}
}

// Load reads enchant tables from the DB2 cache and returns a memoized Catalog.
// Returns an empty catalog if the core tables are unavailable offline.
func Load(cacheDir, build, locale string) *Catalog {
	if build == "" {
		return Empty()
	}
	if cacheDir == "" {
		cacheDir = tables.CacheDir()
	}
	if locale == "" {
		locale = "enUS"
	}
	sie, err := db2.Load(cacheDir, "SpellItemEnchantment", build, locale)
	if err != nil {
		return Empty()
	}
	spellEffect, err := db2.Load(cacheDir, "SpellEffect", build, locale)
	if err != nil {
		return Empty()
	}
	sei, _ := db2.Load(cacheDir, "SpellEquippedItems", build, locale)
	sla, _ := db2.Load(cacheDir, "SkillLineAbility", build, locale)
	reagents, _ := db2.Load(cacheDir, "SpellReagents", build, locale)
	itemEffect, _ := db2.Load(cacheDir, "ItemEffect", build, locale)
	itemXEffect, _ := db2.Load(cacheDir, "ItemXItemEffect", build, locale)
	spellName, err := db2.Load(cacheDir, "SpellName", build, locale)
	if err != nil && locale != "enUS" {
		spellName, _ = db2.Load(cacheDir, "SpellName", build, "enUS")
	}
	spellNameEN, _ := db2.Load(cacheDir, "SpellName", build, "enUS")
	itemSparse, err := db2.Load(cacheDir, "ItemSparse", build, locale)
	if err != nil && locale != "enUS" {
		itemSparse, _ = db2.Load(cacheDir, "ItemSparse", build, "enUS")
	}

	key := memoKey{tables: [10]*db2.Table{
		sie, spellEffect, sei, sla, reagents, itemEffect, itemXEffect, spellName, spellNameEN, itemSparse,
	}}
	memoMu.Lock()
	if cached := memo[key]; cached != nil {
		memoMu.Unlock()
		return cached
	}
	memoMu.Unlock()

	cat := Build(sie, spellEffect, sei, sla, reagents, itemEffect, itemXEffect, spellName, spellNameEN, itemSparse)

	memoMu.Lock()
	memo[key] = cat
	memoMu.Unlock()
	return cat
}

type rawAura struct {
	aura int
	bp   float64
	m0   int
}

type equipReq struct {
	class   int
	invMask uint32
	subMask uint32
}

// Build constructs an enchant Catalog from already-loaded DB2 tables (exported for tests).
func Build(sie, spellEffect, spellEquipped, skillLineAbility, spellReagents,
	itemEffect, itemXEffect, spellName, spellNameEN, itemSparse *db2.Table) *Catalog {
	cat := Empty()
	if sie == nil || spellEffect == nil {
		return cat
	}

	// Pass 1: collect all equip spells and spell references ($<spellID>M<idx>) in SpellItemEnchantment.
	equipSpellNeed := map[int]bool{}
	refSpellNeed := map[int]bool{}
	cID := sie.Col("ID")
	cName := sie.Col("Name_lang")
	cDur := sie.Col("Duration")
	cMinLv := sie.Col("MinLevel")
	cIlvlMin := sie.Col("ItemLevelMin")
	cReqSkill := sie.Col("RequiredSkillID")
	cReqRank := sie.Col("RequiredSkillRank")
	var cEff, cPts, cArg [3]int
	for i := 0; i < 3; i++ {
		cEff[i] = sie.Col(fmt.Sprintf("Effect_%d", i))
		cPts[i] = sie.Col(fmt.Sprintf("EffectPointsMin_%d", i))
		cArg[i] = sie.Col(fmt.Sprintf("EffectArg_%d", i))
	}

	for i := 0; i < sie.Len(); i++ {
		r := sie.Row(i)
		for j := 0; j < 3; j++ {
			if r.IntAt(cEff[j]) == enchTypeEquipSpell {
				if arg := r.IntAt(cArg[j]); arg > 0 {
					equipSpellNeed[arg] = true
				}
			}
		}
		if name := r.StrAt(cName); strings.Contains(name, "$") {
			for _, m := range spellRefRe.FindAllStringSubmatch(name, -1) {
				if sid, err := strconv.Atoi(m[1]); err == nil && sid > 0 {
					refSpellNeed[sid] = true
				}
			}
		}
	}

	// Read SpellEffect once: extract ENCHANT_ITEM mappings, equip-spell auras, and effect base points for $spellIDM1 refs.
	enchantOfSpell := map[int]int{} // spellID -> SpellItemEnchantment.ID
	spellAuras := map[int][]rawAura{}
	spellRefPts := map[int]map[int]float64{} // spellID -> 1-based effect index -> abs(EffectBasePointsF)
	cEDiff := spellEffect.Col("DifficultyID")
	cESpell := spellEffect.Col("SpellID")
	cEEff := spellEffect.Col("Effect")
	cEAura := spellEffect.Col("EffectAura")
	cEIdx := spellEffect.Col("EffectIndex")
	cEBP := spellEffect.Col("EffectBasePointsF")
	cEMisc0 := spellEffect.Col("EffectMiscValue_0")

	for i := 0; i < spellEffect.Len(); i++ {
		r := spellEffect.Row(i)
		if cEDiff >= 0 && r.IntAt(cEDiff) != 0 {
			continue
		}
		sid := r.IntAt(cESpell)
		eff := r.IntAt(cEEff)
		if eff == effEnchantItem {
			if eid := r.IntAt(cEMisc0); eid > 0 {
				enchantOfSpell[sid] = eid
			}
		}
		if equipSpellNeed[sid] && (eff == effApplyAura || eff == effApplyAreaAuraParty) {
			spellAuras[sid] = append(spellAuras[sid], rawAura{
				aura: r.IntAt(cEAura),
				bp:   r.FloatAt(cEBP),
				m0:   r.IntAt(cEMisc0),
			})
		}
		if refSpellNeed[sid] {
			idx := r.IntAt(cEIdx) + 1
			if spellRefPts[sid] == nil {
				spellRefPts[sid] = map[int]float64{}
			}
			bp := r.FloatAt(cEBP)
			if bp < 0 {
				bp = -bp
			}
			spellRefPts[sid][idx] = bp
		}
	}

	// Decode every SpellItemEnchantment row.
	for i := 0; i < sie.Len(); i++ {
		r := sie.Row(i)
		id := r.IntAt(cID)
		if id <= 0 {
			continue
		}
		pts := [3]int{r.IntAt(cPts[0]), r.IntAt(cPts[1]), r.IntAt(cPts[2])}
		name := formatEnchantName(r.StrAt(cName), pts, spellRefPts)
		e := &Enchant{
			ID:                id,
			Name:              name,
			Duration:          r.IntAt(cDur),
			MinLevel:          r.IntAt(cMinLv),
			ItemLevelMin:      r.IntAt(cIlvlMin),
			RequiredSkillID:   r.IntAt(cReqSkill),
			RequiredSkillRank: r.IntAt(cReqRank),
		}
		stats := map[string]float64{}
		for j := 0; j < 3; j++ {
			et := r.IntAt(cEff[j])
			if et == 0 {
				continue
			}
			pt := float64(pts[j])
			arg := r.IntAt(cArg[j])
			switch et {
			case enchTypeCombatSpell:
				e.IsProc = true
			case enchTypeDamage:
				if pt != 0 {
					stats["weaponDmg"] += pt
				}
			case enchTypeEquipSpell:
				for _, a := range spellAuras[arg] {
					decodeAura(stats, &e.IsProc, &e.HasNonSheetMods, &e.HasUnmodeled, a.aura, a.bp, a.m0)
				}
			case enchTypeResistance:
				if arg == 0 && pt != 0 { // 0 = Physical resistance = Armor
					stats["armor"] += pt
				}
			case enchTypeStat:
				decodeItemMod(stats, arg, pt)
			case enchTypeUseSpell:
				e.HasUnmodeled = true
			}
		}
		if len(stats) > 0 {
			e.Stats = cleanStats(stats)
		}
		cat.ByID[id] = e
	}

	// Read SpellName (localized + enUS for QA/Test filtering).
	namesLoc := map[int]string{}
	if spellName != nil {
		cNID, cNV := spellName.Col("ID"), spellName.Col("Name_lang")
		for i := 0; i < spellName.Len(); i++ {
			r := spellName.Row(i)
			sid := r.IntAt(cNID)
			if _, ok := enchantOfSpell[sid]; ok {
				namesLoc[sid] = r.StrAt(cNV)
			}
		}
	}
	namesEN := map[int]string{}
	if spellNameEN != nil {
		cNID, cNV := spellNameEN.Col("ID"), spellNameEN.Col("Name_lang")
		for i := 0; i < spellNameEN.Len(); i++ {
			r := spellNameEN.Row(i)
			sid := r.IntAt(cNID)
			if _, ok := enchantOfSpell[sid]; ok {
				namesEN[sid] = r.StrAt(cNV)
			}
		}
	}

	// Read SpellEquippedItems for ENCHANT_ITEM spells.
	eqReqOf := map[int]equipReq{}
	if spellEquipped != nil {
		cSp := spellEquipped.Col("SpellID")
		cCl := spellEquipped.Col("EquippedItemClass")
		cInv := spellEquipped.Col("EquippedItemInvTypes")
		cSub := spellEquipped.Col("EquippedItemSubclass")
		for i := 0; i < spellEquipped.Len(); i++ {
			r := spellEquipped.Row(i)
			sid := r.IntAt(cSp)
			if _, ok := enchantOfSpell[sid]; ok {
				eqReqOf[sid] = equipReq{
					class:   r.IntAt(cCl),
					invMask: r.Uint32At(cInv),
					subMask: r.Uint32At(cSub),
				}
			}
		}
	}

	// Read SpellReagents for ENCHANT_ITEM spells.
	reagentsOf := map[int][]Reagent{}
	if spellReagents != nil {
		cSp := spellReagents.Col("SpellID")
		var cR, cC [8]int
		for j := 0; j < 8; j++ {
			cR[j] = spellReagents.Col(fmt.Sprintf("Reagent_%d", j))
			cC[j] = spellReagents.Col(fmt.Sprintf("ReagentCount_%d", j))
		}
		for i := 0; i < spellReagents.Len(); i++ {
			r := spellReagents.Row(i)
			sid := r.IntAt(cSp)
			if _, ok := enchantOfSpell[sid]; !ok {
				continue
			}
			var list []Reagent
			for j := 0; j < 8; j++ {
				rid := int64(r.IntAt(cR[j]))
				cnt := r.IntAt(cC[j])
				if rid > 0 && cnt > 0 {
					list = append(list, Reagent{ItemID: rid, Count: cnt})
				}
			}
			if len(list) > 0 {
				reagentsOf[sid] = list
			}
		}
	}

	// Read ItemSparse for item names, vendor buy prices, requirements, and allowable classes.
	type sparseInfo struct {
		name           string
		quality        int
		reqLevel       int
		reqSkill       int
		reqSkillRank   int
		allowableClass int64
		buyUnit        int64
	}
	sparseOf := map[int64]sparseInfo{}
	if itemSparse != nil {
		cIID := itemSparse.Col("ID")
		cDisp := itemSparse.Col("Display_lang")
		cQual := itemSparse.Col("OverallQualityID")
		cReqLv := itemSparse.Col("RequiredLevel")
		cReqSk := itemSparse.Col("RequiredSkill")
		cReqRk := itemSparse.Col("RequiredSkillRank")
		cCls := itemSparse.Col("AllowableClass")
		cBuy := itemSparse.Col("BuyPrice")
		cStk := itemSparse.Col("VendorStackCount")
		for i := 0; i < itemSparse.Len(); i++ {
			r := itemSparse.Row(i)
			iid := int64(r.IntAt(cIID))
			if iid <= 0 {
				continue
			}
			name := r.StrAt(cDisp)
			if name != "" {
				cat.ItemNames[iid] = name
			}
			buy := int64(r.IntAt(cBuy))
			stk := int64(r.IntAt(cStk))
			if stk < 1 {
				stk = 1
			}
			if buy > 0 {
				cat.ItemBuy[iid] = buy / stk
			}
			sparseOf[iid] = sparseInfo{
				name:           name,
				quality:        r.IntAt(cQual),
				reqLevel:       r.IntAt(cReqLv),
				reqSkill:       r.IntAt(cReqSk),
				reqSkillRank:   r.IntAt(cReqRk),
				allowableClass: int64(r.IntAt(cCls)),
				buyUnit:        buy / stk,
			}
		}
	}

	// Source 1: Enchanting profession recipes (SkillLine == 333).
	seenSpell := map[int]bool{}
	if skillLineAbility != nil {
		cSL := skillLineAbility.Col("SkillLine")
		cSp := skillLineAbility.Col("Spell")
		cLow := skillLineAbility.Col("TrivialSkillLineRankLow")
		for i := 0; i < skillLineAbility.Len(); i++ {
			r := skillLineAbility.Row(i)
			if r.IntAt(cSL) != skillEnchanting {
				continue
			}
			sid := r.IntAt(cSp)
			eid := enchantOfSpell[sid]
			if eid <= 0 || seenSpell[sid] {
				continue
			}
			ench := cat.ByID[eid]
			if ench == nil || ench.Duration > 0 {
				continue
			}
			enName := namesEN[sid]
			locName := namesLoc[sid]
			if locName == "" {
				locName = enName
			}
			if isJunkEnchant(enName, locName, ench.Name) {
				continue
			}
			seenSpell[sid] = true
			req := eqReqOf[sid]
			slots, twoHand, ranged, shield, holdable := resolveTarget(req, enName)
			if len(slots) == 0 {
				continue
			}
			opt := &Option{
				ID:           fmt.Sprintf("spell:%d", sid),
				EnchantID:    eid,
				Enchant:      ench,
				Source:       "enchanting",
				SpellID:      sid,
				SpellName:    locName,
				ReqLevel:     ench.MinLevel,
				ReqSkill:     skillEnchanting,
				ReqSkillRank: r.IntAt(cLow),
				Reagents:     reagentsOf[sid],
				EquipClass:   req.class,
				EquipInvMask: req.invMask,
				EquipSubMask: req.subMask,
				Slots:        slots,
				TwoHandOnly:  twoHand,
				RangedOnly:   ranged,
				ShieldOnly:   shield,
				HoldableOnly: holdable,
			}
			cat.addOption(opt)
		}
	}

	// Source 2: Item-applied permanent enchants (Armor Kits, Scopes, ZG/Libram/Shoulder enchants, etc.).
	if itemEffect != nil && itemXEffect != nil {
		ieSpell := map[int]int{}
		cIEID := itemEffect.Col("ID")
		cIESp := itemEffect.Col("SpellID")
		for i := 0; i < itemEffect.Len(); i++ {
			r := itemEffect.Row(i)
			sid := r.IntAt(cIESp)
			if enchantOfSpell[sid] > 0 {
				ieSpell[r.IntAt(cIEID)] = sid
			}
		}
		cXItem := itemXEffect.Col("ItemID")
		cXEff := itemXEffect.Col("ItemEffectID")
		seenItem := map[int64]bool{}
		for i := 0; i < itemXEffect.Len(); i++ {
			r := itemXEffect.Row(i)
			sid := ieSpell[r.IntAt(cXEff)]
			if sid <= 0 {
				continue
			}
			iid := int64(r.IntAt(cXItem))
			if iid <= 0 || seenItem[iid] {
				continue
			}
			eid := enchantOfSpell[sid]
			ench := cat.ByID[eid]
			if ench == nil || ench.Duration > 0 {
				continue
			}
			spInfo := sparseOf[iid]
			enName := namesEN[sid]
			locName := namesLoc[sid]
			if locName == "" {
				locName = enName
			}
			itemName := spInfo.name
			if itemName == "" {
				itemName = locName
			}
			if isJunkEnchant(enName, itemName, ench.Name) {
				continue
			}
			// Skip unobtainable QA/developer items without an ItemSparse entry unless SpellName is valid and non-QA.
			if spInfo.name == "" && locName == "" {
				continue
			}
			req := eqReqOf[sid]
			slots, twoHand, ranged, shield, holdable := resolveTarget(req, enName)
			if len(slots) == 0 {
				continue
			}
			seenItem[iid] = true
			reqLv := spInfo.reqLevel
			if ench.MinLevel > reqLv {
				reqLv = ench.MinLevel
			}
			opt := &Option{
				ID:             fmt.Sprintf("item:%d", iid),
				EnchantID:      eid,
				Enchant:        ench,
				Source:         "item",
				SpellID:        sid,
				SpellName:      locName,
				ItemID:         iid,
				ItemName:       itemName,
				Quality:        spInfo.quality,
				ReqLevel:       reqLv,
				ReqSkill:       spInfo.reqSkill,
				ReqSkillRank:   spInfo.reqSkillRank,
				AllowableClass: spInfo.allowableClass,
				EquipClass:     req.class,
				EquipInvMask:   req.invMask,
				EquipSubMask:   req.subMask,
				Slots:          slots,
				TwoHandOnly:    twoHand,
				RangedOnly:     ranged,
				ShieldOnly:     shield,
				HoldableOnly:   holdable,
			}
			cat.addOption(opt)
		}
	}

	sort.Slice(cat.Options, func(i, j int) bool { return cat.Options[i].ID < cat.Options[j].ID })
	for slot := range cat.BySlot {
		list := cat.BySlot[slot]
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	}
	return cat
}

func (c *Catalog) addOption(opt *Option) {
	c.Options = append(c.Options, opt)
	for _, s := range opt.Slots {
		c.BySlot[s] = append(c.BySlot[s], opt)
	}
}

func formatEnchantName(name string, pts [3]int, refPts map[int]map[int]float64) string {
	if !strings.Contains(name, "$") {
		return name
	}
	out := name
	for i := 0; i < 3; i++ {
		tok := fmt.Sprintf("$k%d", i+1)
		if strings.Contains(out, tok) {
			out = strings.ReplaceAll(out, tok, strconv.Itoa(pts[i]))
		}
		tokI := fmt.Sprintf("$i%d", i+1)
		if strings.Contains(out, tokI) {
			out = strings.ReplaceAll(out, tokI, strconv.Itoa(pts[i]))
		}
	}
	if strings.Contains(out, "$") && len(refPts) > 0 {
		out = spellRefRe.ReplaceAllStringFunc(out, func(m string) string {
			parts := spellRefRe.FindStringSubmatch(m)
			if len(parts) != 3 {
				return m
			}
			sid, _ := strconv.Atoi(parts[1])
			idx, _ := strconv.Atoi(parts[2])
			if v, ok := refPts[sid][idx]; ok {
				return strconv.FormatFloat(v, 'f', -1, 64)
			}
			return m
		})
	}
	return out
}

func isJunkEnchant(names ...string) bool {
	for _, n := range names {
		low := strings.ToLower(n)
		if strings.HasPrefix(low, "qa") || strings.Contains(low, "deprecated") ||
			strings.Contains(low, "test ") || strings.Contains(low, " test") ||
			strings.HasPrefix(low, "test") || strings.Contains(low, "unused") ||
			strings.Contains(low, "zzold") || strings.Contains(low, "monster") ||
			strings.Contains(low, "placeholder") || strings.Contains(low, "[ph]") ||
			strings.Contains(low, "dnt") {
			return true
		}
	}
	return false
}

// resolveTarget maps SpellEquippedItems masks (and fallback English prefix) into equipment slot IDs
// and item subtype restrictions (2H weapon, ranged weapon, shield, held-in-off-hand).
func resolveTarget(req equipReq, enName string) (slots []int64, twoHandOnly, rangedOnly, shieldOnly, holdableOnly bool) {
	switch req.class {
	case 2: // Weapon
		has1H := req.subMask&subMask1H != 0
		has2H := req.subMask&subMask2H != 0
		hasRanged := req.subMask&subMaskRanged != 0
		if hasRanged && !has1H && !has2H {
			return []int64{18}, false, true, false, false
		}
		if has2H && !has1H {
			return []int64{16}, true, false, false, false
		}
		if has1H || has2H {
			return []int64{16, 17}, false, false, false, false
		}
	case 4: // Armor
		inv := req.invMask
		if inv == 0 && req.subMask&subMaskShield != 0 {
			return []int64{17}, false, false, true, false
		}
		var out []int64
		add := func(s int64) {
			for _, x := range out {
				if x == s {
					return
				}
			}
			out = append(out, s)
		}
		if inv&(1<<1) != 0 {
			add(1) // Head
		}
		if inv&(1<<2) != 0 {
			add(2) // Neck
		}
		if inv&(1<<3) != 0 {
			add(3) // Shoulder
		}
		if inv&((1<<5)|(1<<20)) != 0 {
			add(5) // Chest / Robe
		}
		if inv&(1<<6) != 0 {
			add(6) // Waist
		}
		if inv&(1<<7) != 0 {
			add(7) // Legs
		}
		if inv&(1<<8) != 0 {
			add(8) // Feet
		}
		if inv&(1<<9) != 0 {
			add(9) // Wrist
		}
		if inv&(1<<10) != 0 {
			add(10) // Hands
		}
		if inv&(1<<11) != 0 {
			add(11)
			add(12)
		}
		if inv&(1<<14) != 0 {
			add(17)
			if inv == (1 << 14) {
				shieldOnly = true
			}
		}
		if inv&(1<<16) != 0 {
			add(15) // Back / Cloak
		}
		if inv&(1<<23) != 0 {
			add(17)
			if inv == (1 << 23) {
				holdableOnly = true
			}
		}
		if len(out) > 0 {
			return out, false, false, shieldOnly, holdableOnly
		}
	}

	// Fallback by English spell name prefix when SpellEquippedItems is absent (e.g. minimal test tables).
	low := strings.ToLower(enName)
	switch {
	case strings.HasPrefix(low, "enchant 2h weapon"):
		return []int64{16}, true, false, false, false
	case strings.HasPrefix(low, "enchant weapon"):
		return []int64{16, 17}, false, false, false, false
	case strings.HasPrefix(low, "enchant shield"):
		return []int64{17}, false, false, true, false
	case strings.HasPrefix(low, "enchant off-hand"):
		return []int64{17}, false, false, false, true
	case strings.HasPrefix(low, "enchant bracer"):
		return []int64{9}, false, false, false, false
	case strings.HasPrefix(low, "enchant chest"):
		return []int64{5}, false, false, false, false
	case strings.HasPrefix(low, "enchant cloak"):
		return []int64{15}, false, false, false, false
	case strings.HasPrefix(low, "enchant gloves"):
		return []int64{10}, false, false, false, false
	case strings.HasPrefix(low, "enchant boots"):
		return []int64{8}, false, false, false, false
	case strings.HasPrefix(low, "enchant necklace"):
		return []int64{2}, false, false, false, false
	}
	return nil, false, false, false, false
}

// MatchesItem reports whether an enchant Option can be applied to the item equipped in slot.
// equipLoc is the item's INVTYPE_* string; classID/subclassID are its numeric DB2 IDs (0 when unknown).
func MatchesItem(opt *Option, slot int64, equipLoc string, classID, subclassID int64) bool {
	if opt == nil {
		return false
	}
	slotOK := false
	for _, s := range opt.Slots {
		if s == slot {
			slotOK = true
			break
		}
	}
	if !slotOK {
		return false
	}
	if opt.TwoHandOnly {
		return equipLoc == "INVTYPE_2HWEAPON"
	}
	if opt.ShieldOnly {
		return equipLoc == "INVTYPE_SHIELD" || (classID == 4 && subclassID == 6)
	}
	if opt.HoldableOnly {
		return equipLoc == "INVTYPE_HOLDABLE"
	}
	if opt.RangedOnly {
		// Scopes apply to Bows (2), Guns (3), Crossbows (18) — never to Wands (19), Thrown (16), or Relics.
		if equipLoc == "INVTYPE_RELIC" || equipLoc == "INVTYPE_THROWN" {
			return false
		}
		if classID == 2 && subclassID > 0 {
			return (uint32(1)<<uint(subclassID))&subMaskRanged != 0
		}
		return equipLoc == "INVTYPE_RANGED" || equipLoc == "INVTYPE_RANGEDRIGHT"
	}
	// Weapon enchants on slot 17 only apply to actual off-hand weapons (not shields or holdables).
	if slot == 17 && opt.EquipClass == 2 {
		if equipLoc == "INVTYPE_SHIELD" || equipLoc == "INVTYPE_HOLDABLE" {
			return false
		}
	}
	// If SpellEquippedItems specifies a weapon subclass mask and the item has class/subclass info, check it.
	if opt.EquipClass == 2 && opt.EquipSubMask != 0 && classID == 2 && subclassID >= 0 && subclassID < 32 {
		if opt.EquipSubMask&(uint32(1)<<uint(subclassID)) == 0 {
			return false
		}
	}
	// If SpellEquippedItems specifies an armor subclass mask (e.g. Leather/Mail/Plate armor kits exclude Cloth?
	// Note: Classic Armor Kits have subMask=30 = Cloth(1)+Leather(2)+Mail(3)+Plate(4), excluding Misc(0)).
	if opt.EquipClass == 4 && opt.EquipSubMask != 0 && classID == 4 && subclassID >= 0 && subclassID < 32 {
		if opt.EquipSubMask&(uint32(1)<<uint(subclassID)) == 0 {
			return false
		}
	}
	return true
}

// StatsForSlot adapts an enchant's canonical stats to the slot it sits on:
// flat weapon damage on slot 17 (off-hand weapon) maps to offhandDmg, and flat damage/hit/crit on
// slot 18 (ranged weapon) maps to rangedDmg/rangedHit/rangedCrit.
func StatsForSlot(stats map[string]float64, slot int64) map[string]float64 {
	if len(stats) == 0 {
		return nil
	}
	out := make(map[string]float64, len(stats))
	for k, v := range stats {
		if v == 0 {
			continue
		}
		switch {
		case slot == 17 && k == "weaponDmg":
			out["offhandDmg"] += v
		case slot == 18 && k == "weaponDmg":
			out["rangedDmg"] += v
		case slot == 18 && k == "meleeHit":
			out["rangedHit"] += v
		case slot == 18 && k == "meleeCrit":
			out["rangedCrit"] += v
		default:
			out[k] += v
		}
	}
	return cleanStats(out)
}

func decodeItemMod(stats map[string]float64, mod int, pts float64) {
	if pts == 0 {
		return
	}
	switch mod {
	case 0: // ITEM_MOD_MANA
		stats["mana"] += pts
	case 1: // ITEM_MOD_HEALTH
		stats["health"] += pts
	case 3: // ITEM_MOD_AGILITY
		stats["agi"] += pts
	case 4: // ITEM_MOD_STRENGTH
		stats["str"] += pts
	case 5: // ITEM_MOD_INTELLECT
		stats["int"] += pts
	case 6: // ITEM_MOD_SPIRIT
		stats["spi"] += pts
	case 7: // ITEM_MOD_STAMINA
		stats["sta"] += pts
	case 12: // ITEM_MOD_DEFENSE_SKILL_RATING
		stats["defense"] += pts
	case 13: // ITEM_MOD_DODGE_RATING
		stats["dodge"] += pts
	case 14: // ITEM_MOD_PARRY_RATING
		stats["parry"] += pts
	case 15: // ITEM_MOD_BLOCK_RATING
		stats["block"] += pts
	case 16: // ITEM_MOD_HIT_MELEE_RATING
		stats["meleeHit"] += pts / hitRatingPerPct
	case 17: // ITEM_MOD_HIT_RANGED_RATING
		stats["rangedHit"] += pts / hitRatingPerPct
	case 18: // ITEM_MOD_HIT_SPELL_RATING
		stats["spellHit"] += pts / hitRatingPerPct
	case 19: // ITEM_MOD_CRIT_MELEE_RATING
		stats["meleeCrit"] += pts / critRatingPerPct
	case 20: // ITEM_MOD_CRIT_RANGED_RATING
		stats["rangedCrit"] += pts / critRatingPerPct
	case 21: // ITEM_MOD_CRIT_SPELL_RATING
		stats["spellCrit"] += pts / critRatingPerPct
	case 31: // ITEM_MOD_HIT_RATING
		stats["hit"] += pts / hitRatingPerPct
	case 32: // ITEM_MOD_CRIT_RATING
		stats["crit"] += pts / critRatingPerPct
	case 36: // ITEM_MOD_HASTE_RATING
		stats["haste"] += pts
	case 38: // ITEM_MOD_ATTACK_POWER
		stats["ap"] += pts
	case 39: // ITEM_MOD_RANGED_ATTACK_POWER
		stats["rap"] += pts
	case 40: // ITEM_MOD_FERAL_ATTACK_POWER
		stats["feralAp"] += pts
	case 41: // ITEM_MOD_SPELL_HEALING_DONE
		stats["healing"] += pts
	case 42: // ITEM_MOD_SPELL_DAMAGE_DONE
		stats["spDamage"] += pts
	case 43: // ITEM_MOD_MANA_REGENERATION
		stats["mp5"] += pts
	case 45: // ITEM_MOD_SPELL_POWER (damage and healing)
		stats["sp"] += pts
	case 46: // ITEM_MOD_HEALTH_REGEN
		stats["hp5"] += pts
	case 48: // ITEM_MOD_BLOCK_VALUE
		stats["blockValue"] += pts
	case 84: // Forever: Holy Damage
		stats["spHoly"] += pts
	case 85: // Forever: Fire Damage
		stats["spFire"] += pts
	case 86: // Forever: Nature Damage
		stats["spNature"] += pts
	case 87: // Forever: Frost Damage
		stats["spFrost"] += pts
	case 88: // Forever: Shadow Damage
		stats["spShadow"] += pts
	case 89: // Forever: Arcane Damage
		stats["spArcane"] += pts
	}
}

func decodeAura(stats map[string]float64, isProc, hasNonSheet, hasUnmodeled *bool, aura int, bp float64, m0 int) {
	switch aura {
	case auraModDamageDone:
		if m0 == 126 {
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
	case auraModHealth:
		stats["health"] += bp
	case auraModPower:
		if m0 == 0 {
			stats["mana"] += bp
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
		if m0 == 0 {
			stats["mp5"] += bp
		}
	case auraModHealthRegen:
		stats["hp5"] += bp
	case auraModBonusArmor:
		stats["armor"] += bp
	case auraModResistance:
		if m0 == 1 {
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
	case auraProcTriggerSpell, auraProcTriggerDamage:
		*isProc = true
	case auraModCostSchoolPct, auraModDamagePctDone, auraAddFlatModifier, auraAddPctModifier, auraModManaRegenCast:
		*hasNonSheet = true
	default:
		switch aura {
		case 4, 10, 14, 15, 31, 58, 77, 123, 129, 130, 154, 232:
			// utility (speed, mount speed, threat, stealth, disarm immunity, resistances)
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

// ScaleStats converts canonical engine enchant stat keys into weight-scale stat keys.
func ScaleStats(stats map[string]float64, physical bool, topSchool string) map[string]float64 {
	if len(stats) == 0 {
		return nil
	}
	out := map[string]float64{}
	for k, v := range stats {
		switch k {
		case "sp":
			out["sp"] += v
			out["sp_all"] += v
			out["healing"] += v
			out["sp_holy"] += v
			out["sp_fire"] += v
			out["sp_nature"] += v
			out["sp_frost"] += v
			out["sp_shadow"] += v
			out["sp_arcane"] += v
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
		case "ap", "meleeAp":
			out["ap"] += v
		case "rap":
			out["rap"] += v
		case "mp5":
			out["mp5"] += v
		case "hp5":
			out["hp5"] += v
		case "health":
			out["stamina"] += v / 10.0
		case "mana":
			out["intellect"] += v / 15.0
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
		case "weaponDmg":
			out["weapon_dps"] += v / 2.5
		case "rangedDmg":
			out["ranged_dps"] += v / 2.8
		case "hit":
			out["hit"] += v
		case "meleeHit", "rangedHit":
			if physical {
				out["hit"] += v
			}
		case "spellHit":
			if !physical {
				out["hit"] += v
			}
		case "crit":
			out["crit"] += v
		case "meleeCrit", "rangedCrit":
			if physical {
				out["crit"] += v
			}
		case "spellCrit":
			if !physical {
				out["crit"] += v
			}
		case "spellCritHoly":
			if !physical && topSchool == "holy" {
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
	return cleanStats(out)
}
