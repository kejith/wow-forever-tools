// Package writs is the Go side of scripts/writs.py: the best craftsman's writ per tier (Journeyman / Expert /
// Artisan) by net silver per reputation, and the trade crates by silver per Merchant's Favor.
//
// Calc returns the result as typed values (for the lists in the game, which read it); Compute is the bridge
// command `writs`, the same result as the JSON object writs.compute returns.
package writs

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

// Options are the arguments of writs.compute. Prices, Extras and Quests are read from the game folder and
// the cache when nil (the tests and the lists for the game pass them).
type Options struct {
	Wow            string
	Char           string // part of the character folder's name; "" for the most recent snapshot
	Top            int    // alternatives per tier (a Python slice end: negative counts from the end)
	OwnedAtAHPrice bool   // count own items at their AH price instead of free
	Prices         *sv.Table
	Extras         *Extras
	Quests         *QuestOf
}

// Writ is one writ of a tier (a row of writs.compute's tiers[].best).
type Writ struct {
	Tier           string
	Name           string // the item the writ asks for
	Prof           string
	Cost           int64 // writ + item
	Payout         int64 // the coin pouch
	Net            int64 // cost - payout
	Per            float64
	PayoutSource   string
	PayoutMeasured bool
	WCost, ICost   int64
	Writs          int64 // writs in the AH
	Note           string
	Qty            int64
	WritID, ItemID int64
	QuestID        *int64
}

// Tier is one writ tier with its best writs.
type Tier struct {
	Tier   string
	Name   string
	Rep    int64
	Best   []*Writ
	Sealed int64 // sealed writs of this tier the character has
}

// Bundle is one way to fill a crate.
type Bundle struct {
	ID     int64
	Name   string
	Qty    int64
	Unit   *int64 // AH price of one; nil without one
	Cost   *int64 // nil: no AH price and not owned
	Listed int64
	Owned  int64
	Enough bool
}

// Crate is one crate kind of a tier with its cheapest bundle.
type Crate struct {
	CrateID     int64
	Kind        string
	KindKey     string
	Name        string
	CrateCost   *int64 // nil: the empty crate is neither owned nor in the AH
	CrateListed int64
	HaveCrate   bool
	Options     []*Bundle
	Best        *Bundle
	Cost        int64
	Complete    bool
	Per         *float64
	PerGreen    *float64
	Note        string
}

// CrateTier is one crate tier.
type CrateTier struct {
	Tier        string
	Name        string
	Level       int64
	TooLow      bool
	Favor       any // int64 from the table, float64 when measured
	FavorGreen  any
	FavorSource string
	FavorGuess  bool
	SealedID    int64
	Rows        []*Crate
}

// RankedCrate is a crate of the overall cheapest Merchant's Favor, with its tier.
type RankedCrate struct {
	*Crate
	Tier     string
	TierName string
	Favor    any
}

// Crates are the trade crates per tier and the cheapest favor over all tiers.
type Crates struct {
	Tiers []*CrateTier
	Best  []*RankedCrate
}

// Result is what writs.compute returns.
type Result struct {
	ScanDate   string // "" without a scan
	Character  string // the character folder, "" without a snapshot
	Reputation *Reputation
	Tiers      []*Tier
	DailyTotal int64
	DailyNet   int64
	DailyRep   int64
	Crates     Crates
	Favor      any // the character's Merchant's Favor, nil if unknown
	Level      any
}

// Compute is writs.compute for the bridge: the result as the JSON object Python returns.
func Compute(wow, char string, top int, ownedAtAHPrice bool, prices *sv.Table) (*pyjson.Obj, error) {
	r, err := Calc(Options{Wow: wow, Char: char, Top: top, OwnedAtAHPrice: ownedAtAHPrice, Prices: prices})
	if err != nil {
		return nil, err
	}
	return r.Obj(), nil
}

// LoadPrices is load_prices(find_auctionator(wow)) with Python's errors.
func LoadPrices(wow string) (*sv.Table, error) {
	path := game.FindAuctionator(wow)
	if path == "" {
		return nil, errors.New(i18n.T("Auctionator.lua not found under {wow}", "wow", wow))
	}
	prices, err := sv.Prices(path)
	if errors.Is(err, sv.ErrNoPrices) {
		return nil, errors.New(i18n.T("No price data found in Auctionator.lua."))
	}
	return prices, err
}

type price struct {
	m      int64
	ok     bool
	offers int64
}

// priceOf is compute's price(): the minimum price (none when missing or 0) and the offers of the newest
// scan day.
func priceOf(prices *sv.Table, id int64) price {
	e, _ := prices.Get(strconv.FormatInt(id, 10)).(*sv.Table)
	if e == nil || e.IsList() || !sv.Truthy(e.Get("m")) {
		return price{}
	}
	p := price{m: toInt(e.Get("m")), ok: true}
	if a, _ := e.Get("a").(*sv.Table); sv.Truthy(a) {
		var day any
		best := int64(0)
		for _, k := range a.Iter() {
			n, ok := pyfmt.ParseInt(pyfmt.Str(k))
			if !ok {
				continue
			}
			if day == nil || n > best { // max() keeps the first of equal ones
				day, best = k, n
			}
		}
		if v := a.Get(day); sv.Truthy(v) {
			p.offers = toInt(v)
		}
	}
	return p
}

// toInt is a whole number read from a file (Lua and CBOR write prices and counts as integers).
func toInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

// sliceEnd is the end of Python's xs[:top] for a list of n.
func sliceEnd(n, top int) int {
	if top < 0 {
		top += n
		if top < 0 {
			return 0
		}
		return top
	}
	if top > n {
		return n
	}
	return top
}

// sampleAvg is writs.sample_avg: sum / n of a measured sample, false without one.
func sampleAvg(v any) (float64, bool) {
	s, _ := v.(*sv.Table)
	if s == nil || s.IsList() || !sv.Truthy(s.Get("n")) {
		return 0, false
	}
	sum, _ := sv.Num(s.Get("sum"))
	n, _ := sv.Num(s.Get("n"))
	return sum / n, true
}

type payoutFunc func(wid int64, tier string) (int64, string, bool)

// pouchValues is writs.pouch_values: the coin pouch of a writ from the measured pouches, the pouch the
// other writs of its tier gave when it has no turn-in of its own, else the estimate.
func pouchValues(e *Extras, quests *QuestOf) payoutFunc {
	tierOf := map[int64]string{}
	for _, w := range WritList {
		tierOf[w.ID] = w.Tier
	}
	type turnIn struct {
		pouch any
		money any
	}
	perWrit := map[int64]turnIn{}
	type votes struct {
		order []any
		n     map[any]int
	}
	tierPouch := map[string]*votes{}
	for _, wid := range quests.Writs {
		rec, _ := e.WritQuests.Get(quests.Quest[wid]).(*sv.Table)
		if rec == nil || rec.IsList() || !sv.Truthy(rec.Get("pouch")) {
			continue
		}
		pouch := rec.Get("pouch")
		money := rec.Get("money")
		if !sv.Truthy(money) {
			money = int64(0)
		}
		perWrit[wid] = turnIn{pouch, money}
		v := tierPouch[tierOf[wid]]
		if v == nil {
			v = &votes{n: map[any]int{}}
			tierPouch[tierOf[wid]] = v
		}
		if _, ok := v.n[pouch]; !ok {
			v.order = append(v.order, pouch)
		}
		v.n[pouch]++
	}
	return func(wid int64, tier string) (int64, string, bool) {
		t, has := perWrit[wid]
		pouch, money := t.pouch, any(int64(0))
		if has {
			money = t.money
		}
		if !has {
			if v := tierPouch[tier]; v != nil {
				for _, p := range v.order {
					if pouch == nil || v.n[p] > v.n[pouch] {
						pouch = p
					}
				}
			}
		}
		var s any
		if sv.Truthy(pouch) {
			s = e.Pouches.Get(pouch)
		}
		if avg, ok := sampleAvg(s); ok {
			st := s.(*sv.Table)
			lo, _ := sv.Num(st.Get("min"))
			hi, _ := sv.Num(st.Get("max"))
			source := i18n.T("measured: coin pouch opened {n}x, {low} to {high}", "n", st.Get("n"),
				"low", game.Money(lo), "high", game.Money(hi))
			return pyfmt.RoundInt(avg) + toInt(money), source, true
		}
		return PouchEstimate[tier] + toInt(money), i18n.T(pouchEstimateSource), false
	}
}

// Calc is writs.compute: the best writs per tier by net cost per reputation, and the trade crates by cost
// per Merchant's Favor. Amounts are copper.
func Calc(o Options) (*Result, error) {
	if err := loadNames(); err != nil {
		return nil, err
	}
	prices := o.Prices
	if prices == nil {
		var err error
		if prices, err = LoadPrices(o.Wow); err != nil {
			return nil, err
		}
	}
	chars := game.FindChars(o.Wow, o.Char)
	owned := map[int64]int64{}
	var rep *Reputation
	r := &Result{}
	charPath := ""
	if len(chars) > 0 {
		charPath = chars[0]
		var err error
		if owned, rep, err = LoadOwned(charPath); err != nil {
			return nil, err
		}
		r.Character = game.CharKey(charPath)
	}
	extras := o.Extras
	if extras == nil {
		var err error
		if extras, err = LoadExtras(o.Wow, charPath); err != nil {
			return nil, err
		}
	}
	quests := o.Quests
	if quests == nil {
		var err error
		if quests, err = WritQuests(""); err != nil {
			return nil, err
		}
	}
	payoutOf := pouchValues(extras, quests)

	var rows []*Writ
	for _, w := range WritList {
		wp, ip := priceOf(prices, w.ID), priceOf(prices, w.Item)
		haveWrit := owned[w.ID] > 0
		haveItem := owned[w.Item] >= w.Qty
		if (!wp.ok && !haveWrit) || (!ip.ok && !haveItem) {
			continue
		}
		wcost := wp.m
		if haveWrit {
			wcost = 0
		}
		icost := ip.m * w.Qty
		if haveItem && !o.OwnedAtAHPrice {
			icost = 0
		}
		cost := wcost + icost
		payout, source, measured := payoutOf(w.ID, w.Tier)
		net := cost - payout
		var note []string
		if haveWrit {
			note = append(note, i18n.T("you own the writ"))
		}
		if haveItem {
			note = append(note, i18n.T("you own the item ({n}x)", "n", owned[w.Item]))
		}
		if !haveItem && ip.offers < w.Qty*10 {
			note = append(note, i18n.T("only {n} items in the AH", "n", ip.offers))
		}
		row := &Writ{Tier: w.Tier, Name: ItemName(w.Item), Prof: i18n.T(profession[w.Profession]), Cost: cost,
			Payout: payout, Net: net, Per: float64(net) / float64(Rep[w.Tier]), PayoutSource: source,
			PayoutMeasured: measured, WCost: wcost, ICost: icost, Writs: wp.offers, Note: strings.Join(note, ", "),
			Qty: w.Qty, WritID: w.ID, ItemID: w.Item}
		if q, ok := quests.Quest[w.ID]; ok {
			row.QuestID = &q
		}
		rows = append(rows, row)
	}

	for _, t := range []string{"J", "E", "A"} {
		var best []*Writ
		for _, w := range rows {
			if w.Tier == t {
				best = append(best, w)
			}
		}
		sort.SliceStable(best, func(i, j int) bool { return best[i].Per < best[j].Per })
		best = best[:sliceEnd(len(best), o.Top)]
		r.Tiers = append(r.Tiers, &Tier{Tier: t, Name: i18n.T(tierName[t]), Rep: Rep[t], Best: best,
			Sealed: owned[Sealed[t]]})
	}
	for _, t := range r.Tiers {
		if len(t.Best) > 0 {
			r.DailyTotal += t.Best[0].Cost
			r.DailyNet += t.Best[0].Net
		}
	}
	r.DailyRep = Rep["J"] + Rep["E"] + Rep["A"]
	r.Reputation = rep
	if day, ok := sv.ScanDate(prices); ok {
		r.ScanDate = day.Format("2006-01-02")
	}
	r.Crates = crates(prices, owned, o.OwnedAtAHPrice, extras, o.Top)
	r.Favor, r.Level = extras.Favor, extras.Level
	return r, nil
}

// crates is writs.crates: per crate tier every crate kind with its cheapest bundle and the cost per point
// of Merchant's Favor.
func crates(prices *sv.Table, owned map[int64]int64, ownedAtAHPrice bool, e *Extras, top int) Crates {
	level, hasLevel := sv.Num(e.Level)
	var out Crates
	for _, tr := range CrateTiers {
		t := string(tr)
		common, uncommon := CrateFavor[t][0], CrateFavor[t][1]
		sc, su := SealedCrate[t][0], SealedCrate[t][1]
		mc, okC := sampleAvg(e.Crates.Get(sc))
		mu, okU := sampleAvg(e.Crates.Get(su))
		favor, favorGreen := float64(common), float64(uncommon)
		var favorV, greenV any = common, uncommon
		if okC {
			favor, favorV = mc, mc
		}
		if okU {
			favorGreen, greenV = mu, mu
		}
		var source string
		switch {
		case okC:
			source = i18n.T("measured: handed in {n}x", "n", e.Crates.Tab(sc).Get("n"))
		case CrateFavorGuess[t]:
			source = i18n.T("estimated, extrapolated from Apprentice 5, Journeyman 10, Expert 15")
		default:
			source = i18n.T("from urgametips.com and wow.gg (Nov 2026)")
		}
		var rows []*Crate
		for _, c := range CrateList {
			if c.Tier != t {
				continue
			}
			cp := priceOf(prices, c.ID)
			haveCrate := owned[c.ID] > 0
			var opts []*Bundle
			for _, b := range c.Bundles {
				iid, qty := b[0], b[1]
				ip := priceOf(prices, iid)
				have := owned[iid] >= qty
				o := &Bundle{ID: iid, Name: ItemName(iid), Qty: qty, Listed: ip.offers, Owned: owned[iid],
					Enough: have || ip.offers >= qty}
				if ip.ok {
					u := ip.m
					o.Unit = &u
				}
				switch {
				case have && !ownedAtAHPrice:
					zero := int64(0)
					o.Cost = &zero
				case ip.ok:
					cost := ip.m * qty
					o.Cost = &cost
				}
				opts = append(opts, o)
			}
			// Cheapest bundle the AH can actually fill; a short supply only when nothing else is there.
			var best *Bundle
			for _, o := range opts {
				if o.Cost == nil {
					continue
				}
				if best == nil || bundleLess(o, best) {
					best = o
				}
			}
			var crateCost *int64
			if haveCrate {
				zero := int64(0)
				crateCost = &zero
			} else if cp.ok {
				m := cp.m
				crateCost = &m
			}
			var note []string
			if haveCrate {
				note = append(note, i18n.T("you own the crate ({n}x)", "n", owned[c.ID]))
			} else if !cp.ok {
				note = append(note, i18n.T("empty crate not in the AH (drops from enemies)"))
			}
			switch {
			case best == nil:
				note = append(note, i18n.T("no bundle with an AH price"))
			case best.Owned >= best.Qty:
				note = append(note, i18n.T("you own the bundle ({n}x {name})", "n", best.Owned, "name", best.Name))
			case !best.Enough:
				note = append(note, i18n.T("only {n}x {name} in the AH", "n", best.Listed, "name", best.Name))
			}
			complete := best != nil && crateCost != nil
			var cost int64
			if crateCost != nil {
				cost = *crateCost
			}
			if best != nil {
				cost += *best.Cost
			}
			row := &Crate{CrateID: c.ID, Kind: i18n.T(crateKind[c.Kind]), KindKey: c.Kind, Name: ItemName(c.ID),
				CrateCost: crateCost, CrateListed: cp.offers, HaveCrate: haveCrate, Options: opts, Best: best,
				Cost: cost, Complete: complete, Note: strings.Join(note, ", ")}
			if complete {
				per, green := float64(cost)/favor, float64(cost)/favorGreen
				row.Per, row.PerGreen = &per, &green
			}
			rows = append(rows, row)
		}
		sort.SliceStable(rows, func(i, j int) bool { return crateLess(rows[i], rows[j]) })
		out.Tiers = append(out.Tiers, &CrateTier{Tier: t, Name: i18n.T(crateTierName[t]), Level: CrateLevel[t],
			TooLow: hasLevel && level < float64(CrateLevel[t]), Favor: favorV, FavorGreen: greenV,
			FavorSource: source, FavorGuess: CrateFavorGuess[t] && !okC, SealedID: sc, Rows: rows})
	}
	var ranked []*RankedCrate
	for _, t := range out.Tiers {
		for _, c := range t.Rows {
			if c.Complete && !t.TooLow {
				ranked = append(ranked, &RankedCrate{Crate: c, Tier: t.Tier, TierName: t.Name, Favor: t.Favor})
			}
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return *ranked[i].Per < *ranked[j].Per })
	out.Best = ranked[:sliceEnd(len(ranked), top)]
	return out
}

// bundleLess is the key of min(priced, key=lambda o: (not o['enough'], o['cost'])).
func bundleLess(a, b *Bundle) bool {
	if a.Enough != b.Enough {
		return a.Enough
	}
	return *a.Cost < *b.Cost
}

// crateLess is the key of rows.sort(key=lambda r: (not r['complete'], r['per'] if r['complete'] else
// r['cost'])).
func crateLess(a, b *Crate) bool {
	if a.Complete != b.Complete {
		return a.Complete
	}
	if a.Complete {
		return *a.Per < *b.Per
	}
	return a.Cost < b.Cost
}

func intPtr(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func floatPtr(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func orNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Obj is the writ as writs.compute returns it.
func (w *Writ) Obj() *pyjson.Obj {
	return pyjson.New("tier", w.Tier, "name", w.Name, "prof", w.Prof, "cost", w.Cost, "payout", w.Payout,
		"net", w.Net, "per", w.Per, "payoutSource", w.PayoutSource, "payoutMeasured", w.PayoutMeasured,
		"wcost", w.WCost, "icost", w.ICost, "writs", w.Writs, "note", w.Note, "qty", w.Qty, "writId", w.WritID,
		"itemId", w.ItemID, "questId", intPtr(w.QuestID))
}

// Obj is the bundle as writs.crates returns it.
func (b *Bundle) Obj() *pyjson.Obj {
	if b == nil {
		return nil
	}
	return pyjson.New("id", b.ID, "name", b.Name, "qty", b.Qty, "unit", intPtr(b.Unit), "cost", intPtr(b.Cost),
		"listed", b.Listed, "owned", b.Owned, "enough", b.Enough)
}

// Obj is the crate row as writs.crates returns it.
func (c *Crate) Obj() *pyjson.Obj {
	opts := make([]any, 0, len(c.Options))
	for _, o := range c.Options {
		opts = append(opts, o.Obj())
	}
	var best any
	if c.Best != nil {
		best = c.Best.Obj()
	}
	return pyjson.New("crateId", c.CrateID, "kind", c.Kind, "kindKey", c.KindKey, "name", c.Name,
		"crateCost", intPtr(c.CrateCost), "crateListed", c.CrateListed, "haveCrate", c.HaveCrate,
		"options", opts, "best", best, "cost", c.Cost, "complete", c.Complete, "per", floatPtr(c.Per),
		"perGreen", floatPtr(c.PerGreen), "note", c.Note)
}

// Obj is the result as writs.compute returns it.
func (r *Result) Obj() *pyjson.Obj {
	tiers := []any{}
	for _, t := range r.Tiers {
		best := []any{}
		for _, w := range t.Best {
			best = append(best, w.Obj())
		}
		tiers = append(tiers, pyjson.New("tier", t.Tier, "name", t.Name, "rep", t.Rep, "best", best,
			"sealed", t.Sealed))
	}
	var rep any
	if r.Reputation != nil {
		rep = pyjson.New("name", r.Reputation.Name, "standing", r.Reputation.Standing,
			"value", r.Reputation.Value, "max", r.Reputation.Max)
	}
	crateTiers := []any{}
	for _, t := range r.Crates.Tiers {
		rows := []any{}
		for _, c := range t.Rows {
			rows = append(rows, c.Obj())
		}
		crateTiers = append(crateTiers, pyjson.New("tier", t.Tier, "name", t.Name, "level", t.Level,
			"tooLow", t.TooLow, "favor", t.Favor, "favorGreen", t.FavorGreen, "favorSource", t.FavorSource,
			"favorGuess", t.FavorGuess, "sealedId", t.SealedID, "rows", rows))
	}
	ranked := []any{}
	for _, c := range r.Crates.Best {
		ranked = append(ranked, c.Crate.Obj().Set("tier", c.Tier).Set("tierName", c.TierName).Set("favor", c.Favor))
	}
	return pyjson.New("scanDate", orNil(r.ScanDate), "character", orNil(r.Character), "reputation", rep,
		"tiers", tiers, "dailyTotal", r.DailyTotal, "dailyNet", r.DailyNet, "dailyRep", r.DailyRep,
		"crates", pyjson.New("tiers", crateTiers, "best", ranked), "favor", r.Favor, "level", r.Level)
}
