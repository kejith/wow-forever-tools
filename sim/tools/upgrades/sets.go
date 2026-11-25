package upgrades

import (
	"fmt"
	"sort"

	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/internal/tables"
	"github.com/kejith/wow-tools-forever/sim/itemsets"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
)

// LoadSetCatalog ensures ItemSet, ItemSetSpell and SpellEffect are cached for the snapshot's
// client build and loads the decoded set catalog. Returns an empty catalog when no build is
// known or the tables are unavailable offline.
func LoadSetCatalog(c *sv.Table, cacheDir string) *itemsets.Catalog {
	if c == nil {
		return itemsets.Empty()
	}
	client := c.Tab("client")
	if !sv.Truthy(client.Get("version")) || !sv.Truthy(client.Get("build")) {
		return itemsets.Empty()
	}
	build := pyfmt.Str(client.Get("version")) + "." + pyfmt.Str(client.Get("build"))
	locale := client.Str("locale")
	if locale == "" {
		locale = "deDE"
	}
	if cacheDir == "" {
		cacheDir = tables.CacheDir()
	}
	for _, t := range []string{"ItemSet", "ItemSetSpell", "SpellEffect", "SpellClassOptions", "SpellName"} {
		tables.Ensure(cacheDir, t, build, locale)
	}
	return itemsets.Load(cacheDir, build, locale)
}

// SkillRanks extracts skillID -> rank from a character snapshot.
func SkillRanks(c *sv.Table) map[int]int {
	out := map[int]int{}
	if c == nil {
		return out
	}
	for _, s := range c.Tab("skills").Tab("skills").Tables() {
		if id, ok := s.Num("skillID"); ok && s.Has("skillID") {
			rank, _ := s.Num("rank")
			out[int(id)] = int(rank)
		}
	}
	return out
}

// SetChange describes one set bonus activated ("gain") or broken ("loss") by a gear swap.
type SetChange struct {
	Kind      string // "gain" or "loss"
	SetID     int
	SetName   string
	Threshold int
	SpellID   int
	BonusName string
}

// SetTracker tracks a character's currently equipped item set counts and computes exact
// set-bonus stat and spell-modifier deltas for any hypothetical swap or package.
type SetTracker struct {
	Cat          *itemsets.Catalog
	EquippedIDs  map[int64]int64 // slot -> itemID
	Skills       map[int]int
	Physical     bool
	TopSchool    string
	BaseBonuses  []itemsets.ActiveBonus
	BaseScale    Stats
	BaseNonSheet []int
}

// NewSetTracker initializes a SetTracker for a character snapshot and set catalog.
func NewSetTracker(c *sv.Table, cat *itemsets.Catalog) *SetTracker {
	if cat == nil {
		cat = itemsets.Empty()
	}
	eqIDs := map[int64]int64{}
	var ids []int64
	if c != nil {
		for _, e := range c.Tab("equipment").Tables() {
			slot, ok := sv.Num(e.Get("slot"))
			if !ok || int64(slot) == 0 {
				continue
			}
			id, ok := sv.Num(e.Get("id"))
			if !ok || int64(id) <= 0 {
				continue
			}
			eqIDs[int64(slot)] = int64(id)
			ids = append(ids, int64(id))
		}
	}
	skills := SkillRanks(c)
	physical := isPhysicalRole(c)
	topSchool := topSchoolForChar(c)
	base := cat.ActiveBonuses(ids, skills)
	baseScale := Stats(itemsets.ScaleStats(base, physical, topSchool))
	if baseScale == nil {
		baseScale = Stats{}
	}
	return &SetTracker{
		Cat:          cat,
		EquippedIDs:  eqIDs,
		Skills:       skills,
		Physical:     physical,
		TopSchool:    topSchool,
		BaseBonuses:  base,
		BaseScale:    baseScale,
		BaseNonSheet: itemsets.NonSheetSpellIDs(base),
	}
}

func topSchoolForChar(c *sv.Table) string {
	if c == nil {
		return ""
	}
	ch := c.Tab("character")
	classFile := ch.Str("classFile")
	spec := enginebundle.SpecOf(c)
	switch classFile {
	case "WARLOCK", "PRIEST":
		if enginebundle.RoleForChar(c) == "healer" {
			return "holy"
		}
		return "shadow"
	case "MAGE":
		switch spec {
		case "fire", "frost", "arcane":
			return spec
		default:
			return "frost"
		}
	case "DRUID", "SHAMAN":
		if enginebundle.RoleForChar(c) == "healer" {
			return "nature"
		}
		return "nature"
	case "PALADIN":
		return "holy"
	}
	return ""
}

// HasSetData reports whether any sets are loaded in the catalog.
func (t *SetTracker) HasSetData() bool {
	return t != nil && t.Cat != nil && len(t.Cat.Sets) > 0
}

// touchesSet returns true if any removed slot's item or any added item belongs to a set.
func (t *SetTracker) touchesSet(rm []int64, addIDs []int64) bool {
	if !t.HasSetData() {
		return false
	}
	for _, s := range rm {
		if id := t.EquippedIDs[s]; id > 0 {
			if _, ok := t.Cat.ItemToSet[int(id)]; ok {
				return true
			}
		}
	}
	for _, id := range addIDs {
		if id > 0 {
			if _, ok := t.Cat.ItemToSet[int(id)]; ok {
				return true
			}
		}
	}
	return false
}

// After returns the active, gained, and lost bonuses after removing slots rm and equipping addIDs.
func (t *SetTracker) After(rm []int64, addIDs []int64) (active, gained, lost []itemsets.ActiveBonus) {
	if !t.touchesSet(rm, addIDs) {
		return t.BaseBonuses, nil, nil
	}
	rmSet := make(map[int64]bool, len(rm))
	for _, s := range rm {
		rmSet[s] = true
	}
	ids := make([]int64, 0, len(t.EquippedIDs)+len(addIDs))
	for slot, id := range t.EquippedIDs {
		if !rmSet[slot] {
			ids = append(ids, id)
		}
	}
	ids = append(ids, addIDs...)
	active = t.Cat.ActiveBonuses(ids, t.Skills)
	gained, lost = itemsets.DiffBonuses(t.BaseBonuses, active)
	return active, gained, lost
}

// EngineChange computes the engine stat delta and non-sheet set bonus spell change for a swap.
func (t *SetTracker) EngineChange(rm []int64, addIDs []int64) (statDelta map[string]float64, setSpells []int, hasSetChange bool) {
	if !t.touchesSet(rm, addIDs) {
		return nil, nil, false
	}
	active, gained, lost := t.After(rm, addIDs)
	if len(gained) == 0 && len(lost) == 0 {
		return nil, nil, false
	}
	statDelta = itemsets.DeltaStats(gained, lost)
	nextNonSheet := itemsets.NonSheetSpellIDs(active)
	if !intsEqual(t.BaseNonSheet, nextNonSheet) {
		hasSetChange = true
		setSpells = nextNonSheet
	}
	return statDelta, setSpells, hasSetChange
}

// ScaleDelta returns the weight-scale stat delta from gained minus lost set bonuses.
func (t *SetTracker) ScaleDelta(rm []int64, addIDs []int64) Stats {
	if !t.touchesSet(rm, addIDs) {
		return nil
	}
	_, gained, lost := t.After(rm, addIDs)
	if len(gained) == 0 && len(lost) == 0 {
		return nil
	}
	d := Stats{}
	if len(gained) > 0 {
		d.Add(Stats(itemsets.ScaleStats(gained, t.Physical, t.TopSchool)), 1)
	}
	if len(lost) > 0 {
		d.Add(Stats(itemsets.ScaleStats(lost, t.Physical, t.TopSchool)), -1)
	}
	return d
}

// Changes returns deduplicated human-facing set bonus changes (one per SetID + Threshold)
// for a single swap relative to the baseline equipment.
func (t *SetTracker) Changes(rm []int64, addIDs []int64) []SetChange {
	_, gained, lost := t.After(rm, addIDs)
	return changesFromDiff(gained, lost)
}

func changesFromDiff(gained, lost []itemsets.ActiveBonus) []SetChange {
	if len(gained) == 0 && len(lost) == 0 {
		return nil
	}
	type key struct {
		kind      string
		setID     int
		threshold int
	}
	seen := map[key]bool{}
	var out []SetChange
	add := func(kind string, list []itemsets.ActiveBonus) {
		for _, b := range list {
			k := key{kind, b.SetID, b.Threshold}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, SetChange{
				Kind:      kind,
				SetID:     b.SetID,
				SetName:   b.SetName,
				Threshold: b.Threshold,
				SpellID:   b.SpellID,
				BonusName: b.SpellName,
			})
		}
	}
	add("gain", gained)
	add("loss", lost)
	return out
}

// SetChangesJSON converts SetChange entries into pyjson objects for API/UI output.
func SetChangesJSON(changes []SetChange) []any {
	if len(changes) == 0 {
		return nil
	}
	out := make([]any, len(changes))
	for i, c := range changes {
		out[i] = pyjson.New("kind", c.Kind, "setId", c.SetID, "set", c.SetName,
			"threshold", c.Threshold, "bonus", c.BonusName)
	}
	return out
}

// ActiveSummaries returns summaries for all sets with at least one currently equipped piece.
func (t *SetTracker) ActiveSummaries() []itemsets.SetSummary {
	if !t.HasSetData() {
		return nil
	}
	ids := make([]int64, 0, len(t.EquippedIDs))
	for _, id := range t.EquippedIDs {
		ids = append(ids, id)
	}
	return t.Cat.Summary(ids, t.Skills)
}

// SummariesJSON converts SetSummary entries into pyjson objects for API/UI output.
func SummariesJSON(summaries []itemsets.SetSummary) []any {
	out := make([]any, 0, len(summaries))
	for _, s := range summaries {
		bonuses := make([]any, 0, len(s.Bonuses))
		seenThr := map[int]bool{}
		for _, b := range s.Bonuses {
			if seenThr[b.Threshold] {
				continue
			}
			seenThr[b.Threshold] = true
			bonuses = append(bonuses, pyjson.New("threshold", b.Threshold, "active", b.Active, "name", b.SpellName))
		}
		out = append(out, pyjson.New("id", s.ID, "name", s.Name, "equipped", s.Equipped, "total", s.Total,
			"skillMet", s.SkillMet, "bonuses", bonuses))
	}
	return out
}

// SpellSetLabels maps SpellID -> "Set Name (Npc)" for currently active set bonuses,
// used to label set bonus entries in the engine coverage report.
func (t *SetTracker) SpellSetLabels() map[int]string {
	if t == nil || len(t.BaseBonuses) == 0 {
		return nil
	}
	out := make(map[int]string, len(t.BaseBonuses))
	for _, b := range t.BaseBonuses {
		if _, ok := out[b.SpellID]; ok {
			continue
		}
		label := fmt.Sprintf("%s (%d)", b.SetName, b.Threshold)
		if b.SpellName != "" {
			label = fmt.Sprintf("%s (%d): %s", b.SetName, b.Threshold, b.SpellName)
		}
		out[b.SpellID] = label
	}
	return out
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// setBundles builds multi-piece candidate bundles (2 or 3 pieces from the same item set in
// distinct slot groups) that unlock at least one threshold bonus not already active, so the
// package optimizer can discover synergistic sets (e.g. Devilsaur 2pc, Bloodvine 3pc) even
// when individual pieces look weak on their own.
func setBundles(usable []*cand, tracker *SetTracker, budget int64) [][]pick {
	if !tracker.HasSetData() || len(usable) == 0 {
		return nil
	}
	// Group usable candidates by setID -> slotGroup -> cheapest/best candidate per item.
	bySet := map[int]map[string][]*cand{}
	for _, x := range usable {
		if x.price > budget {
			continue
		}
		s := tracker.Cat.SetOf(x.id)
		if s == nil || !s.SkillMet(tracker.Skills) {
			continue
		}
		// Only consider sets that have at least one modeled bonus (stats or non-sheet mods).
		hasUseful := false
		for _, b := range s.Bonuses {
			if len(b.Stats) > 0 || b.HasNonSheetMods {
				hasUseful = true
				break
			}
		}
		if !hasUseful {
			continue
		}
		g := groupOf(x.slot)
		if bySet[s.ID] == nil {
			bySet[s.ID] = map[string][]*cand{}
		}
		bySet[s.ID][g] = append(bySet[s.ID][g], x)
	}
	if len(bySet) == 0 {
		return nil
	}

	// Count currently equipped pieces per set (and which slot groups hold them).
	eqCounts := map[int]int{}
	eqSlotGroup := map[int]map[string]bool{}
	for slot, id := range tracker.EquippedIDs {
		if s := tracker.Cat.SetOf(id); s != nil {
			eqCounts[s.ID]++
			if eqSlotGroup[s.ID] == nil {
				eqSlotGroup[s.ID] = map[string]bool{}
			}
			eqSlotGroup[s.ID][groupOf(slot)] = true
		}
	}

	setIDs := make([]int, 0, len(bySet))
	for sid := range bySet {
		setIDs = append(setIDs, sid)
	}
	sort.Ints(setIDs)

	var bundles [][]pick
	for _, sid := range setIDs {
		s := tracker.Cat.Sets[sid]
		grpMap := bySet[sid]
		if len(grpMap) < 2 {
			continue
		}
		// Pick best candidate (highest gain, then lowest price) per slot group for this set.
		grps := make([]string, 0, len(grpMap))
		for g := range grpMap {
			grps = append(grps, g)
		}
		sort.Strings(grps)
		bestPerGrp := make([]*cand, 0, len(grps))
		for _, g := range grps {
			cs := grpMap[g]
			best := cs[0]
			for _, x := range cs[1:] {
				if x.gain > best.gain || (x.gain == best.gain && x.price < best.price) {
					best = x
				}
			}
			// Skip if this slot already wears the exact same set item.
			alreadyWorn := false
			for _, rm := range best.remove {
				if tracker.EquippedIDs[rm] == best.id {
					alreadyWorn = true
					break
				}
			}
			if !alreadyWorn {
				bestPerGrp = append(bestPerGrp, best)
			}
		}
		if len(bestPerGrp) < 2 {
			continue
		}
		// Cap to top 6 slots per set to keep combinatorics small.
		if len(bestPerGrp) > 6 {
			sort.SliceStable(bestPerGrp, func(i, j int) bool {
				if bestPerGrp[i].gain != bestPerGrp[j].gain {
					return bestPerGrp[i].gain > bestPerGrp[j].gain
				}
				return bestPerGrp[i].price < bestPerGrp[j].price
			})
			bestPerGrp = bestPerGrp[:6]
		}
		// Find thresholds strictly above current count that have modeled effects.
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
		// Helper to compute resulting set piece count when equipping a list of set candidates.
		newCount := func(xs []*cand) int {
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
				seen[x.id] = true
			}
			return len(seen)
		}
		reachesNewThreshold := func(n int) bool {
			for thr := range wantThr {
				if n >= thr {
					return true
				}
			}
			return false
		}
		// 2-piece bundles.
		for i := 0; i < len(bestPerGrp); i++ {
			for j := i + 1; j < len(bestPerGrp); j++ {
				a, b := bestPerGrp[i], bestPerGrp[j]
				if a.id == b.id || a.price+b.price > budget {
					continue
				}
				if reachesNewThreshold(newCount([]*cand{a, b})) {
					bundles = append(bundles, []pick{{a, a.remove}, {b, b.remove}})
				}
			}
		}
		// 3-piece bundles (for 3pc sets like Bloodvine when 0 pieces are worn).
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
							if a.id == b.id || a.id == c.id || b.id == c.id {
								continue
							}
							if a.price+b.price+c.price > budget {
								continue
							}
							if reachesNewThreshold(newCount([]*cand{a, b, c})) {
								bundles = append(bundles, []pick{{a, a.remove}, {b, b.remove}, {c, c.remove}})
							}
						}
					}
				}
			}
		}
	}
	return bundles
}
