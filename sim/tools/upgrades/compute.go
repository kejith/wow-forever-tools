package upgrades

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kejith/wow-forever-tools/sim/engine"
	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/tools/enginebundle"
)

// NewID: item IDs above this exist only in Forever.
const newID = 200000

// pairs are the slots that exist twice.
var pairs = map[int64][2]int64{11: {11, 12}, 12: {11, 12}, 13: {13, 14}, 14: {13, 14}}

// Options are the arguments of upgrades.compute besides the folder and the character.
type Options struct {
	Level          int64
	Preview        int64
	MaxPrice       float64
	Top            int
	Profile        string
	Sort           string // "gain" or "price"
	Engine         string // "off", "on", "auto"
	FightSeconds   float64
	TargetDelta    int
	KillsPerMinute float64
	Metric         string
	Method         string
	Iterations     int
	// Consumables is the best set of consumables for the fight (consumables.fight_buffs), nil without:
	// every number is then rated on top of it.
	Consumables *Consumed
	// FightBuffs, when set, is consumables.fight_buffs: called with the metric asked for and the fight once the
	// engine is asked for and models the class, it gives the set every number is rated on top of (it replaces
	// Consumables). An error falls back to the weight scale with engine "auto", as Python's LookupError does,
	// unless it is a *Fatal.
	FightBuffs FightBuffsFunc
}

// FightBuffsFunc is consumables.fight_buffs for one character: the best set of consumables for a metric ("" =
// the spec's own) over a fight of seconds against enemies targetDelta levels above.
type FightBuffsFunc func(metric string, seconds float64, targetDelta int) (*Consumed, error)

// Fatal wraps an error of FightBuffs that Python raises as something else than a LookupError: upgrades
// returns it whatever the engine option says.
type Fatal struct{ Err error }

func (f *Fatal) Error() string { return f.Err.Error() }
func (f *Fatal) Unwrap() error { return f.Err }

// Consumed is what consumables.fight_buffs returns.
type Consumed struct {
	Delta map[string]float64
	Names []string
	Gain  float64
	Unit  string
	Text  string
}

// LoadPrices is writs.load_prices(writs.find_auctionator(wow)) with Python's errors.
func LoadPrices(wow string) (*sv.Table, error) {
	path := game.FindAuctionator(wow)
	if path == "" {
		return nil, errors.New(i18n.T("Auctionator.lua not found under {wow}", "wow", wow))
	}
	p, err := sv.Prices(path)
	if errors.Is(err, sv.ErrNoPrices) {
		return nil, errors.New(i18n.T("No price data found in Auctionator.lua."))
	}
	return p, err
}

// isDict tells whether a value read from the price database is a Python dict.
func isDict(v any) (*sv.Table, bool) {
	t, ok := v.(*sv.Table)
	if !ok || t.IsList() {
		return nil, false
	}
	return t, true
}

func intKey(k any) (int64, bool) {
	switch x := k.(type) {
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case string:
		return pyfmt.ParseInt(x)
	}
	return 0, false
}

// cand is one item of the pool of candidates.
type cand struct {
	id            int64
	it            *sv.Table
	price         int64
	slot          int64
	variants      [][]int64
	norm          Stats
	extra         []string
	soon          bool
	source, owner string
	remove        []int64
	gain, points  float64
}

type eval struct {
	key  string
	adds []Stats
	rm   []int64
	noMC bool
}

type pick struct {
	x      *cand
	remove []int64
}

type state struct {
	cost  int64
	gain  float64
	picks []pick
}

// pareto keeps the (cost, gain, ...) states where more money buys more gain.
func pareto(states []state) []state {
	s := append([]state(nil), states...)
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].cost != s[j].cost {
			return s[i].cost < s[j].cost
		}
		return s[i].gain > s[j].gain
	})
	var out []state
	best := math.Inf(-1)
	for _, st := range s {
		if st.gain > best+1e-9 {
			out = append(out, st)
			best = st.gain
		}
	}
	return out
}

// bestPackage is upgrades.best_package: a multiple-choice knapsack, at most one option per group, the highest
// total gain within the budget.
func bestPackage(groups [][]state, budget int64) state {
	frontier := []state{{}}
	for _, options := range groups {
		var keep []state
		for _, o := range options {
			if o.gain > 0 && o.cost <= budget {
				keep = append(keep, o)
			}
		}
		options = pareto(keep)
		states := append([]state(nil), frontier...)
		for _, f := range frontier {
			for _, o := range options {
				if f.cost+o.cost <= budget {
					picks := append(append([]pick(nil), f.picks...), o.picks...)
					states = append(states, state{f.cost + o.cost, f.gain + o.gain, picks})
				}
			}
		}
		frontier = pareto(states)
		if len(frontier) > 4000 { // thin out: best state per price bucket
			step := budget / 4000
			if step < 1 {
				step = 1
			}
			var order []int64
			bucket := map[int64]state{}
			for _, st := range frontier {
				k := floorDiv(st.cost, step)
				if _, ok := bucket[k]; !ok {
					order = append(order, k)
				}
				bucket[k] = st
			}
			thin := make([]state, len(order))
			for i, k := range order {
				thin[i] = bucket[k]
			}
			frontier = pareto(thin)
		}
	}
	return frontier[len(frontier)-1]
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func groupOf(slot int64) string {
	if p, ok := pairs[slot]; ok {
		return itoa(p[0]) + "," + itoa(p[1])
	}
	if slot == 16 || slot == 17 {
		return "weapon"
	}
	return itoa(slot)
}

func joinSlots(rm []int64) string {
	parts := make([]string, len(rm))
	for i, s := range rm {
		parts[i] = itoa(s)
	}
	return strings.Join(parts, "+")
}

func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func orZero(t *sv.Table, k string) any {
	if v := t.Get(k); sv.Truthy(v) {
		return v
	}
	return int64(0)
}

func sliceTop[T any](xs []T, top int) []T {
	if top < 0 {
		top += len(xs)
		if top < 0 {
			top = 0
		}
	}
	if top < len(xs) {
		return xs[:top]
	}
	return xs
}

func pctLines() map[string]*regexp.Regexp {
	m := map[string]*regexp.Regexp{}
	for _, pl := range PctLine {
		m[pl.Kind] = pl.Re
	}
	return m
}

// Compute is upgrades.compute: upgrade candidates per slot and the best purchase for the budget, as plain
// data (copper prices). prices may be nil (read from the game folder).
func Compute(wow, char string, prices *sv.Table, o Options) (*pyjson.Obj, error) {
	if o.Sort == "" {
		o.Sort = "gain"
	}
	if o.Engine == "" {
		o.Engine = "off"
	}
	if prices == nil {
		p, err := LoadPrices(wow)
		if err != nil {
			return nil, err
		}
		prices = p
	}
	last, haveLast := int64(0), false
	for _, e := range prices.Values() {
		t, ok := isDict(e)
		if !ok {
			continue
		}
		for _, k := range t.Tab("a").Iter() {
			if d, ok := intKey(k); ok && (!haveLast || d > last) {
				last, haveLast = d, true
			}
		}
	}
	if !haveLast {
		return nil, errors.New("max() iterable argument is empty")
	}
	files := game.FindChars(wow, char)
	if len(files) == 0 {
		return nil, errors.New(i18n.T("No WoWMCP.lua found for this character."))
	}
	c, err := LoadChar(files[0])
	if err != nil {
		return nil, err
	}
	if c == nil {
		c = sv.NewTable()
	}
	ch := c.Tab("character")
	info := ItemExport(wow, c)
	itemsTab := info.Tab("items")
	var itemIDs []int64
	items := map[int64]*sv.Table{}
	var itemList []*sv.Table
	for i, k := range itemsTab.Keys() {
		id, ok := intKey(k)
		x, isTab := itemsTab.Values()[i].(*sv.Table)
		if !ok || !isTab {
			continue
		}
		if _, dup := items[id]; !dup {
			itemIDs = append(itemIDs, id)
		}
		items[id] = x
	}
	for _, id := range itemIDs {
		itemList = append(itemList, items[id])
	}
	if len(items) == 0 {
		return nil, errors.New(i18n.T("No item export found: type /wowmcp items in the game, wait for the message, then /reload."))
	}
	var levelOut any = o.Level
	level := float64(o.Level)
	if o.Level == 0 {
		levelOut = orValue(ch, "level", int64(60))
		level, _ = sv.Num(levelOut)
	}
	gold, _ := ch.Num("money")
	budget := int64(gold)
	if o.MaxPrice != 0 {
		budget = int64(o.MaxPrice * 10000)
	}

	equipped := map[int64]*sv.Table{}
	for _, e := range c.Tab("equipment").Tables() {
		if s, ok := sv.Num(e.Get("slot")); ok {
			equipped[int64(s)] = e
		}
	}
	skills := map[string]float64{}
	for _, s := range c.Tab("skills").Tab("skills").Tables() {
		if name := s.Str("name"); name != "" {
			skills[name], _ = s.Num("rank")
		}
	}
	ratings := LearnRatings(itemList)
	eq, totals := EquippedStats(c, ratings)
	scale := BuildScale(c, o.Profile, totals["hit"], totals["crit"])
	base := SetValue(totals, scale)
	twoHanded := equipped[16].Str("equipLoc") == "INVTYPE_2HWEAPON"
	// The class line of a tooltip names the classes in the client's language, like the character's class.
	classNames := warlockNames
	if own := ch.Str("class"); own != "" {
		classNames = []string{own}
	}

	// The combat engine, when asked for: "on" must work, "auto" falls back to the weight scale.
	var eng *enginebundle.Engine
	classFile := ch.Str("classFile")
	if o.Engine != "off" {
		var consumed *Consumed
		eng, consumed, err = startEngine(c, classFile, totals, scale, o)
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
	if eng == nil && enginebundle.Supported(classFile) && classFile != "WARLOCK" {
		// The weight scale knows only the warlock; a class the engine models must not fall back to it.
		return nil, errors.New(i18n.T("This class is rated by the combat engine (simforever), which did not run."))
	}

	// What the valuer sees: the engine keeps healing and per-school damage apart, the scale does not.
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

	swap := func(remove []int64, adds []Stats) float64 {
		t := totals.Copy()
		for _, s := range remove {
			t.Add(eq[s], -1)
		}
		for _, n := range adds {
			t.Add(n, 1)
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
				list[i] = enginebundle.Eval{Key: ev.key, Delta: enginebundle.Delta(adds, removes), NoMC: ev.noMC}
			}
			return eng.Batch(list, reoptimize)
		}
		out := map[string]float64{}
		for _, ev := range evals {
			out[ev.key] = swap(ev.rm, ev.adds)
		}
		return out, nil
	}

	usableWep := usableWeaponsFor(classFile)
	usableArm := usableArmorFor(classFile, level)
	var pool []*cand
	consider := func(iid int64, it *sv.Table, price int64, source, owner string) bool {
		slots := SlotOf[it.Str("equipLoc")]
		req, _ := it.Num("reqLevel")
		if len(slots) == 0 || req > level+float64(o.Preview) {
			return false
		}
		classID, hasClass := sv.Num(it.Get("classID"))
		subclass, hasSub := sv.Num(it.Get("subclassID"))
		if hasClass && classID == 2 && (!hasSub || !usableWep[int64(subclass)] || subclass != float64(int64(subclass))) {
			return false
		}
		if hasClass && classID == 4 && (!hasSub || !usableArm[int64(subclass)] || subclass != float64(int64(subclass))) {
			return false
		}
		tip := tipLines(it)
		for _, t := range tip {
			if hasAnyPrefix(t, classesPrefix) {
				named := false
				for _, n := range classNames {
					named = named || strings.Contains(t, n)
				}
				if !named {
					return false
				}
			}
		}
		if !meetsSkillRequirements(tip, skills) {
			return false
		}
		ranged := len(slots) == 1 && slots[0] == 18
		n := normValue(it, ranged)
		var slot int64
		var variants [][]int64
		switch {
		case len(slots) == 2 && slots[0] == 16 && slots[1] == 17:
			slot, variants = 16, [][]int64{{16, 17}}
		case len(slots) == 2: // ring or trinket: like Pawn, against the weaker of the two (the bigger gain)
			// If this item is Unique / Unique-Equipped and already worn in one of the pair
			// slots, it may only replace that slot (never produce two identical equipped items).
			if isUnique(tip) {
				id0, _ := equipped[slots[0]].Num("id")
				id1, _ := equipped[slots[1]].Num("id")
				if int64(id0) == iid {
					slot, variants = slots[0], [][]int64{{slots[0]}}
				} else if int64(id1) == iid {
					slot, variants = slots[1], [][]int64{{slots[1]}}
				} else {
					slot, variants = 0, [][]int64{{slots[0]}, {slots[1]}}
				}
			} else {
				slot, variants = 0, [][]int64{{slots[0]}, {slots[1]}}
			}
		case len(slots) == 1 && slots[0] == 17 && twoHanded: // an off-hand item means giving up the staff
			slot, variants = 17, [][]int64{{16}}
		default:
			slot, variants = slots[0], [][]int64{{slots[0]}}
		}
		pool = append(pool, &cand{id: iid, it: it, price: price, slot: slot, variants: variants, norm: n,
			extra: SpecialEffects(tip), soon: req > level, source: source, owner: owner})
		return true
	}

	// What the account already has costs nothing; the auction house is not asked for the same item again.
	owned, ownedUnknown := OwnedGear(files[0], c, items)
	have := map[int64]bool{}
	for _, ow := range owned {
		if consider(ow.ID, ow.It, 0, ow.Source, ow.Owner) {
			have[ow.ID] = true
		}
	}
	for _, iid := range itemIDs {
		it := items[iid]
		e, ok := isDict(prices.Get(strconv.FormatInt(iid, 10)))
		q, _ := it.Num("quality")
		if have[iid] || q < 2 || !ok || !sv.Truthy(e.Get("m")) {
			continue
		}
		recent := false
		for _, d := range e.Tab("a").Iter() {
			if day, ok := intKey(d); ok && day >= last-3 {
				recent = true
				break
			}
		}
		if !recent {
			continue
		}
		m, _ := sv.Num(e.Get("m"))
		if m <= float64(budget) {
			consider(iid, it, int64(m), "ah", "")
		}
	}

	// Phase 1: value all candidates with the fixed baseline rotation.
	// Add-only points (pt:*) are only needed to select pair/weapon combo
	// candidates (rings, trinkets, 1H weapons, off-hands). Helper entries
	// (pt:* and rm:*) never consume the Monte-Carlo MCTop budget.
	isComboSlot := func(x *cand) bool {
		if x.slot == 0 || x.slot == 11 || x.slot == 12 || x.slot == 13 || x.slot == 14 || x.slot == 17 {
			return true
		}
		return x.slot == 16 && len(x.variants) > 0 && len(x.variants[0]) == 1
	}
	var evals []eval
	for i, x := range pool {
		if isComboSlot(x) {
			evals = append(evals, eval{fmt.Sprintf("pt:%d", i), []Stats{x.norm}, nil, true})
		}
	}
	for i, x := range pool {
		for j, rm := range x.variants {
			// When the engine runs, Phase 2 refines the top swaps (with reoptimize & MC).
			evals = append(evals, eval{fmt.Sprintf("sw:%d:%d", i, j), []Stats{x.norm}, rm, eng != nil})
		}
	}
	removes := map[string][]int64{}
	for s := range SlotName {
		removes[joinSlots([]int64{s})] = []int64{s}
	}
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
		evals = append(evals, eval{"rm:" + joinSlots(rm), nil, rm, true})
	}
	values, err := valueBatch(evals, false)
	if err != nil {
		return nil, err
	}

	// Phase 2 (engine only): re-evaluate the top candidates per slot (plus all
	// owned items) with rotation re-optimization (and Monte-Carlo when active),
	// so threshold upgrades (new heal rank, rotation shift) are scored accurately
	// and MCTop goes strictly to real upgrades.
	if eng != nil && len(pool) > 0 {
		bySlot := map[int64][]int{}
		for i, x := range pool {
			best := 0
			for j := range x.variants {
				if values[fmt.Sprintf("sw:%d:%d", i, j)] > values[fmt.Sprintf("sw:%d:%d", i, best)] {
					best = j
				}
			}
			s := x.slot
			if s == 0 {
				s = x.variants[best][0]
			}
			bySlot[s] = append(bySlot[s], i)
		}
		var refine []eval
		picked := map[int]bool{}
		for _, idxs := range bySlot {
			sort.SliceStable(idxs, func(a, b int) bool {
				ga, gb := bestSwapVal(values, idxs[a], pool[idxs[a]]), bestSwapVal(values, idxs[b], pool[idxs[b]])
				if ga != gb {
					return ga > gb
				}
				return pool[idxs[a]].price < pool[idxs[b]].price
			})
			for k, idx := range idxs {
				x := pool[idx]
				if k < 12 || x.source != "ah" {
					if !picked[idx] {
						picked[idx] = true
						for j, rm := range x.variants {
							refine = append(refine, eval{fmt.Sprintf("sw:%d:%d", idx, j), []Stats{x.norm}, rm, false})
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

	itemOut := func(x *cand, gain float64, remove []int64) *pyjson.Obj {
		it := x.it
		old := oldOf(remove)
		var gainPct, perGold any
		if old > 0.5 {
			gainPct = pyfmt.RoundInt(gain / old * 100)
		}
		if x.price != 0 {
			perGold = pyfmt.Round(gain/(float64(x.price)/10000), 1)
		}
		effects := make([]any, len(x.extra))
		for i, e := range x.extra {
			effects[i] = e
		}
		return pyjson.New("id", x.id, "name", it.Get("name"), "quality", it.Get("quality"),
			"reqLevel", orZero(it, "reqLevel"), "price", x.price, "gain", pyfmt.Round(gain, 1), "gainPct", gainPct,
			"perGold", perGold, "source", x.source, "owner", strOrNil(x.owner), "where", WhereText(x.source, x.owner),
			"stats", Short(it.Tab("stats")), "isNew", x.id >= newID, "soon", x.soon, "effects", effects,
			"twoHand", len(remove) == 2, "tip", Tooltip(it, nil))
	}

	type reviewRow struct {
		obj   *pyjson.Obj
		price int64
	}
	var review []reviewRow
	for i, x := range pool {
		best := 0
		for j := range x.variants {
			if values[fmt.Sprintf("sw:%d:%d", i, j)] > values[fmt.Sprintf("sw:%d:%d", i, best)] {
				best = j
			}
		}
		x.remove, x.gain, x.points = x.variants[best], values[fmt.Sprintf("sw:%d:%d", i, best)], values[fmt.Sprintf("pt:%d", i)]
		if x.slot == 0 {
			x.slot = x.remove[0]
		}
		req, _ := x.it.Num("reqLevel")
		if len(x.extra) > 0 && x.gain < 5 && req >= level-15 {
			row := pyjson.New("slot", SlotLabel(x.slot))
			out := itemOut(x, x.gain, x.remove)
			for _, k := range out.Keys() {
				row.Set(k, out.Get(k))
			}
			review = append(review, reviewRow{row, x.price})
		}
	}

	var rows []*cand
	for _, x := range pool {
		if x.gain > 0.5 && !(len(x.extra) > 0 && x.gain < 5) {
			rows = append(rows, x)
		}
	}
	// By gain per gold, what is owned comes first: it costs nothing.
	less := func(a, b *cand) bool {
		if o.Sort == "price" {
			if (a.price > 0) != (b.price > 0) {
				return b.price > 0
			}
			ra, rb := -a.gain/float64(max(a.price, 1)), -b.gain/float64(max(b.price, 1))
			if ra != rb {
				return ra < rb
			}
			return -a.gain < -b.gain
		}
		if a.gain != b.gain {
			return a.gain > b.gain
		}
		return a.price < b.price
	}
	slotSet := map[int64]bool{}
	var slotOrder []int64
	for _, x := range rows {
		if !slotSet[x.slot] {
			slotSet[x.slot] = true
			slotOrder = append(slotOrder, x.slot)
		}
	}
	sort.Slice(slotOrder, func(a, b int) bool { return slotOrder[a] < slotOrder[b] })
	slotsOut := []any{}
	for _, slot := range slotOrder {
		var best []*cand
		for _, x := range rows {
			if x.slot == slot {
				best = append(best, x)
			}
		}
		sort.SliceStable(best, func(a, b int) bool { return less(best[a], best[b]) })
		best = sliceTop(best, o.Top)
		cands := make([]any, len(best))
		for i, x := range best {
			cands[i] = itemOut(x, x.gain, x.remove)
		}
		var current any
		if cur := equipped[slot]; sv.Truthy(cur) {
			label := SlotLabel(slot)
			current = pyjson.New("name", cur.Get("name"), "id", cur.Get("id"), "quality", cur.Get("quality"),
				"stats", Short(cur.Tab("stats")), "tip", Tooltip(cur, &label))
		}
		slotsOut = append(slotsOut, pyjson.New("slot", slot, "slotName", SlotLabel(slot), "current", current,
			"candidates", cands))
	}

	// Best purchase for the budget: one option per slot group, pairs for rings/trinkets and main hand + off hand.
	var usable []*cand
	for _, x := range pool {
		if !x.soon {
			usable = append(usable, x)
		}
	}
	var groupKeys []string
	groups := map[string][]state{}
	addGroup := func(key string, st state) {
		if _, ok := groups[key]; !ok {
			groupKeys = append(groupKeys, key)
		}
		groups[key] = append(groups[key], st)
	}
	for _, x := range rows {
		if !x.soon {
			addGroup(groupOf(x.slot), state{x.price, x.gain, []pick{{x, x.remove}}})
		}
	}
	type combo struct {
		key    string
		x, y   *cand
		ra, rb []int64
	}
	byPoints := func(xs []*cand, n int) []*cand {
		sort.SliceStable(xs, func(a, b int) bool { return -xs[a].points < -xs[b].points })
		return sliceTop(xs, n)
	}
	var combos []combo
	for _, p := range [][2]int64{{11, 12}, {13, 14}} { // the order of set(PAIRS.values()) in CPython
		var both []*cand
		for _, x := range usable {
			if x.slot == p[0] || x.slot == p[1] {
				both = append(both, x)
			}
		}
		both = byPoints(both, 12)
		for i, x := range both {
			for _, y := range both[i+1:] {
				combos = append(combos, combo{groupOf(p[0]), x, y, []int64{p[0]}, []int64{p[1]}})
			}
		}
	}
	var oneHand, offHand []*cand
	for _, x := range usable {
		if x.slot == 16 && len(x.remove) == 1 {
			oneHand = append(oneHand, x)
		}
		if x.slot == 17 {
			offHand = append(offHand, x)
		}
	}
	oneHand, offHand = byPoints(oneHand, 10), byPoints(offHand, 10)
	for _, x := range oneHand {
		for _, y := range offHand {
			combos = append(combos, combo{"weapon", x, y, []int64{16}, []int64{17}})
		}
	}
	cevals := make([]eval, len(combos))
	for i, cb := range combos {
		cevals[i] = eval{fmt.Sprintf("cb:%d", i), []Stats{cb.x.norm, cb.y.norm}, append(append([]int64(nil), cb.ra...), cb.rb...), false}
	}
	cvals, err := valueBatch(cevals, eng != nil)
	if err != nil {
		return nil, err
	}
	for i, cb := range combos {
		addGroup(cb.key, state{cb.x.price + cb.y.price, cvals[fmt.Sprintf("cb:%d", i)],
			[]pick{{cb.x, cb.ra}, {cb.y, cb.rb}}})
	}
	groupList := make([][]state, len(groupKeys))
	for i, k := range groupKeys {
		groupList[i] = groups[k]
	}
	picks := bestPackage(groupList, budget).picks

	// Drop units that add nothing on top of the rest (e.g. redundant hit past
	// cap) and, in engine mode, run up to 2 rounds of exact 1-opt local search
	// across slot groups so freed budget or non-additive interactions (hit cap,
	// mana thresholds) can pick a better alternative or fill an empty slot.
	evalSet := func(ps []pick) ([]Stats, []int64) {
		var adds []Stats
		var emptied []int64
		for _, p := range ps {
			for _, s := range p.remove {
				if _, ok := eq[s]; ok && !containsSlot(emptied, s) {
					emptied = append(emptied, s)
				}
			}
			if twoHanded && p.x.slot == 17 {
				if _, ok := eq[16]; ok && !containsSlot(emptied, 16) {
					emptied = append(emptied, 16)
				}
			}
			adds = append(adds, p.x.norm)
		}
		return adds, emptied
	}
	for range 4 {
		var prefixAdds []Stats
		var emptied []int64
		var prefixEvals []eval
		for i, p := range picks {
			for _, s := range p.remove {
				if _, ok := eq[s]; ok && !containsSlot(emptied, s) {
					emptied = append(emptied, s)
				}
			}
			prefixAdds = append(prefixAdds, p.x.norm)
			prefixEvals = append(prefixEvals, eval{fmt.Sprintf("px:%d", i),
				append([]Stats(nil), prefixAdds...), append([]int64(nil), emptied...), true})
		}
		pvals, err := valueBatch(prefixEvals, eng != nil)
		if err != nil {
			return nil, err
		}
		unitGain := map[string]float64{}
		for i, p := range picks {
			prev := 0.0
			if i > 0 {
				prev = pvals[fmt.Sprintf("px:%d", i-1)]
			}
			unitGain[groupOf(p.x.slot)] += pvals[fmt.Sprintf("px:%d", i)] - prev
		}
		var keep []pick
		for _, p := range picks {
			if unitGain[groupOf(p.x.slot)] > 0.005 {
				keep = append(keep, p)
			}
		}
		if len(keep) == len(picks) {
			break
		}
		picks = keep
	}
	if eng != nil && len(groupKeys) > 0 {
		// Pre-sort top affordable options per group for 1-opt swaps.
		topOpts := map[string][]state{}
		for _, k := range groupKeys {
			opts := append([]state(nil), groups[k]...)
			sort.SliceStable(opts, func(i, j int) bool {
				if opts[i].gain != opts[j].gain {
					return opts[i].gain > opts[j].gain
				}
				return opts[i].cost < opts[j].cost
			})
			if len(opts) > 6 {
				opts = opts[:6]
			}
			topOpts[k] = opts
		}
		for range 2 {
			curCost := int64(0)
			byGrp := map[string][]pick{}
			for _, p := range picks {
				curCost += p.x.price
				g := groupOf(p.x.slot)
				byGrp[g] = append(byGrp[g], p)
			}
			baseAdds, baseRm := evalSet(picks)
			type move struct {
				key   string
				picks []pick
				cost  int64
			}
			moves := []move{{key: "opt:cur", picks: picks, cost: curCost}}
			optEvals := []eval{{key: "opt:cur", adds: baseAdds, rm: baseRm, noMC: true}}
			for _, g := range groupKeys {
				grpCost := int64(0)
				for _, p := range byGrp[g] {
					grpCost += p.x.price
				}
				var rest []pick
				for _, p := range picks {
					if groupOf(p.x.slot) != g {
						rest = append(rest, p)
					}
				}
				remBudget := budget - (curCost - grpCost)
				for oi, opt := range topOpts[g] {
					if opt.cost > remBudget {
						continue
					}
					candPicks := append(append([]pick(nil), rest...), opt.picks...)
					adds, rm := evalSet(candPicks)
					mk := fmt.Sprintf("opt:%s:%d", g, oi)
					moves = append(moves, move{key: mk, picks: candPicks, cost: curCost - grpCost + opt.cost})
					optEvals = append(optEvals, eval{key: mk, adds: adds, rm: rm, noMC: true})
				}
			}
			if len(optEvals) <= 1 {
				break
			}
			mvals, err := valueBatch(optEvals, true)
			if err != nil {
				return nil, err
			}
			curVal := mvals["opt:cur"]
			bestIdx := 0
			bestVal := curVal
			for i := 1; i < len(moves); i++ {
				v := mvals[moves[i].key]
				if v > bestVal+0.05 || (math.Abs(v-bestVal) <= 0.05 && v >= curVal-1e-9 && moves[i].cost < moves[bestIdx].cost) {
					bestIdx, bestVal = i, v
				}
			}
			if bestIdx == 0 {
				break
			}
			picks = moves[bestIdx].picks
		}
	}

	// Sort picks into final buying order (owned first, then best gain per gold)
	// BEFORE computing prefix gains so each row's step gain and cumGain match
	// stopping at that exact point in the shopping list.
	sort.SliceStable(picks, func(a, b int) bool {
		pa, pb := picks[a].x.price, picks[b].x.price
		if (pa > 0) != (pb > 0) {
			return pb > 0
		}
		ka, kb := picks[a].x.gain, picks[b].x.gain
		if pa > 0 {
			ka = ka / (float64(pa) / 10000)
		}
		if pb > 0 {
			kb = kb / (float64(pb) / 10000)
		}
		if ka != kb {
			return ka > kb
		}
		return pa < pb
	})

	var pvals map[string]float64
	if len(picks) > 0 {
		var prefixAdds []Stats
		var emptied []int64
		var prefixEvals []eval
		for i, p := range picks {
			for _, s := range p.remove {
				if _, ok := eq[s]; ok && !containsSlot(emptied, s) {
					emptied = append(emptied, s)
				}
			}
			if twoHanded && p.x.slot == 17 {
				if _, ok := eq[16]; ok && !containsSlot(emptied, 16) {
					emptied = append(emptied, 16)
				}
			}
			prefixAdds = append(prefixAdds, p.x.norm)
			prefixEvals = append(prefixEvals, eval{fmt.Sprintf("px:%d", i),
				append([]Stats(nil), prefixAdds...), append([]int64(nil), emptied...), false})
		}
		pvals, err = valueBatch(prefixEvals, eng != nil)
		if err != nil {
			return nil, err
		}
	}
	cost := int64(0)
	for _, p := range picks {
		cost += p.x.price
	}
	pkgList := make([]any, 0, len(picks))
	value := 0.0
	cumCost := int64(0)
	for i, p := range picks {
		next := pvals[fmt.Sprintf("px:%d", i)]
		step := next - value
		value = next
		cumCost += p.x.price
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
			label := SlotLabel(int64(oldSlot))
			compare = Tooltip(old, &label)
		}
		row := pyjson.New("slot", p.x.slot, "slotName", SlotLabel(p.x.slot), "replaces", replaced, "compareTip", compare)
		out := itemOut(p.x, step, p.remove)
		for _, k := range out.Keys() {
			row.Set(k, out.Get(k))
		}
		row.Set("cumCost", cumCost)
		row.Set("cumGain", pyfmt.Round(value, 1))
		pkgList = append(pkgList, row)
	}
	baseValue := base
	if eng != nil {
		baseValue = 0
		if eng.Baseline != nil {
			baseValue = eng.Baseline.Value
		}
	}
	var pkgPct any
	if baseValue != 0 {
		pkgPct = pyfmt.RoundInt(value / baseValue * 100)
	}
	pkgOut := pyjson.New("cost", cost, "gain", pyfmt.Round(value, 1), "gainPct", pkgPct, "items", pkgList)

	// What the header shows: the engine's combat model when it ran, the weight scale otherwise.
	weightsText := scale.Text
	scaleRows := make([]any, len(scale.Rows))
	for i, r := range scale.Rows {
		scaleRows[i] = r
	}
	var engineOut any
	if eng != nil {
		weightsText, scaleRows, engineOut = engineSection(eng, c, classFile, o)
	}

	sort.SliceStable(review, func(a, b int) bool { return review[a].price < review[b].price })
	reviewList := []any{}
	for i, r := range review {
		if i == 15 {
			break
		}
		reviewList = append(reviewList, r.obj)
	}
	unknown := make([]any, len(ownedUnknown))
	for i, ow := range ownedUnknown {
		unknown[i] = pyjson.New("id", ow.ID, "name", ow.It.Get("name"), "source", ow.Source, "owner", strOrNil(ow.Owner),
			"where", WhereText(ow.Source, ow.Owner))
	}
	scan := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(last))
	return pyjson.New(
		"scanDate", scan.Format("2006-01-02"),
		"exportUpdated", info.Get("updated"), "exportCount", len(items), "exportMissing", info.Tab("missing").Len(),
		"level", levelOut, "budget", budget, "character", filepath.Base(filepath.Dir(filepath.Dir(files[0]))),
		"weights", weightsText, "profile", scale.Profile, "profileLabel", scale.Label, "scale", scaleRows,
		"setValue", pyfmt.Round(baseValue, 1), "sort", o.Sort, "engine", engineOut, "package", pkgOut,
		"slots", slotsOut, "review", reviewList,
		"owned", pyjson.New("count", len(have), "unknown", unknown)), nil
}

func bestSwapVal(values map[string]float64, idx int, x *cand) float64 {
	best := math.Inf(-1)
	for j := range x.variants {
		if v := values[fmt.Sprintf("sw:%d:%d", idx, j)]; v > best {
			best = v
		}
	}
	return best
}

func containsSlot(xs []int64, s int64) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// lessSlots orders slot tuples as Python orders tuples.
func lessSlots(a, b []int64) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// startEngine is the engine part of upgrades.compute: the bundle from the snapshot, with the fight asked for.
// The second result is the set of consumables (Options.FightBuffs) when one was asked for and found.
func startEngine(c *sv.Table, classFile string, totals Stats, scale *Scale, o Options) (*enginebundle.Engine,
	*Consumed, error) {
	if !enginebundle.Supported(classFile) {
		cls := classFile
		if cls == "" {
			cls = "?"
		}
		return nil, nil, errors.New(i18n.T("The combat engine does not model this class yet ({cls}).", "cls", cls))
	}
	st := c.Tab("character").Tab("stats")
	lifeTap := engine.LifeTap{}
	if m := LifeTapText.FindStringSubmatch(st.Tab("lifeTap").Str("text")); m != nil && Number(m[1]) > 0 {
		lifeTap = engine.LifeTap{HealthPerCast: Number(m[1]), ManaPerCast: Number(m[2])}
	}
	// Kills per minute feed buffs from killing blows (Spirit Tap); negative = the engine decides.
	fight := engine.Fight{Seconds: o.FightSeconds, TargetDelta: o.TargetDelta, KillsPerMinute: -1}
	if o.TargetDelta < 0 || o.TargetDelta > 3 {
		fight.TargetDelta = Profiles[scale.Profile].Target
	}
	if o.KillsPerMinute >= 0 {
		fight.KillsPerMinute = o.KillsPerMinute
	}
	consumed := o.Consumables
	if o.FightBuffs != nil {
		var err error
		if consumed, err = o.FightBuffs(o.Metric, fight.Seconds, fight.TargetDelta); err != nil {
			return nil, nil, err
		}
	}
	var buffs map[string]float64
	if consumed != nil {
		buffs = consumed.Delta
	}
	eng, err := enginebundle.Start(c, totals["mp5"], totals["hp5"], fight, o.Metric, lifeTap,
		enginebundle.Options{Method: o.Method, Iterations: o.Iterations, Buffs: buffs})
	return eng, consumed, err
}
