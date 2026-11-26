package upgrades

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/itemsets"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
)

// Virtual BiS slot ID for Two-Hand weapons (kept distinct from 1H Main Hand slot 16
// so Top 10 2H and Top 10 1H lists do not crowd each other out).
const bisSlotTwoHand int64 = 160

// bisSlotOrder is the display order of BiS slots.
var bisSlotOrder = []int64{
	1, 2, 3, 15, 5, 9, 10, 6, 7, 8, 11, 13, 16, 17, bisSlotTwoHand, 18,
}

// BiSOptions configures ComputeBiS.
type BiSOptions struct {
	LevelMode      string   // "current" (default) or "60"
	Preview        int64    // extra levels above current in "current" mode
	Top            int      // items per slot (default 10)
	Sources        []string // active source categories (default: quest, dungeon, raid, crafted)
	Profile        string
	Engine         string // "off", "on", "auto"
	FightSeconds   float64
	TargetDelta    int
	KillsPerMinute float64
	Metric         string
	Method         string
	Iterations     int
	Consumables    *Consumed
	FightBuffs     FightBuffsFunc
	SetCatalog     *itemsets.Catalog
	// Catalog optionally supplies a preloaded BiSCatalog (for tests).
	Catalog *BiSCatalog
}

// ParseSources splits a comma-separated source filter list into valid category keys.
func ParseSources(s string) []string {
	if strings.TrimSpace(s) == "" {
		return append([]string(nil), DefaultBiSSources...)
	}
	valid := map[string]bool{}
	for _, k := range AllSourceCategories {
		valid[k] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(s, ",") {
		k := strings.ToLower(strings.TrimSpace(part))
		if valid[k] && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), DefaultBiSSources...)
	}
	return out
}

func bisSlotLabel(slot int64, classFile string) string {
	switch slot {
	case 11:
		return i18n.T("Finger")
	case 13:
		return i18n.T("Trinket")
	case 16:
		return i18n.T("Main Hand (1H)")
	case 17:
		return i18n.T("Off Hand")
	case bisSlotTwoHand:
		return i18n.T("Two-Hand")
	default:
		return SlotLabelFor(slot, classFile)
	}
}

func completedQuestSet(c *sv.Table) map[int64]bool {
	out := map[int64]bool{}
	tab := c.Tab("completedQuests")
	if tab == nil {
		return out
	}
	if tab.IsList() {
		for _, v := range tab.List() {
			if id, ok := intKey(v); ok && id > 0 {
				out[id] = true
			}
		}
		return out
	}
	for i, k := range tab.Keys() {
		if sv.Truthy(tab.Values()[i]) {
			if id, ok := intKey(k); ok && id > 0 {
				out[id] = true
			}
		}
	}
	return out
}

// skillRanksByID maps WoW SkillLine ID -> rank from the character's skills table (English & German names).
var skillNameToID = map[string]int64{
	"blacksmithing": 164, "schmiedekunst": 164,
	"leatherworking": 165, "lederverarbeitung": 165,
	"alchemy": 171, "alchemie": 171,
	"herbalism": 182, "kräuterkunde": 182,
	"cooking": 185, "kochkunst": 185,
	"tailoring": 197, "schneiderei": 197,
	"engineering": 202, "ingenieurskunst": 202,
	"enchanting": 333, "verzauberkunst": 333,
}

func charSkillsByID(c *sv.Table) map[int64]int64 {
	out := map[int64]int64{}
	for _, s := range c.Tab("skills").Tab("skills").Tables() {
		name := strings.ToLower(strings.TrimSpace(s.Str("name")))
		rank, _ := s.Num("rank")
		if id, ok := skillNameToID[name]; ok && rank > 0 {
			out[id] = int64(rank)
		}
	}
	return out
}

func factionKey(ch *sv.Table) string {
	switch strings.ToLower(strings.TrimSpace(ch.Str("faction"))) {
	case "alliance", "allianz":
		return "alliance"
	case "horde":
		return "horde"
	}
	return ""
}

func allowsClass(classes []string, classFile string) bool {
	if len(classes) == 0 || classFile == "" {
		return true
	}
	for _, c := range classes {
		if strings.EqualFold(c, classFile) {
			return true
		}
	}
	return false
}

// ComputeBiS ranks the top N best-in-slot items per equipment slot for the character,
// filtered by item source categories (quest, dungeon, raid, crafted, ...) and level mode
// ("current" or "60"), evaluated with the same combat model / weight scale as AH upgrades.
func ComputeBiS(wow, char string, prices *sv.Table, o BiSOptions) (*pyjson.Obj, error) {
	if o.Top <= 0 {
		o.Top = 10
	}
	if o.LevelMode != "60" {
		o.LevelMode = "current"
	}
	if o.Engine == "" {
		o.Engine = "auto"
	}
	activeSources := o.Sources
	if len(activeSources) == 0 {
		activeSources = append([]string(nil), DefaultBiSSources...)
	}
	srcAllowed := map[string]bool{}
	for _, s := range activeSources {
		srcAllowed[s] = true
	}

	files := game.FindChars(wow, char)
	if len(files) == 0 {
		return nil, errors.New(i18n.T("No WoWToolsForever.lua found for this character."))
	}
	c, err := LoadChar(files[0])
	if err != nil {
		return nil, err
	}
	if c == nil {
		c = sv.NewTable()
	}
	ch := c.Tab("character")
	levelOut := orValue(ch, "level", int64(60))
	charLevel, _ := sv.Num(levelOut)
	if charLevel <= 0 {
		charLevel = 60
	}
	targetLevel := charLevel + float64(o.Preview)
	profLevel := charLevel
	if o.LevelMode == "60" {
		targetLevel = 60
		profLevel = 60
	}
	if targetLevel > 60 {
		targetLevel = 60
	}

	classFile := ch.Str("classFile")
	faction := factionKey(ch)
	completed := completedQuestSet(c)
	charSkills := charSkillsByID(c)
	lang := i18n.Language()

	// Optional in-game item export & AH prices (neither is required for BiS!).
	exportedItems := map[int64]*sv.Table{}
	var exportedList []*sv.Table
	if info := ItemExport(wow, c); info != nil {
		itTab := info.Tab("items")
		for i, k := range itTab.Keys() {
			if id, ok := intKey(k); ok {
				if t, ok := itTab.Values()[i].(*sv.Table); ok {
					exportedItems[id] = t
					exportedList = append(exportedList, t)
				}
			}
		}
	}
	if prices == nil && wow != "" {
		if p, err := LoadPrices(wow); err == nil {
			prices = p
		}
	}
	ahPriceOf := func(iid int64) int64 {
		if prices == nil {
			return 0
		}
		e, ok := isDict(prices.Get(strconv.FormatInt(iid, 10)))
		if !ok || !sv.Truthy(e.Get("m")) {
			return 0
		}
		m, _ := sv.Num(e.Get("m"))
		return int64(m)
	}

	equipped := map[int64]*sv.Table{}
	equippedIDs := map[int64]bool{}
	for _, e := range c.Tab("equipment").Tables() {
		if s, ok := sv.Num(e.Get("slot")); ok && s > 0 {
			equipped[int64(s)] = e
			if id, ok := e.Num("id"); ok && id > 0 {
				equippedIDs[int64(id)] = true
			}
		}
	}
	ownedMap := map[int64]Owned{}
	owned, _ := OwnedGear(files[0], c, exportedItems)
	for _, ow := range owned {
		if _, exists := ownedMap[ow.ID]; !exists {
			ownedMap[ow.ID] = ow
		}
	}

	ratings := LearnRatings(exportedList)
	eq, totals := EquippedStats(c, ratings)
	setCat := o.SetCatalog
	if setCat == nil {
		setCat = LoadSetCatalog(c, "")
	}
	enchCat := LoadEnchantCatalog(c, "")
	tracker := NewSetTracker(c, setCat)
	totalsWithSets := totals.Copy()
	totalsWithSets.Add(tracker.BaseScale, 1)
	scale := BuildScale(c, o.Profile, totalsWithSets["hit"], totalsWithSets["crit"])
	base := SetValue(totalsWithSets, scale)
	twoHanded := equipped[16].Str("equipLoc") == "INVTYPE_2HWEAPON"

	// Start combat engine (same logic as Compute).
	var eng *enginebundle.Engine
	spec := enginebundle.SpecOf(c)
	dw := canDualWield(classFile, profLevel, spec, ch.Tab("stats"))
	physical := isPhysicalRole(c)
	if o.Engine != "off" {
		uOpts := Options{
			Profile: o.Profile, Engine: o.Engine, FightSeconds: o.FightSeconds,
			TargetDelta: o.TargetDelta, KillsPerMinute: o.KillsPerMinute,
			Metric: o.Metric, Method: o.Method, Iterations: o.Iterations,
			Consumables: o.Consumables, FightBuffs: o.FightBuffs,
		}
		var consumed *Consumed
		eng, consumed, err = startEngine(c, classFile, totals, scale, uOpts)
		if consumed != nil {
			o.Consumables = consumed
		}
		var fatal *Fatal
		if err != nil {
			if o.Engine == "on" || errors.As(err, &fatal) {
				return nil, err
			}
			eng = nil
		}
	}

	pct := pctLines()
	normValue := func(rec *sv.Table, ranged bool) Stats { return NormStats(rec, ranged, ratings) }
	eqValue := eq
	if eng != nil {
		normValue = func(rec *sv.Table, ranged bool) Stats {
			return Stats(enginebundle.NormItem(rec, ranged, ratings, pct))
		}
		eqValue = map[int64]Stats{}
		for s, e := range equipped {
			if s != 0 {
				eqValue[s] = normValue(e, s == 18)
			}
		}
	}

	swap := func(remove []int64, adds []Stats, addIDs []int64) float64 {
		t := totalsWithSets.Copy()
		for _, s := range remove {
			t.Add(eq[s], -1)
		}
		for _, n := range adds {
			t.Add(n, 1)
		}
		if sd := tracker.ScaleDelta(remove, addIDs); len(sd) > 0 {
			t.Add(sd, 1)
		}
		return SetValue(t, scale) - base
	}
	valueBatch := func(evals []eval, reoptimize bool) (map[string]float64, error) {
		if eng != nil {
			list := make([]enginebundle.Eval, len(evals))
			for i, ev := range evals {
				adds := make([]map[string]float64, len(ev.adds))
				for j, a := range ev.adds {
					adds[j] = a
				}
				removes := make([]map[string]float64, len(ev.rm))
				for j, s := range ev.rm {
					removes[j] = eqValue[s]
				}
				delta := enginebundle.Delta(adds, removes)
				setDelta, setSpells, hasSetChange := tracker.EngineChange(ev.rm, ev.addIDs)
				if len(setDelta) > 0 {
					if delta == nil {
						delta = map[string]float64{}
					}
					for k, v := range setDelta {
						delta[k] += v
						if delta[k] == 0 {
							delete(delta, k)
						}
					}
				}
				list[i] = enginebundle.Eval{
					Key: ev.key, Delta: delta, NoMC: ev.noMC,
					HasSetChange: hasSetChange, SetBonusSpells: setSpells,
				}
			}
			return eng.Batch(list, reoptimize)
		}
		out := map[string]float64{}
		for _, ev := range evals {
			out[ev.key] = swap(ev.rm, ev.adds, ev.addIDs)
		}
		return out, nil
	}

	// Load BiS catalog (bundled + live ForeverMyBis + live AtlasLoot + DB2).
	cat := o.Catalog
	if cat == nil {
		build := c.Tab("client").Str("build")
		clientLocale := c.Tab("client").Str("locale")
		if clientLocale == "" {
			if lang == "de" {
				clientLocale = "deDE"
			} else {
				clientLocale = "enUS"
			}
		}
		cat = LoadBiSCatalog(wow, build, clientLocale)
	}

	usableWep := usableWeaponsFor(classFile)
	usableArm := usableArmorFor(classFile, profLevel)

	type bisCand struct {
		item       *BiSCatalogItem
		svTab      *sv.Table
		bisSlot    int64
		variants   [][]int64
		norm       Stats
		extra      []string
		sources    []BiSSource
		allSources []BiSSource
		soon       bool
		remove     []int64
		gain       float64
	}

	// Sort item IDs for deterministic ordering.
	allIDs := make([]int64, 0, len(cat.Items))
	for id := range cat.Items {
		allIDs = append(allIDs, id)
	}
	sort.Slice(allIDs, func(i, j int) bool { return allIDs[i] < allIDs[j] })

	sourceCounts := map[string]int{}
	for _, k := range AllSourceCategories {
		sourceCounts[k] = 0
	}

	var pool []*bisCand
	for _, id := range allIDs {
		it := cat.Items[id]
		if it == nil || it.Quality < 2 {
			continue
		}
		if it.ReqLvl > int64(targetLevel) {
			continue
		}
		if it.Faction != "" && faction != "" && it.Faction != faction {
			continue
		}
		if !allowsClass(it.Classes, classFile) {
			continue
		}
		eqLoc := it.EquipLoc()
		slots := SlotOf[eqLoc]
		if len(slots) == 0 {
			continue
		}
		if it.ClassID == 2 && !usableWep[it.SubID] {
			continue
		}
		if it.ClassID == 4 && !usableArm[it.SubID] {
			continue
		}

		// Filter sources by character faction.
		var validSrcs []BiSSource
		itemKinds := map[string]bool{}
		for _, s := range it.Sources {
			if s.Faction != "" && faction != "" && s.Faction != faction {
				continue
			}
			validSrcs = append(validSrcs, s)
			itemKinds[s.Kind] = true
		}
		if len(validSrcs) == 0 {
			continue
		}
		// Count available items per source category BEFORE applying the user's source filter.
		for k := range itemKinds {
			sourceCounts[k]++
		}

		// Apply user's selected source category filter.
		var matchedSrcs []BiSSource
		for _, s := range validSrcs {
			if srcAllowed[s.Kind] {
				matchedSrcs = append(matchedSrcs, s)
			}
		}
		if len(matchedSrcs) == 0 {
			continue
		}

		svTab := it.ToSVTable(exportedItems[id], lang)
		tip := tipLines(svTab)
		ranged := len(slots) == 1 && slots[0] == 18
		norm := normValue(svTab, ranged)
		extra := SpecialEffectsFor(tip, physical)

		// Skip items with zero stats AND no proc/special effects (e.g. cosmetic/unfinished DB2 stubs).
		if len(norm) == 0 && len(extra) == 0 && !it.HasProc && len(it.Procs) == 0 {
			continue
		}

		addCand := func(bSlot int64, variants [][]int64) {
			pool = append(pool, &bisCand{
				item:       it,
				svTab:      svTab,
				bisSlot:    bSlot,
				variants:   variants,
				norm:       norm,
				extra:      extra,
				sources:    matchedSrcs,
				allSources: validSrcs,
				soon:       o.LevelMode == "current" && float64(it.ReqLvl) > charLevel,
			})
		}

		switch {
		case eqLoc == "INVTYPE_2HWEAPON":
			addCand(bisSlotTwoHand, [][]int64{{16, 17}})
		case eqLoc == "INVTYPE_FINGER":
			if it.Unique || isUnique(tip) {
				id0, _ := equipped[11].Num("id")
				id1, _ := equipped[12].Num("id")
				if int64(id0) == id {
					addCand(11, [][]int64{{11}})
				} else if int64(id1) == id {
					addCand(11, [][]int64{{12}})
				} else {
					addCand(11, [][]int64{{11}, {12}})
				}
			} else {
				addCand(11, [][]int64{{11}, {12}})
			}
		case eqLoc == "INVTYPE_TRINKET":
			if it.Unique || isUnique(tip) {
				id0, _ := equipped[13].Num("id")
				id1, _ := equipped[14].Num("id")
				if int64(id0) == id {
					addCand(13, [][]int64{{13}})
				} else if int64(id1) == id {
					addCand(13, [][]int64{{14}})
				} else {
					addCand(13, [][]int64{{13}, {14}})
				}
			} else {
				addCand(13, [][]int64{{13}, {14}})
			}
		case eqLoc == "INVTYPE_WEAPON":
			// 1H weapon fits Main Hand; if dual-wielding, also evaluate for Off Hand.
			addCand(16, [][]int64{{16}})
			if dw {
				addCand(17, [][]int64{{17}})
			}
		case len(slots) == 1 && slots[0] == 17:
			// Compare Off Hand against slot 17 (its own slot contribution even when wearing a 2H).
			addCand(17, [][]int64{{17}})
		default:
			addCand(slots[0], [][]int64{{slots[0]}})
		}
	}

	// Phase 1: fast valuation of all candidates against baseline rotation (or weight scale).
	var evals []eval
	for i, x := range pool {
		for j, rm := range x.variants {
			evals = append(evals, eval{
				key:    fmt.Sprintf("sw:%d:%d", i, j),
				adds:   []Stats{x.norm},
				addIDs: []int64{x.item.ID},
				rm:     rm,
				noMC:   eng != nil,
			})
		}
	}
	removes := map[string][]int64{}
	for s := range SlotName {
		removes[joinSlots([]int64{s})] = []int64{s}
	}
	removes["16+17"] = []int64{16, 17}
	for _, x := range pool {
		for _, rm := range x.variants {
			removes[joinSlots(rm)] = rm
		}
	}
	var rmList [][]int64
	for _, rm := range removes {
		rmList = append(rmList, rm)
	}
	sort.Slice(rmList, func(a, b int) bool { return lessSlots(rmList[a], rmList[b]) })
	for _, rm := range rmList {
		evals = append(evals, eval{key: "rm:" + joinSlots(rm), rm: rm, noMC: true})
	}
	values, err := valueBatch(evals, false)
	if err != nil {
		return nil, err
	}

	// Phase 2 (engine only): refine top candidates per BiS slot with rotation re-optimization (and MC).
	if eng != nil && len(pool) > 0 {
		bySlot := map[int64][]int{}
		for i, x := range pool {
			bySlot[x.bisSlot] = append(bySlot[x.bisSlot], i)
		}
		var refine []eval
		picked := map[int]bool{}
		refineLimit := o.Top + 6
		if refineLimit < 16 {
			refineLimit = 16
		}
		for _, idxs := range bySlot {
			sort.SliceStable(idxs, func(a, b int) bool {
				ga, gb := bestBiSSwapVal(values, idxs[a], pool[idxs[a]].variants), bestBiSSwapVal(values, idxs[b], pool[idxs[b]].variants)
				if ga != gb {
					return ga > gb
				}
				return pool[idxs[a]].item.ItemLvl > pool[idxs[b]].item.ItemLvl
			})
			for k, idx := range idxs {
				x := pool[idx]
				setAffected := false
				if tracker.HasSetData() {
					for _, rm := range x.variants {
						if len(tracker.Changes(rm, []int64{x.item.ID})) > 0 {
							setAffected = true
							break
						}
					}
				}
				if k < refineLimit || equippedIDs[x.item.ID] || setAffected {
					if !picked[idx] {
						picked[idx] = true
						for j, rm := range x.variants {
							refine = append(refine, eval{
								key:    fmt.Sprintf("sw:%d:%d", idx, j),
								adds:   []Stats{x.norm},
								addIDs: []int64{x.item.ID},
								rm:     rm,
							})
						}
					}
				}
			}
		}
		if len(refine) > 0 {
			rvals, err := valueBatch(refine, true)
			if err != nil {
				return nil, err
			}
			for k, v := range rvals {
				values[k] = v
			}
		}
	}

	oldOf := func(remove []int64) float64 { return -values["rm:"+joinSlots(remove)] }

	// Pick best variant per candidate and group by BiS slot.
	slotBuckets := map[int64][]*bisCand{}
	for i, x := range pool {
		best := 0
		for j := range x.variants {
			if values[fmt.Sprintf("sw:%d:%d", i, j)] > values[fmt.Sprintf("sw:%d:%d", i, best)] {
				best = j
			}
		}
		x.remove = x.variants[best]
		x.gain = values[fmt.Sprintf("sw:%d:%d", i, best)]
		slotBuckets[x.bisSlot] = append(slotBuckets[x.bisSlot], x)
	}

	buildSourceObjs := func(srcs []BiSSource, ownedOrEq bool) []any {
		out := make([]any, 0, len(srcs))
		for _, s := range srcs {
			obj := pyjson.New(
				"kind", s.Kind,
				"kindLabel", SourceKindLabel(s.Kind),
				"label", cat.FormatSourceLabel(s, lang),
			)
			if s.Place != "" {
				obj.Set("place", cat.LocalizePlace(s.Place, lang))
			}
			if s.QuestID > 0 {
				obj.Set("questId", s.QuestID)
				if completed[s.QuestID] && !ownedOrEq {
					obj.Set("questDone", true)
				}
			}
			if s.Skill > 0 {
				obj.Set("skill", skillLocalized(s.Skill, lang))
				if s.SkillRank > 0 {
					obj.Set("skillRank", s.SkillRank)
				}
				if rank, ok := charSkills[s.Skill]; ok && rank >= s.SkillRank {
					obj.Set("canCraft", true)
				}
			}
			out = append(out, obj)
		}
		return out
	}

	buildCandObj := func(x *bisCand, rank int) *pyjson.Obj {
		it := x.item
		old := oldOf(x.remove)
		var gainPct any
		if old > 0.5 {
			gainPct = pyfmt.RoundInt(x.gain / old * 100)
		}
		isEq := equippedIDs[it.ID]
		ow, isOwned := ownedMap[it.ID]
		where := any(nil)
		if isEq {
			where = i18n.T("equipped")
		} else if isOwned {
			where = WhereText(ow.Source, ow.Owner)
		}
		var price any
		if it.Bind == "boe" && !isEq && !isOwned {
			if p := ahPriceOf(it.ID); p > 0 {
				price = p
			}
		}
		effects := make([]any, 0, len(x.extra)+len(it.Procs))
		for _, e := range x.extra {
			effects = append(effects, e)
		}
		if len(effects) == 0 {
			for _, pr := range it.Procs {
				effects = append(effects, pr)
			}
		}
		slotTitle := bisSlotLabel(x.bisSlot, classFile)
		obj := pyjson.New(
			"rank", rank,
			"id", it.ID,
			"name", it.DisplayName(lang),
			"quality", it.Quality,
			"ilvl", it.ItemLvl,
			"reqLevel", it.ReqLvl,
			"gain", pyfmt.Round(x.gain, 1),
			"gainPct", gainPct,
			"stats", Short(x.svTab.Tab("stats")),
			"isNew", it.ID >= newID,
			"soon", x.soon,
			"unique", it.Unique,
			"twoHand", x.bisSlot == bisSlotTwoHand,
			"bind", strOrNil(it.Bind),
			"equipped", isEq,
			"owned", isOwned,
			"where", where,
			"price", price,
			"sources", buildSourceObjs(x.sources, isEq || isOwned),
			"effects", effects,
			"hasProc", it.HasProc || len(x.extra) > 0,
			"tip", Tooltip(x.svTab, &slotTitle),
		)
		if sc := tracker.Changes(x.remove, []int64{it.ID}); len(sc) > 0 {
			obj.Set("setChanges", SetChangesJSON(sc))
		}
		var lostEnchants []any
		for _, s := range x.remove {
			if ench := EquippedEnchantJSON(equipped[s], enchCat); ench != nil {
				lostEnchants = append(lostEnchants, ench)
			}
		}
		if len(lostEnchants) > 0 {
			obj.Set("replacesEnchants", lostEnchants)
		}
		return obj
	}

	curItemObj := func(slot int64) *pyjson.Obj {
		cur := equipped[slot]
		if !sv.Truthy(cur) {
			return nil
		}
		label := SlotLabelFor(slot, classFile)
		obj := pyjson.New("name", cur.Get("name"), "id", cur.Get("id"), "quality", cur.Get("quality"),
			"stats", Short(cur.Tab("stats")), "tip", Tooltip(cur, &label))
		if ench := EquippedEnchantJSON(cur, enchCat); ench != nil {
			obj.Set("enchant", ench)
		}
		return obj
	}

	slotsOut := []any{}
	for _, bSlot := range bisSlotOrder {
		list := slotBuckets[bSlot]
		if len(list) == 0 {
			continue
		}
		sort.SliceStable(list, func(a, b int) bool {
			if list[a].gain != list[b].gain {
				return list[a].gain > list[b].gain
			}
			if list[a].item.ItemLvl != list[b].item.ItemLvl {
				return list[a].item.ItemLvl > list[b].item.ItemLvl
			}
			if list[a].item.Quality != list[b].item.Quality {
				return list[a].item.Quality > list[b].item.Quality
			}
			return list[a].item.ID < list[b].item.ID
		})
		topList := sliceTop(list, o.Top)
		inTop := map[int64]bool{}
		cands := make([]any, len(topList))
		for i, x := range topList {
			inTop[x.item.ID] = true
			cands[i] = buildCandObj(x, i+1)
		}

		// Collect notable unmodeled Proc / On-Use items for this slot that didn't land in Top N
		// (crucial for trinkets & proc weapons like Talisman of Ephemeral Power or Hand of Justice).
		var specials []*bisCand
		for _, x := range list {
			if inTop[x.item.ID] {
				continue
			}
			if (x.item.HasProc || len(x.extra) > 0) && float64(x.item.ReqLvl) >= targetLevel-15 {
				specials = append(specials, x)
			}
		}
		sort.SliceStable(specials, func(a, b int) bool {
			if specials[a].item.ItemLvl != specials[b].item.ItemLvl {
				return specials[a].item.ItemLvl > specials[b].item.ItemLvl
			}
			return specials[a].gain > specials[b].gain
		})
		specials = sliceTop(specials, 6)
		specOut := make([]any, len(specials))
		for i, x := range specials {
			specOut[i] = buildCandObj(x, 0)
		}

		// Current equipped item(s) for this BiS slot.
		var current any
		var currentSecond any
		switch bSlot {
		case 11:
			current = curItemObj(11)
			currentSecond = curItemObj(12)
		case 13:
			current = curItemObj(13)
			currentSecond = curItemObj(14)
		case bisSlotTwoHand:
			current = curItemObj(16)
			if !twoHanded {
				currentSecond = curItemObj(17)
			}
		default:
			current = curItemObj(bSlot)
		}

		slotObj := pyjson.New(
			"slot", bSlot,
			"slotName", bisSlotLabel(bSlot, classFile),
			"current", current,
			"candidates", cands,
			"totalCandidates", len(list),
		)
		if currentSecond != nil {
			slotObj.Set("currentSecond", currentSecond)
		}
		if len(specOut) > 0 {
			slotObj.Set("specials", specOut)
		}
		slotsOut = append(slotsOut, slotObj)
	}

	baseValue := base
	if eng != nil {
		baseValue = 0
		if eng.Baseline != nil {
			baseValue = eng.Baseline.Value
		}
	}
	weightsText := scale.Text
	scaleRows := make([]any, len(scale.Rows))
	for i, r := range scale.Rows {
		scaleRows[i] = r
	}
	var engineOut any
	if eng != nil {
		weightsText, scaleRows, engineOut = engineSection(eng, c, classFile, Options{
			Profile: o.Profile, Engine: o.Engine, FightSeconds: o.FightSeconds,
			TargetDelta: o.TargetDelta, KillsPerMinute: o.KillsPerMinute,
			Metric: o.Metric, Method: o.Method, Iterations: o.Iterations,
			Consumables: o.Consumables,
		}, tracker)
	}

	srcCountsObj := pyjson.New()
	for _, k := range AllSourceCategories {
		srcCountsObj.Set(k, sourceCounts[k])
	}
	activeSrcList := make([]any, len(activeSources))
	for i, s := range activeSources {
		activeSrcList[i] = s
	}
	dataSrcList := make([]any, len(cat.DataSources))
	for i, s := range cat.DataSources {
		dataSrcList[i] = s
	}

	res := pyjson.New(
		"character", filepath.Base(filepath.Dir(filepath.Dir(files[0]))),
		"level", levelOut,
		"targetLevel", int64(targetLevel),
		"levelMode", o.LevelMode,
		"faction", strOrNil(faction),
		"classFile", classFile,
		"top", o.Top,
		"sources", activeSrcList,
		"sourceCounts", srcCountsObj,
		"dataSources", dataSrcList,
		"catalogItems", len(cat.Items),
		"weights", weightsText,
		"profile", scale.Profile,
		"profileLabel", scale.Label,
		"scale", scaleRows,
		"setValue", pyfmt.Round(baseValue, 1),
		"engine", engineOut,
		"slots", slotsOut,
	)
	if activeSets := SummariesJSON(tracker.ActiveSummaries()); len(activeSets) > 0 {
		res.Set("activeSets", activeSets)
	}
	return res, nil
}

func bestBiSSwapVal(values map[string]float64, idx int, variants [][]int64) float64 {
	best := values[fmt.Sprintf("sw:%d:0", idx)]
	for j := 1; j < len(variants); j++ {
		if v := values[fmt.Sprintf("sw:%d:%d", idx, j)]; v > best {
			best = v
		}
	}
	return best
}
