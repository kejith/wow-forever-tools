// Package enchants evaluates permanent enchantments per equipped slot for a character using
// the combat engine (with weight-scale fallback), calculating net metric gains over the
// currently applied enchant, reagent/item costs from Auctionator + vendor prices, gain per gold,
// and flagging proc/special enchants (e.g. Crusader, Fiery Weapon, Minor Speed).
package enchants

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"

	"github.com/kejith/wow-tools-forever/sim/enchants"
	"github.com/kejith/wow-tools-forever/sim/engine"
	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
	"github.com/kejith/wow-tools-forever/sim/tools/marketsync"
	"github.com/kejith/wow-tools-forever/sim/tools/recipes"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

type reagentPrice struct {
	id      int64
	name    string
	count   int
	unit    int64
	total   int64
	missing bool
}

type candRow struct {
	slot      int64
	opt       *enchants.Option
	evalKey   string
	netDelta  map[string]float64
	gain      float64
	cost      *int64
	reagents  []reagentPrice
	owned     int64
	canCraft  bool
	isCurrent bool
	isBest    bool
	isBestVal bool
}

// Options configures enchant recommendations for a character.
type Options struct {
	Top            int
	Engine         string  // "auto" (default), "on", "off"
	FightSeconds   float64 // default 60
	TargetDelta    int     // -1 = automatic (+2/+3)
	KillsPerMinute float64 // -1 = automatic
	Metric         string  // "" = spec default
	Profile        string  // weight-scale fallback profile
	Catalog        *enchants.Catalog
	Prices         *sv.Table
}

// paperdollOrder is the display order of enchantable slots.
var paperdollOrder = []int64{1, 2, 3, 15, 5, 9, 10, 7, 8, 16, 17, 18}

// classBit maps WoW classFile to the AllowableClass bitmask in ItemSparse.
var classBit = map[string]int64{
	"WARRIOR": 1 << 0,
	"PALADIN": 1 << 1,
	"HUNTER":  1 << 2,
	"ROGUE":   1 << 3,
	"PRIEST":  1 << 4,
	"SHAMAN":  1 << 6,
	"MAGE":    1 << 7,
	"WARLOCK": 1 << 8,
	"DRUID":   1 << 10,
}

func classAllowed(mask int64, classFile string) bool {
	if mask <= 0 {
		return true
	}
	bit := classBit[classFile]
	if bit == 0 {
		return true
	}
	return mask&bit != 0
}

// Compute evaluates permanent enchant upgrades across all equipped enchantable slots.
func Compute(wow, char string, o Options) (*pyjson.Obj, error) {
	if o.Top <= 0 {
		o.Top = 5
	}
	if o.Engine == "" {
		o.Engine = "auto"
	}
	if o.FightSeconds <= 0 {
		o.FightSeconds = 60
	}

	files := game.FindChars(wow, char)
	if len(files) == 0 {
		return nil, errors.New(i18n.T("No WoWToolsForever.lua found for this character."))
	}
	c, err := upgrades.LoadChar(files[0])
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New(i18n.T("No WoWToolsForever.lua found for this character."))
	}

	cat := o.Catalog
	if cat == nil {
		cat = upgrades.LoadEnchantCatalog(c, "")
	}
	prices := o.Prices
	if prices == nil {
		prices, _, _ = marketsync.LoadPrices(wow)
	}
	vendor := loadVendor(wow)
	var mkt *recipes.Market
	if prices != nil {
		mkt = recipes.NewMarket(prices, vendor, nil)
	}

	ch := c.Tab("character")
	classFile := ch.Str("classFile")
	level := 60
	if lv, ok := ch.Num("level"); ok && lv > 0 {
		level = int(lv)
	}
	skills := upgrades.SkillRanks(c)
	learnedSpells := learnedRecipes(c)
	ownedItems := ownedCounts(c)

	eqStats, totals := upgrades.EquippedStats(c, nil)
	_ = eqStats
	scale := upgrades.BuildScale(c, o.Profile, totals["hit"], totals["crit"])
	physical := isPhysical(c)
	topSchool := topSchoolOf(c)

	// Start combat engine when requested.
	var eng *enginebundle.Engine
	metric := o.Metric
	if metric == "" {
		metric = enginebundle.DefaultMetric(classFile, enginebundle.SpecOf(c))
	}
	if o.Engine != "off" && enginebundle.Supported(classFile) {
		st := ch.Tab("stats")
		lifeTap := engine.LifeTap{}
		if m := upgrades.LifeTapText.FindStringSubmatch(st.Tab("lifeTap").Str("text")); m != nil && upgrades.Number(m[1]) > 0 {
			lifeTap = engine.LifeTap{HealthPerCast: upgrades.Number(m[1]), ManaPerCast: upgrades.Number(m[2])}
		}
		fight := engine.Fight{Seconds: o.FightSeconds, TargetDelta: o.TargetDelta, KillsPerMinute: -1}
		if o.TargetDelta < 0 || o.TargetDelta > 3 {
			fight.TargetDelta = upgrades.Profiles[scale.Profile].Target
		}
		if o.KillsPerMinute >= 0 {
			fight.KillsPerMinute = o.KillsPerMinute
		}
		var errEng error
		eng, errEng = enginebundle.Start(c, totals["mp5"], totals["hp5"], fight, metric, lifeTap, enginebundle.Options{})
		if errEng != nil {
			if o.Engine == "on" {
				return nil, errEng
			}
			eng = nil
		}
	}

	equipped := map[int64]*sv.Table{}
	for _, e := range c.Tab("equipment").Tables() {
		if s, ok := sv.Num(e.Get("slot")); ok {
			equipped[int64(s)] = e
		}
	}

	type slotPlan struct {
		slot     int64
		item     *sv.Table
		curID    int
		curEnch  *enchants.Enchant
		curStats map[string]float64
		cands    []*candRow
		specials []*candRow
	}

	priceItem := func(iid int64) (int64, bool) {
		var best int64
		hasBest := false
		if mkt != nil {
			if a := mkt.AH(iid); a != nil && a.PriceF() > 0 {
				best, hasBest = int64(math.Round(a.PriceF())), true
			}
			if vp, _ := mkt.VendorPrice(iid); vp > 0 {
				v := int64(math.Round(vp))
				if !hasBest || v < best {
					best, hasBest = v, true
				}
			}
		}
		if !hasBest && recipes.VendorItems[iid] {
			if b := cat.ItemBuy[iid]; b > 0 {
				best, hasBest = b, true
			}
		}
		return best, hasBest
	}

	priceOption := func(opt *enchants.Option) (cost *int64, regs []reagentPrice, owned int64) {
		if opt.Source == "item" {
			owned = ownedItems[opt.ItemID]
			if owned > 0 {
				z := int64(0)
				return &z, nil, owned
			}
			if p, ok := priceItem(opt.ItemID); ok {
				return &p, nil, 0
			}
			return nil, nil, 0
		}
		if len(opt.Reagents) == 0 {
			return nil, nil, 0
		}
		var sum int64
		complete := true
		for _, r := range opt.Reagents {
			name := cat.ItemNames[r.ItemID]
			if name == "" {
				name = fmt.Sprintf("#%d", r.ItemID)
			}
			unit, ok := priceItem(r.ItemID)
			rp := reagentPrice{id: r.ItemID, name: name, count: r.Count, unit: unit, missing: !ok}
			if ok {
				rp.total = unit * int64(r.Count)
				sum += rp.total
			} else {
				complete = false
			}
			regs = append(regs, rp)
		}
		if complete {
			return &sum, regs, 0
		}
		return nil, regs, 0
	}

	var plans []*slotPlan
	var batchEvals []enginebundle.Eval

	for _, slot := range paperdollOrder {
		eq := equipped[slot]
		if eq == nil {
			continue
		}
		opts := cat.BySlot[slot]
		if len(opts) == 0 {
			continue
		}
		equipLoc := eq.Str("equipLoc")
		classID, _ := eq.Num("classID")
		subclassID, _ := eq.Num("subclassID")
		ilvl, _ := eq.Num("ilvl")

		curID := enchants.ParseEnchantID(eq)
		var curEnch *enchants.Enchant
		var curStats map[string]float64
		if curID > 0 {
			curEnch = cat.ByID[curID]
			if curEnch != nil {
				curStats = enchants.StatsForSlot(curEnch.Stats, slot)
			}
		}

		// Deduplicate by EnchantID per slot, preferring Enchanting recipes or cheaper options.
		bestByEnch := map[int]*candRow{}
		var orderIDs []int
		for _, opt := range opts {
			if !enchants.MatchesItem(opt, slot, equipLoc, int64(classID), int64(subclassID)) {
				continue
			}
			if opt.ReqLevel > level {
				continue
			}
			if opt.Enchant.ItemLevelMin > 0 && ilvl > 0 && int(ilvl) < opt.Enchant.ItemLevelMin {
				continue
			}
			if !classAllowed(opt.AllowableClass, classFile) {
				continue
			}
			if opt.Source == "item" && opt.ReqSkill > 0 && skills[opt.ReqSkill] < opt.ReqSkillRank {
				continue
			}
			cost, regs, owned := priceOption(opt)
			canCraft := opt.Source == "enchanting" && learnedSpells[opt.SpellID]
			row := &candRow{
				slot:      slot,
				opt:       opt,
				cost:      cost,
				reagents:  regs,
				owned:     owned,
				canCraft:  canCraft,
				isCurrent: curID > 0 && opt.EnchantID == curID,
			}
			prev, exists := bestByEnch[opt.EnchantID]
			if !exists {
				bestByEnch[opt.EnchantID] = row
				orderIDs = append(orderIDs, opt.EnchantID)
			} else if preferOption(row, prev) {
				bestByEnch[opt.EnchantID] = row
			}
		}
		if len(orderIDs) == 0 {
			continue
		}

		sp := &slotPlan{slot: slot, item: eq, curID: curID, curEnch: curEnch, curStats: curStats}
		for _, eid := range orderIDs {
			row := bestByEnch[eid]
			ench := row.opt.Enchant
			optStats := enchants.StatsForSlot(ench.Stats, slot)
			if len(optStats) == 0 {
				if ench.IsProc || ench.HasUnmodeled {
					sp.specials = append(sp.specials, row)
				}
				continue
			}
			if row.isCurrent {
				row.gain = 0
			} else {
				delta := subtractStats(optStats, curStats)
				row.netDelta = delta
				if len(delta) > 0 {
					row.evalKey = fmt.Sprintf("e:%d:%d", slot, eid)
					if eng != nil {
						batchEvals = append(batchEvals, enginebundle.Eval{Key: row.evalKey, Delta: delta})
					}
				}
			}
			sp.cands = append(sp.cands, row)
			if ench.IsProc {
				sp.specials = append(sp.specials, row)
			}
		}
		if len(sp.cands) > 0 || len(sp.specials) > 0 || curID > 0 {
			plans = append(plans, sp)
		}
	}

	// Evaluate all candidate deltas in one engine batch (or weight-scale fallback).
	var gains map[string]float64
	if eng != nil && len(batchEvals) > 0 {
		var errB error
		gains, errB = eng.Batch(batchEvals, true)
		if errB != nil {
			return nil, errB
		}
	}

	baseScaleVal := upgrades.SetValue(totals, scale)
	rateScale := func(delta map[string]float64) float64 {
		t := totals.Copy()
		t.Add(upgrades.Stats(enchants.ScaleStats(delta, physical, topSchool)), 1)
		return upgrades.SetValue(t, scale) - baseScaleVal
	}

	combinedDelta := map[string]float64{}
	var totalCost int64
	unpricedCount := 0
	enchantedSlots := 0

	for _, sp := range plans {
		if sp.curID > 0 {
			enchantedSlots++
		}
		for _, r := range sp.cands {
			if r.isCurrent || len(r.netDelta) == 0 {
				continue
			}
			if eng != nil {
				r.gain = gains[r.evalKey]
			} else {
				r.gain = rateScale(r.netDelta)
			}
		}

		// Keep only meaningful upgrades (> 0.05) plus the currently equipped enchant (if present).
		var filtered []*candRow
		for _, r := range sp.cands {
			if r.gain > 0.05 || r.isCurrent {
				filtered = append(filtered, r)
			}
		}
		sort.SliceStable(filtered, func(i, j int) bool {
			if math.Abs(filtered[i].gain-filtered[j].gain) > 1e-6 {
				return filtered[i].gain > filtered[j].gain
			}
			ci, cj := costOrMax(filtered[i].cost), costOrMax(filtered[j].cost)
			if ci != cj {
				return ci < cj
			}
			return filtered[i].opt.ID < filtered[j].opt.ID
		})

		// Pick best overall and best value per gold among positive-gain options.
		var bestRow, bestValRow *candRow
		bestPerGold := -1.0
		for _, r := range filtered {
			if r.gain <= 0.05 {
				continue
			}
			if bestRow == nil {
				bestRow = r
			}
			if r.cost != nil && *r.cost > 0 {
				pg := r.gain / (float64(*r.cost) / 10000.0)
				if pg > bestPerGold {
					bestPerGold = pg
					bestValRow = r
				}
			} else if r.cost != nil && *r.cost == 0 && bestValRow == nil {
				bestValRow = r
				bestPerGold = math.Inf(1)
			}
		}
		if bestRow != nil {
			bestRow.isBest = true
			for k, v := range bestRow.netDelta {
				combinedDelta[k] += v
			}
			if bestRow.cost != nil {
				totalCost += *bestRow.cost
			} else {
				unpricedCount++
			}
		}
		if bestValRow != nil && bestValRow != bestRow {
			bestValRow.isBestVal = true
		}

		// Slice to top N while always keeping current and bestValue if they fell past top N.
		if len(filtered) > o.Top {
			kept := append([]*candRow(nil), filtered[:o.Top]...)
			has := func(target *candRow) bool {
				for _, x := range kept {
					if x == target {
						return true
					}
				}
				return false
			}
			if bestValRow != nil && !has(bestValRow) {
				kept = append(kept, bestValRow)
			}
			for _, r := range filtered[o.Top:] {
				if r.isCurrent && !has(r) {
					kept = append(kept, r)
				}
			}
			filtered = kept
		}
		sp.cands = filtered

		// Sort special/proc enchants by price then name, cap to 6.
		sort.SliceStable(sp.specials, func(i, j int) bool {
			ci, cj := costOrMax(sp.specials[i].cost), costOrMax(sp.specials[j].cost)
			if ci != cj {
				return ci < cj
			}
			return sp.specials[i].opt.SpellName < sp.specials[j].opt.SpellName
		})
		if len(sp.specials) > 6 {
			sp.specials = sp.specials[:6]
		}
	}

	// Evaluate combined best enchant upgrade set in one engine run (handles hit cap & mana interactions).
	totalGain := 0.0
	for k, v := range combinedDelta {
		if math.Abs(v) < 1e-9 {
			delete(combinedDelta, k)
		}
	}
	if len(combinedDelta) > 0 {
		if eng != nil {
			if res, errC := eng.Batch([]enginebundle.Eval{{Key: "all", Delta: combinedDelta}}, true); errC == nil {
				totalGain = res["all"]
			}
		} else {
			totalGain = rateScale(combinedDelta)
		}
	}

	baseValue := baseScaleVal
	unit := i18n.T("points")
	mode := "scale"
	if eng != nil && eng.Baseline != nil {
		baseValue = eng.Baseline.Value
		mode = "engine"
		switch metric {
		case "hps":
			unit = "HPS"
		case "ttoom":
			unit = "s"
		default:
			unit = "DPS"
		}
	}

	candJSON := func(r *candRow) *pyjson.Obj {
		name := r.opt.SpellName
		if r.opt.Source == "item" && r.opt.ItemName != "" {
			name = r.opt.ItemName
		}
		var costVal any
		var perGold any
		if r.cost != nil {
			costVal = *r.cost
			if *r.cost > 0 && r.gain > 0 {
				perGold = pyfmt.Round(r.gain/(float64(*r.cost)/10000.0), 1)
			}
		}
		regs := make([]any, len(r.reagents))
		for i, rg := range r.reagents {
			ro := pyjson.New("id", rg.id, "name", rg.name, "count", rg.count, "missing", rg.missing)
			if !rg.missing {
				ro.Set("unitPrice", rg.unit)
				ro.Set("totalPrice", rg.total)
			}
			regs[i] = ro
		}
		obj := pyjson.New(
			"id", r.opt.ID,
			"enchantId", r.opt.EnchantID,
			"name", name,
			"effect", r.opt.Enchant.Name,
			"source", r.opt.Source,
			"gain", pyfmt.Round(r.gain, 1),
			"cost", costVal,
			"perGold", perGold,
			"best", r.isBest,
			"bestValue", r.isBestVal,
			"current", r.isCurrent,
			"canCraft", r.canCraft,
			"owned", r.owned,
			"proc", r.opt.Enchant.IsProc,
		)
		if r.opt.SpellID > 0 {
			obj.Set("spellId", r.opt.SpellID)
		}
		if r.opt.ItemID > 0 {
			obj.Set("itemId", r.opt.ItemID)
			obj.Set("quality", r.opt.Quality)
		}
		if len(regs) > 0 {
			obj.Set("reagents", regs)
		}
		return obj
	}

	slotsOut := make([]any, 0, len(plans))
	for _, sp := range plans {
		cands := make([]any, len(sp.cands))
		var bestGain float64
		var bestCost any
		for i, r := range sp.cands {
			cands[i] = candJSON(r)
			if r.isBest {
				bestGain = r.gain
				if r.cost != nil {
					bestCost = *r.cost
				}
			}
		}
		specials := make([]any, len(sp.specials))
		for i, r := range sp.specials {
			specials[i] = candJSON(r)
		}
		label := upgrades.SlotLabelFor(sp.slot, classFile)
		itemObj := pyjson.New(
			"id", sp.item.Get("id"),
			"name", sp.item.Get("name"),
			"quality", sp.item.Get("quality"),
			"ilvl", sp.item.Get("ilvl"),
			"tip", upgrades.Tooltip(sp.item, &label),
		)
		var curObj any
		if sp.curID > 0 {
			curObj = upgrades.EquippedEnchantJSON(sp.item, cat)
		}
		slotsOut = append(slotsOut, pyjson.New(
			"slot", sp.slot,
			"slotName", label,
			"item", itemObj,
			"current", curObj,
			"bestGain", pyfmt.Round(bestGain, 1),
			"bestCost", bestCost,
			"candidates", cands,
			"specials", specials,
		))
	}

	out := pyjson.New(
		"character", filepath.Base(filepath.Dir(filepath.Dir(files[0]))),
		"level", level,
		"mode", mode,
		"metric", metric,
		"unit", unit,
		"baseValue", pyfmt.Round(baseValue, 1),
		"totalGain", pyfmt.Round(totalGain, 1),
		"totalValue", pyfmt.Round(baseValue+totalGain, 1),
		"totalCost", totalCost,
		"unpricedCount", unpricedCount,
		"enchantedSlots", enchantedSlots,
		"totalSlots", len(plans),
		"slots", slotsOut,
	)
	if eng != nil {
		out.Set("fightSeconds", eng.Fight.Seconds)
		out.Set("targetDelta", eng.Fight.TargetDelta)
		out.Set("metrics", enginebundle.Metrics[classFile])
	}
	if we := upgrades.WeaponEnchantsJSON(c, cat); len(we) > 0 {
		out.Set("weaponEnchants", we)
	}
	return out, nil
}

func loadVendor(wow string) *sv.Table {
	vendor := sv.NewTable()
	for _, path := range game.AccountFiles(wow, "WoWToolsForever.lua") {
		acct, err := sv.LoadTable(path, "WoWToolsForever_Account")
		if err != nil || acct == nil {
			continue
		}
		if v, ok := acct.Get("vendor").(*sv.Table); ok {
			for i, k := range v.Keys() {
				vendor.Set(k, v.Values()[i])
			}
		}
	}
	return vendor
}

func preferOption(a, b *candRow) bool {
	if a.owned > 0 && b.owned == 0 {
		return true
	}
	if a.opt.Source == "enchanting" && b.opt.Source != "enchanting" {
		return true
	}
	if a.opt.Source != "enchanting" && b.opt.Source == "enchanting" {
		return false
	}
	return costOrMax(a.cost) < costOrMax(b.cost)
}

func costOrMax(c *int64) int64 {
	if c == nil {
		return math.MaxInt64
	}
	return *c
}

func subtractStats(next, cur map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, v := range next {
		out[k] += v
	}
	for k, v := range cur {
		out[k] -= v
	}
	for k, v := range out {
		if math.Abs(v) < 1e-9 {
			delete(out, k)
		}
	}
	return out
}

func learnedRecipes(c *sv.Table) map[int]bool {
	out := map[int]bool{}
	if c == nil {
		return out
	}
	for _, r := range c.Tab("recipes").Tables() {
		if id, ok := r.Num("spellId"); ok && id > 0 {
			out[int(id)] = true
		} else if id, ok := r.Num("id"); ok && id > 0 {
			out[int(id)] = true
		}
	}
	return out
}

func ownedCounts(c *sv.Table) map[int64]int64 {
	out := map[int64]int64{}
	if c == nil {
		return out
	}
	scan := func(tab *sv.Table) {
		if tab == nil {
			return
		}
		for _, bag := range tab.Values() {
			bt, ok := bag.(*sv.Table)
			if !ok {
				continue
			}
			for _, it := range bt.Tab("items").Tables() {
				id, ok := it.Num("id")
				if !ok || id <= 0 {
					continue
				}
				cnt, _ := it.Num("count")
				if cnt <= 0 {
					cnt = 1
				}
				out[int64(id)] += int64(cnt)
			}
		}
	}
	scan(c.Tab("bags"))
	scan(c.Tab("bank"))
	return out
}

func isPhysical(c *sv.Table) bool {
	role := enginebundle.RoleForChar(c)
	return role == "melee" || role == "ranged" || role == "tank"
}

func topSchoolOf(c *sv.Table) string {
	ch := c.Tab("character")
	switch ch.Str("classFile") {
	case "WARLOCK", "PRIEST":
		if enginebundle.RoleForChar(c) == "healer" {
			return "holy"
		}
		return "shadow"
	case "MAGE":
		switch enginebundle.SpecOf(c) {
		case "fire", "frost", "arcane":
			return enginebundle.SpecOf(c)
		}
		return "frost"
	case "DRUID", "SHAMAN":
		return "nature"
	case "PALADIN":
		return "holy"
	}
	return ""
}
