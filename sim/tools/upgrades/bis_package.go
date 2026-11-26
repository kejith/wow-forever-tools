package upgrades

import (
	"fmt"
	"sort"

	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/itemsets"
)

type bisPick struct {
	x      *bisCand
	remove []int64
}

type bisOpt struct {
	gain  float64
	picks []bisPick
}

func bisGroupOf(bSlot int64) string {
	switch bSlot {
	case 11, 12:
		return "11,12"
	case 13, 14:
		return "13,14"
	case 16, 17, bisSlotTwoHand:
		return "weapon"
	default:
		return itoa(bSlot)
	}
}

func bisPickSlot(p bisPick) int64 {
	if len(p.remove) == 2 && p.remove[0] == 16 && p.remove[1] == 17 {
		return bisSlotTwoHand
	}
	if len(p.remove) == 1 {
		return p.remove[0]
	}
	return p.x.bisSlot
}

// allSourcesDoneQuest reports whether every matched source of x is an already-completed quest
// and the character neither equips nor owns the item (so it can no longer be obtained).
func allSourcesDoneQuest(x *bisCand, completed map[int64]bool, equippedIDs map[int64]bool, ownedMap map[int64]Owned) bool {
	if equippedIDs[x.item.ID] {
		return false
	}
	if _, owned := ownedMap[x.item.ID]; owned {
		return false
	}
	if len(x.sources) == 0 {
		return false
	}
	for _, s := range x.sources {
		if s.Kind != SrcQuest || s.QuestID <= 0 || !completed[s.QuestID] {
			return false
		}
	}
	return true
}

// bisSetBundles generates multi-piece (2pc / 3pc) set bonus bundles from usable BiS candidates
// so local search can jump directly into strong set bonuses (e.g. Bloodvine 3pc, Devilsaur 2pc, Tier sets).
func bisSetBundles(usable []*bisCand, tracker *SetTracker) [][]bisPick {
	if !tracker.HasSetData() {
		return nil
	}
	eqCounts := map[int]int{}
	for _, id := range tracker.EquippedIDs {
		if s := tracker.Cat.SetOf(id); s != nil {
			eqCounts[s.ID]++
		}
	}
	bySet := map[int][]*bisCand{}
	for _, x := range usable {
		s := tracker.Cat.SetOf(x.item.ID)
		if s == nil {
			continue
		}
		if s.RequiredSkill > 0 && tracker.Skills[s.RequiredSkill] < s.RequiredSkillRank {
			continue
		}
		bySet[s.ID] = append(bySet[s.ID], x)
	}
	if len(bySet) == 0 {
		return nil
	}
	setIDs := make([]int, 0, len(bySet))
	for sid := range bySet {
		setIDs = append(setIDs, sid)
	}
	sort.Ints(setIDs)

	type scoredBundle struct {
		score float64
		picks []bisPick
	}
	var scored []scoredBundle

	for _, sid := range setIDs {
		s := tracker.Cat.Sets[sid]
		if s == nil {
			continue
		}
		grpMap := map[string][]*bisCand{}
		for _, x := range bySet[sid] {
			g := bisGroupOf(x.bisSlot)
			grpMap[g] = append(grpMap[g], x)
		}
		if len(grpMap) < 2 {
			continue
		}
		grps := make([]string, 0, len(grpMap))
		for g := range grpMap {
			grps = append(grps, g)
		}
		sort.Strings(grps)
		bestPerGrp := make([]*bisCand, 0, len(grps))
		for _, g := range grps {
			cs := grpMap[g]
			best := cs[0]
			for _, x := range cs[1:] {
				if x.gain > best.gain {
					best = x
				}
			}
			bestPerGrp = append(bestPerGrp, best)
		}
		if len(bestPerGrp) < 2 {
			continue
		}
		if len(bestPerGrp) > 6 {
			sort.SliceStable(bestPerGrp, func(i, j int) bool { return bestPerGrp[i].gain > bestPerGrp[j].gain })
			bestPerGrp = bestPerGrp[:6]
		}
		curN := eqCounts[sid]
		wantThr := map[int]bool{}
		for _, b := range s.Bonuses {
			if b.Threshold > curN && (len(b.Stats) > 0 || b.HasNonSheetMods) {
				wantThr[b.Threshold] = true
			}
		}
		if len(wantThr) == 0 {
			continue
		}
		newCount := func(xs []*bisCand) int {
			rmSet := map[int64]bool{}
			for _, x := range xs {
				for _, r := range x.remove {
					rmSet[r] = true
				}
			}
			seen := map[int64]bool{}
			for slot, id := range tracker.EquippedIDs {
				if !rmSet[slot] && tracker.Cat.SetOf(id) == s {
					seen[id] = true
				}
			}
			for _, x := range xs {
				seen[x.item.ID] = true
			}
			return len(seen)
		}
		reaches := func(n int) bool {
			for thr := range wantThr {
				if n >= thr {
					return true
				}
			}
			return false
		}
		for i := 0; i < len(bestPerGrp); i++ {
			for j := i + 1; j < len(bestPerGrp); j++ {
				a, b := bestPerGrp[i], bestPerGrp[j]
				if a.item.ID == b.item.ID {
					continue
				}
				if reaches(newCount([]*bisCand{a, b})) {
					scored = append(scored, scoredBundle{
						score: a.gain + b.gain,
						picks: []bisPick{{a, a.remove}, {b, b.remove}},
					})
				}
			}
		}
		if len(bestPerGrp) >= 3 {
			need3 := false
			for thr := range wantThr {
				if thr-curN >= 3 {
					need3 = true
					break
				}
			}
			if need3 {
				for i := 0; i < len(bestPerGrp); i++ {
					for j := i + 1; j < len(bestPerGrp); j++ {
						for k := j + 1; k < len(bestPerGrp); k++ {
							a, b, c := bestPerGrp[i], bestPerGrp[j], bestPerGrp[k]
							if a.item.ID == b.item.ID || a.item.ID == c.item.ID || b.item.ID == c.item.ID {
								continue
							}
							if reaches(newCount([]*bisCand{a, b, c})) {
								scored = append(scored, scoredBundle{
									score: a.gain + b.gain + c.gain,
									picks: []bisPick{{a, a.remove}, {b, b.remove}, {c, c.remove}},
								})
							}
						}
					}
				}
			}
		}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	if len(scored) > 32 {
		scored = scored[:32]
	}
	out := make([][]bisPick, len(scored))
	for i, sb := range scored {
		out[i] = sb.picks
	}
	return out
}

// computeBiSPackage assembles the single best combination of BiS upgrades across all slots
// relative to the character's currently equipped gear (jointly optimizing ring/trinket pairs,
// 1H+OH vs. 2H weapons, hit cap / mana thresholds, and multi-piece set bonuses).
func computeBiSPackage(
	pool []*bisCand,
	equipped map[int64]*sv.Table,
	equippedIDs map[int64]bool,
	ownedMap map[int64]Owned,
	completed map[int64]bool,
	eq map[int64]Stats,
	tracker *SetTracker,
	baseValue float64,
	twoHanded, dw, hasEngine bool,
	classFile string,
	valueBatch func([]eval, bool) (map[string]float64, error),
	buildCandObj func(*bisCand, int) *pyjson.Obj,
) (*pyjson.Obj, error) {
	emptyPkg := func() *pyjson.Obj {
		return pyjson.New(
			"gain", 0.0,
			"gainPct", nil,
			"targetValue", pyfmt.Round(baseValue, 1),
			"ownedCount", 0,
			"items", []any{},
		)
	}

	// Eligible candidates: usable at target level (not soon), not already equipped,
	// and not exclusively gated behind an already-completed quest.
	var usable []*bisCand
	for _, x := range pool {
		if x.soon || equippedIDs[x.item.ID] || allSourcesDoneQuest(x, completed, equippedIDs, ownedMap) {
			continue
		}
		usable = append(usable, x)
	}
	if len(usable) == 0 {
		return emptyPkg(), nil
	}

	var groupKeys []string
	groups := map[string][]bisOpt{}
	addGroup := func(key string, opt bisOpt) {
		if _, ok := groups[key]; !ok {
			groupKeys = append(groupKeys, key)
		}
		groups[key] = append(groups[key], opt)
	}

	// Single-item options per group.
	for _, x := range usable {
		// When wearing a 2H weapon, an off-hand alone cannot be equipped without a 1H main hand
		// (off-hands still enter via 1H+OH pairs below).
		if twoHanded && x.bisSlot == 17 {
			continue
		}
		// Keep positive upgrades plus set-piece candidates (so 1-opt can still consider them).
		if x.gain > 0.05 || (tracker.HasSetData() && tracker.Cat.SetOf(x.item.ID) != nil) {
			addGroup(bisGroupOf(x.bisSlot), bisOpt{gain: x.gain, picks: []bisPick{{x, x.remove}}})
		}
	}

	// Paired combinations: rings (11+12), trinkets (13+14), and 1H + Off-Hand (16+17).
	type bisCombo struct {
		key    string
		x, y   *bisCand
		ra, rb []int64
	}
	topByGain := func(xs []*bisCand, n int) []*bisCand {
		cp := append([]*bisCand(nil), xs...)
		sort.SliceStable(cp, func(a, b int) bool {
			if cp[a].gain != cp[b].gain {
				return cp[a].gain > cp[b].gain
			}
			return cp[a].item.ItemLvl > cp[b].item.ItemLvl
		})
		return sliceTop(cp, n)
	}

	var combos []bisCombo
	for _, p := range [][2]int64{{11, 12}, {13, 14}} {
		var cands []*bisCand
		seen := map[int64]bool{}
		for _, x := range usable {
			if x.bisSlot == p[0] && !seen[x.item.ID] {
				seen[x.item.ID] = true
				cands = append(cands, x)
			}
		}
		cands = topByGain(cands, 10)
		for i, x := range cands {
			// Also allow two copies of the same non-unique ring/trinket!
			start := i + 1
			if !x.item.Unique {
				start = i
			}
			for _, y := range cands[start:] {
				combos = append(combos, bisCombo{bisGroupOf(p[0]), x, y, []int64{p[0]}, []int64{p[1]}})
			}
		}
	}

	var oneHand, offHand []*bisCand
	seenMH, seenOH := map[int64]bool{}, map[int64]bool{}
	for _, x := range usable {
		if x.bisSlot == 16 && !seenMH[x.item.ID] {
			seenMH[x.item.ID] = true
			oneHand = append(oneHand, x)
		}
		if x.bisSlot == 17 && !seenOH[x.item.ID] {
			seenOH[x.item.ID] = true
			offHand = append(offHand, x)
		}
	}
	oneHand, offHand = topByGain(oneHand, 10), topByGain(offHand, 10)
	for _, x := range oneHand {
		for _, y := range offHand {
			if x.item.ID == y.item.ID && x.item.Unique {
				continue
			}
			// Only allow 1H weapon in off-hand when dual-wielding (already enforced in pool, guard anyway).
			if y.item.EquipLoc() == "INVTYPE_WEAPON" && !dw {
				continue
			}
			combos = append(combos, bisCombo{"weapon", x, y, []int64{16}, []int64{17}})
		}
	}

	if len(combos) > 0 {
		cevals := make([]eval, len(combos))
		for i, cb := range combos {
			cevals[i] = eval{
				key:    fmt.Sprintf("bcb:%d", i),
				adds:   []Stats{cb.x.norm, cb.y.norm},
				addIDs: []int64{cb.x.item.ID, cb.y.item.ID},
				rm:     []int64{cb.ra[0], cb.rb[0]},
				noMC:   true,
			}
		}
		cvals, err := valueBatch(cevals, hasEngine)
		if err != nil {
			return nil, err
		}
		for i, cb := range combos {
			g := cvals[fmt.Sprintf("bcb:%d", i)]
			if g > 0.05 {
				addGroup(cb.key, bisOpt{gain: g, picks: []bisPick{{cb.x, cb.ra}, {cb.y, cb.rb}}})
			}
		}
	}

	if len(groupKeys) == 0 {
		return emptyPkg(), nil
	}
	sort.Strings(groupKeys)

	// Keep top 8 distinct options per group and pick the best positive option per group as starting point.
	topOpts := map[string][]bisOpt{}
	var picks []bisPick
	for _, g := range groupKeys {
		opts := append([]bisOpt(nil), groups[g]...)
		sort.SliceStable(opts, func(i, j int) bool { return opts[i].gain > opts[j].gain })
		if len(opts) > 8 {
			opts = opts[:8]
		}
		topOpts[g] = opts
		if len(opts) > 0 && opts[0].gain > 0.05 {
			picks = append(picks, opts[0].picks...)
		}
	}

	evalSet := func(ps []bisPick) ([]Stats, []int64, []int64) {
		var adds []Stats
		var addIDs []int64
		var emptied []int64
		for _, p := range ps {
			for _, s := range p.remove {
				if _, ok := eq[s]; ok && !containsSlot(emptied, s) {
					emptied = append(emptied, s)
				}
			}
			if twoHanded && len(p.remove) == 1 && p.remove[0] == 17 {
				if _, ok := eq[16]; ok && !containsSlot(emptied, 16) {
					emptied = append(emptied, 16)
				}
			}
			adds = append(adds, p.x.norm)
			addIDs = append(addIDs, p.x.item.ID)
		}
		return adds, addIDs, emptied
	}

	// Multi-round exact 1-opt + set-bundle local search over the complete combined gear set.
	bundles := bisSetBundles(usable, tracker)
	for range 3 {
		byGrp := map[string][]bisPick{}
		for _, p := range picks {
			g := bisGroupOf(p.x.bisSlot)
			byGrp[g] = append(byGrp[g], p)
		}
		baseAdds, baseIDs, baseRm := evalSet(picks)
		type move struct {
			key   string
			picks []bisPick
		}
		moves := []move{{key: "bopt:cur", picks: picks}}
		optEvals := []eval{{key: "bopt:cur", adds: baseAdds, addIDs: baseIDs, rm: baseRm, noMC: true}}

		for _, g := range groupKeys {
			var rest []bisPick
			for _, p := range picks {
				if bisGroupOf(p.x.bisSlot) != g {
					rest = append(rest, p)
				}
			}
			if len(byGrp[g]) > 0 {
				adds, addIDs, rm := evalSet(rest)
				mk := fmt.Sprintf("bopt:%s:drop", g)
				moves = append(moves, move{key: mk, picks: rest})
				optEvals = append(optEvals, eval{key: mk, adds: adds, addIDs: addIDs, rm: rm, noMC: true})
			}
			for oi, opt := range topOpts[g] {
				candPicks := append(append([]bisPick(nil), rest...), opt.picks...)
				adds, addIDs, rm := evalSet(candPicks)
				mk := fmt.Sprintf("bopt:%s:%d", g, oi)
				moves = append(moves, move{key: mk, picks: candPicks})
				optEvals = append(optEvals, eval{key: mk, adds: adds, addIDs: addIDs, rm: rm, noMC: true})
			}
		}

		for bi, bnd := range bundles {
			bGrps := make(map[string]bool, len(bnd))
			for _, p := range bnd {
				bGrps[bisGroupOf(p.x.bisSlot)] = true
			}
			var rest []bisPick
			for _, p := range picks {
				if !bGrps[bisGroupOf(p.x.bisSlot)] {
					rest = append(rest, p)
				}
			}
			candPicks := append(append([]bisPick(nil), rest...), bnd...)
			adds, addIDs, rm := evalSet(candPicks)
			mk := fmt.Sprintf("bopt:set:%d", bi)
			moves = append(moves, move{key: mk, picks: candPicks})
			optEvals = append(optEvals, eval{key: mk, adds: adds, addIDs: addIDs, rm: rm, noMC: true})
		}

		if len(optEvals) <= 1 {
			break
		}
		mvals, err := valueBatch(optEvals, hasEngine)
		if err != nil {
			return nil, err
		}
		bestIdx := 0
		bestVal := mvals["bopt:cur"]
		for i := 1; i < len(moves); i++ {
			v := mvals[moves[i].key]
			if v > bestVal+0.05 {
				bestIdx, bestVal = i, v
			}
		}
		if bestIdx == 0 {
			break
		}
		picks = moves[bestIdx].picks
	}

	// Prune units that contribute nothing on top of the rest (e.g. excess hit past cap).
	for range 4 {
		if len(picks) == 0 {
			break
		}
		var prefixAdds []Stats
		var prefixIDs []int64
		var emptied []int64
		var prefixEvals []eval
		for i, p := range picks {
			for _, s := range p.remove {
				if _, ok := eq[s]; ok && !containsSlot(emptied, s) {
					emptied = append(emptied, s)
				}
			}
			prefixAdds = append(prefixAdds, p.x.norm)
			prefixIDs = append(prefixIDs, p.x.item.ID)
			prefixEvals = append(prefixEvals, eval{
				key:    fmt.Sprintf("bpx:%d", i),
				adds:   append([]Stats(nil), prefixAdds...),
				addIDs: append([]int64(nil), prefixIDs...),
				rm:     append([]int64(nil), emptied...),
				noMC:   true,
			})
		}
		pvals, err := valueBatch(prefixEvals, hasEngine)
		if err != nil {
			return nil, err
		}
		unitGain := map[string]float64{}
		for i, p := range picks {
			prev := 0.0
			if i > 0 {
				prev = pvals[fmt.Sprintf("bpx:%d", i-1)]
			}
			unitGain[bisGroupOf(p.x.bisSlot)] += pvals[fmt.Sprintf("bpx:%d", i)] - prev
		}
		var keep []bisPick
		for _, p := range picks {
			if unitGain[bisGroupOf(p.x.bisSlot)] > 0.005 {
				keep = append(keep, p)
			}
		}
		if len(keep) == len(picks) {
			break
		}
		picks = keep
	}

	if len(picks) == 0 {
		return emptyPkg(), nil
	}

	// Sort into farming/upgrade priority order: owned items first (free instant upgrades),
	// then biggest gain first (putting MH before OH when a 1H+OH pair replaces a 2H weapon).
	isOwnedPick := func(p bisPick) bool {
		_, ok := ownedMap[p.x.item.ID]
		return ok
	}
	sort.SliceStable(picks, func(a, b int) bool {
		oa, ob := isOwnedPick(picks[a]), isOwnedPick(picks[b])
		if oa != ob {
			return oa
		}
		if picks[a].x.gain != picks[b].x.gain {
			return picks[a].x.gain > picks[b].x.gain
		}
		return picks[a].x.item.ItemLvl > picks[b].x.item.ItemLvl
	})
	// If wearing a 2H and the plan switches to 1H + OH, keep MH immediately before OH so the pair stays together.
	if twoHanded {
		mhIdx, ohIdx := -1, -1
		for i, p := range picks {
			if len(p.remove) == 1 && p.remove[0] == 16 {
				mhIdx = i
			} else if len(p.remove) == 1 && p.remove[0] == 17 {
				ohIdx = i
			}
		}
		if mhIdx >= 0 && ohIdx >= 0 && ohIdx < mhIdx {
			mh := picks[mhIdx]
			copy(picks[ohIdx+1:mhIdx+1], picks[ohIdx:mhIdx])
			picks[ohIdx] = mh
		}
	}

	// Compute exact step-by-step prefix gains & set bonus transitions.
	var prefixAdds []Stats
	var prefixIDs []int64
	var emptied []int64
	var prefixEvals []eval
	stepSetChanges := make([][]SetChange, len(picks))
	prevActive := tracker.BaseBonuses
	for i, p := range picks {
		for _, s := range p.remove {
			if _, ok := eq[s]; ok && !containsSlot(emptied, s) {
				emptied = append(emptied, s)
			}
		}
		if twoHanded && len(p.remove) == 1 && p.remove[0] == 17 {
			if _, ok := eq[16]; ok && !containsSlot(emptied, 16) {
				emptied = append(emptied, 16)
			}
		}
		prefixAdds = append(prefixAdds, p.x.norm)
		prefixIDs = append(prefixIDs, p.x.item.ID)
		nextActive, _, _ := tracker.After(emptied, prefixIDs)
		gained, lost := itemsets.DiffBonuses(prevActive, nextActive)
		stepSetChanges[i] = changesFromDiff(gained, lost)
		prevActive = nextActive
		prefixEvals = append(prefixEvals, eval{
			key:    fmt.Sprintf("bfx:%d", i),
			adds:   append([]Stats(nil), prefixAdds...),
			addIDs: append([]int64(nil), prefixIDs...),
			rm:     append([]int64(nil), emptied...),
		})
	}
	pvals, err := valueBatch(prefixEvals, hasEngine)
	if err != nil {
		return nil, err
	}

	pkgList := make([]any, 0, len(picks))
	value := 0.0
	ownedCount := 0
	for i, p := range picks {
		next := pvals[fmt.Sprintf("bfx:%d", i)]
		step := next - value
		value = next
		if isOwnedPick(p) {
			ownedCount++
		}
		var replaced []any
		var old *sv.Table
		for _, s := range p.remove {
			if e, ok := equipped[s]; ok {
				replaced = append(replaced, e.Get("name"))
				if old == nil {
					old = e
				}
			}
		}
		if replaced == nil {
			replaced = []any{}
		}
		var compare any
		if old != nil {
			oldSlot, _ := sv.Num(old.Get("slot"))
			label := SlotLabelFor(int64(oldSlot), classFile)
			compare = Tooltip(old, &label)
		}
		pSlot := bisPickSlot(p)
		row := pyjson.New(
			"slot", pSlot,
			"slotName", bisSlotLabel(pSlot, classFile),
			"replaces", replaced,
			"compareTip", compare,
		)
		out := buildCandObj(p.x, 0)
		for _, k := range out.Keys() {
			row.Set(k, out.Get(k))
		}
		row.Set("gain", pyfmt.Round(step, 1))
		row.Set("cumGain", pyfmt.Round(value, 1))
		row.Set("cumValue", pyfmt.Round(baseValue+value, 1))
		if len(stepSetChanges[i]) > 0 {
			row.Set("setChanges", SetChangesJSON(stepSetChanges[i]))
		} else {
			row.Set("setChanges", nil)
		}
		pkgList = append(pkgList, row)
	}

	var pkgPct any
	if baseValue != 0 {
		pkgPct = pyfmt.RoundInt(value / baseValue * 100)
	}
	return pyjson.New(
		"gain", pyfmt.Round(value, 1),
		"gainPct", pkgPct,
		"targetValue", pyfmt.Round(baseValue+value, 1),
		"ownedCount", ownedCount,
		"items", pkgList,
	), nil
}
