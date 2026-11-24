package recipes

import (
	"sort"
	"strconv"

	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// HistoryDays is recipes.HISTORY_DAYS: the profit history shows this many full scan days.
const HistoryDays = 8

// FullScanShare is recipes.FULL_SCAN_SHARE: a day counts as a full scan when it holds this share of the
// biggest scan day.
var FullScanShare = 0.3

// VendorItems are recipes.VENDOR_ITEMS: reagents every trade or general goods vendor sells. Price =
// ItemSparse.BuyPrice / VendorStackCount, until the addon reads the real price at a vendor.
var VendorItems = map[int64]bool{
	3371: true, 3372: true, 8925: true, 18256: true, // vials
	2678: true, 2692: true, 3713: true, 159: true, 1179: true, 2596: true, 4289: true, 8150: true, // cooking
	2320: true, 2321: true, 4291: true, 8343: true, 14341: true, // threads
	2324: true, 2604: true, 6260: true, 4340: true, 4341: true, 4342: true, 2325: true, 10290: true, // dyes
	3857: true, 2880: true, 3466: true, 18567: true, // coal, flux
	6217: true, 4470: true, 4399: true, 4400: true, 10647: true, 10648: true, // rod, wood, kegs, gelatin, parchment
	6530: true, // nightcrawlers
}

// Market is recipes.Market: Auctionator's prices (sv.Prices), the vendor prices the addon saw
// (WoWToolsForever_Account.vendor of every account, merged) and the catalog's items for the trade vendors' prices.
type Market struct {
	Prices  *sv.Table
	Vendor  *sv.Table
	Catalog *Catalog // only its items are used; nil = no items ({} in Python)
	// LastDay is the newest day of any "a" table (days since 2020-01-01), 0 without one.
	LastDay int64
	// ScanDays are the last HistoryDays full scan days, ascending.
	ScanDays []int64
}

// entry is prices.get(key) when it is a dict.
func entry(prices *sv.Table, iid int64) *sv.Table {
	e, ok := prices.Get(strconv.FormatInt(iid, 10)).(*sv.Table)
	if !ok || e.IsList() {
		return nil
	}
	return e
}

// tab is `e.get(k) or {}`.
func tab(e *sv.Table, k string) *sv.Table {
	t, _ := e.Get(k).(*sv.Table)
	return t
}

// dayKeys are int(k) for k in table (the keys of a dict, the values of a list); a key that is no number
// is skipped (Python would stop with a ValueError).
func dayKeys(t *sv.Table) []int64 {
	var out []int64
	for _, k := range t.Iter() {
		switch x := k.(type) {
		case int64:
			out = append(out, x)
		case string:
			if n, ok := pyfmt.ParseInt(x); ok {
				out = append(out, n)
			}
		}
	}
	return out
}

// dayGet is t.get(str(day)).
func dayGet(t *sv.Table, day int64) any {
	if t.IsList() {
		return nil
	}
	return t.Get(strconv.FormatInt(day, 10))
}

func num(v any) float64 {
	f, _ := sv.Num(v)
	return f
}

// NewMarket is recipes.Market(prices, vendor, items): counts the offers per day to find the last scan day
// and the full scan days. catalog may be nil (consumables passes no items).
func NewMarket(prices, vendor *sv.Table, catalog *Catalog) *Market {
	if vendor == nil {
		vendor = sv.NewTable()
	}
	m := &Market{Prices: prices, Vendor: vendor, Catalog: catalog}
	days := map[int64]int{}
	for _, v := range prices.Values() {
		e, ok := v.(*sv.Table)
		if !ok || e.IsList() {
			continue
		}
		for _, d := range dayKeys(tab(e, "a")) {
			days[d]++
		}
	}
	biggest, first := 0, true
	for d, n := range days {
		if first || d > m.LastDay {
			m.LastDay, first = d, false
		}
		biggest = max(biggest, n)
	}
	// Auctionator also writes day prices for a single search. Only full scans count for the history.
	var full []int64
	for d, n := range days {
		if float64(n) >= FullScanShare*float64(biggest) {
			full = append(full, d)
		}
	}
	sort.Slice(full, func(i, j int) bool { return full[i] < full[j] })
	if len(full) > HistoryDays {
		full = full[len(full)-HistoryDays:]
	}
	m.ScanDays = full
	return m
}

// AH is what Market.ah returns for an item with a price: the lowest price at the last scan, the amount
// listed that day, the age of that day in days (nil = None, when there is no day) and the median of the day
// prices of the last 7 scan days (as an int, or the price itself without day prices). The values are as
// Python has them: ints stay ints.
type AH struct {
	Price  any // e["m"]
	Listed any // offers on the item's newest day, 0 without
	Age    any // int64 or nil
	Median any // int64, or e["m"]
}

// PriceF is the price as a float.
func (a *AH) PriceF() float64 { return num(a.Price) }

// AH is Market.ah: nil (None) when the item has no price.
func (m *Market) AH(iid int64) *AH {
	e := entry(m.Prices, iid)
	if e == nil || !sv.Truthy(e.Get("m")) {
		return nil
	}
	avail := tab(e, "a")
	day := int64(0)
	for i, d := range dayKeys(avail) {
		if i == 0 || d > day {
			day = d
		}
	}
	var recent []float64
	h := tab(e, "h")
	if !h.IsList() {
		for i, k := range h.Keys() {
			s, ok := k.(string)
			if !ok {
				continue
			}
			d, ok := pyfmt.ParseInt(s)
			p := h.Values()[i]
			if ok && d > m.LastDay-7 && sv.Truthy(p) {
				recent = append(recent, num(p))
			}
		}
	}
	a := &AH{Price: e.Get("m"), Listed: int64(0), Median: e.Get("m")}
	if v := dayGet(avail, day); v != nil {
		a.Listed = v
	}
	if day != 0 {
		a.Age = m.LastDay - day
	}
	if len(recent) > 0 {
		a.Median = int64(pyfmt.Median(recent)) // int() truncates toward zero
	}
	return a
}

// DayPrice is Market.day_price: the AH price on a scan day, the middle of the day's highest and lowest
// minimum price (Auctionator stores only these two), on the last scan day the current minimum price as in
// AH. 0 stands for None (both are falsy to every caller).
func (m *Market) DayPrice(iid, day int64) float64 {
	e := entry(m.Prices, iid)
	if e == nil {
		return 0
	}
	high := dayGet(tab(e, "h"), day)
	if !sv.Truthy(high) {
		return 0
	}
	if day == m.LastDay && sv.Truthy(e.Get("m")) {
		return num(e.Get("m"))
	}
	low := dayGet(tab(e, "l"), day)
	if sv.Truthy(low) {
		return (num(high) + num(low)) / 2
	}
	return num(high)
}

// DayListed is Market.day_listed: the offers of an item on a day, 0 without.
func (m *Market) DayListed(iid, day int64) any {
	e := entry(m.Prices, iid)
	if e == nil {
		return int64(0)
	}
	v := dayGet(tab(e, "a"), day)
	if !sv.Truthy(v) {
		return int64(0)
	}
	return v
}

// seen is `vendor.get(iid) or vendor.get(str(iid))`.
func (m *Market) seen(iid int64) any {
	if v := m.Vendor.Get(iid); sv.Truthy(v) {
		return v
	}
	return m.Vendor.Get(strconv.FormatInt(iid, 10))
}

// VendorPrice is Market.vendor_price: the price per piece and the vendor NPC (nil = None) when a vendor
// sells the item without a stock limit; price 0 (None) otherwise.
func (m *Market) VendorPrice(iid int64) (float64, any) {
	if s, ok := m.seen(iid).(*sv.Table); ok && !s.IsList() && sv.Truthy(s.Get("price")) && !sv.Truthy(s.Get("limited")) {
		return num(s.Get("price")), s.Get("npc")
	}
	it := m.Catalog.Item(iid)
	if VendorItems[iid] && it.Buy != 0 {
		return float64(it.Buy) / float64(it.BuyStack), nil
	}
	return 0, nil
}

// Reagent is what Market.reagent returns: the cheapest way to buy, "ah" or "vendor" (with the vendor's
// name when the addon saw one). Unit is nil (None) when the item has no price.
type Reagent struct {
	Unit   *float64
	Source any // "ah", "vendor" or nil
	Vendor any // the NPC or nil
	Listed any // offers in the AH, 0 without
}

// Reagent is Market.reagent.
func (m *Market) Reagent(iid int64) Reagent {
	ah := m.AH(iid)
	vp, npc := m.VendorPrice(iid)
	type option struct {
		price  float64
		source string
		npc    any
	}
	var options []option
	if ah != nil {
		options = append(options, option{ah.PriceF(), "ah", nil})
	}
	if vp != 0 {
		options = append(options, option{vp, "vendor", npc})
	}
	if len(options) == 0 {
		return Reagent{Listed: int64(0)}
	}
	best := options[0]
	for _, o := range options[1:] {
		if o.price < best.price {
			best = o
		}
	}
	r := Reagent{Unit: &best.price, Source: best.source, Vendor: best.npc, Listed: int64(0)}
	if ah != nil {
		r.Listed = ah.Listed
	}
	return r
}

// OwnedCounts is recipes.owned_counts: item -> pieces in the bags and the bank of a snapshot.
func OwnedCounts(c *sv.Table) map[int64]int64 {
	out := map[int64]int64{}
	var containers []*sv.Table
	if bags, ok := c.Get("bags").(*sv.Table); ok {
		containers = append(containers, bags.Tables()...)
	}
	if bank, ok := c.Get("bank").(*sv.Table); ok {
		if list, ok := bank.Get("containers").(*sv.Table); ok {
			containers = append(containers, list.Tables()...)
		}
	}
	for _, bag := range containers {
		items, ok := bag.Get("items").(*sv.Table)
		if !ok {
			continue
		}
		for _, it := range items.Tables() {
			if !sv.Truthy(it.Get("id")) {
				continue
			}
			id := int64(num(it.Get("id")))
			n := int64(1)
			if sv.Truthy(it.Get("count")) {
				n = int64(num(it.Get("count")))
			}
			out[id] += n
		}
	}
	return out
}
