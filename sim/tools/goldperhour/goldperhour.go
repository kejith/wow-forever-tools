// Package goldperhour is the Go side of GoldPerHour: sessions, always-on daily loot & cashflow,
// auction house sales, dungeon runs, top drops and character/account wealth history stored in
// GoldPerHourDB (account SavedVariables GoldPerHour.lua) for the desktop app.
package goldperhour

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// Categories are the loot/gathering income categories the addon tells apart.
var Categories = []string{"herb", "mining", "skinning", "fishing", "loot", "dungeon"}

// HerbGathering is the spell ID of Herb Gathering.
const HerbGathering = 2366

// MiningBase is the base spell ID of Mining.
const MiningBase = 2575

// defaults are DEFAULTS in GoldPerHour.lua, in their order.
var defaults = []struct {
	key string
	val any
}{
	{"scale", int64(1)},
	{"tooltipItems", int64(6)},
	{"priceSource", "auctionator"},
	{"resumeAfterReload", true},
	{"autoStart", false},
	{"deductAhCut", true},
	{"vendorFloor", true},
	{"ahMinValue", int64(0)},
	{"wealthInterval", int64(300)},
	{"minDropQuality", int64(3)},
	{"minDropValue", int64(50000)},
	{"trackAlways", true},
}

var (
	link           = regexp.MustCompile(`\|Hitem:([0-9]+)[^|]*\|h\[(.*?)\]\|h`)
	color          = regexp.MustCompile(`\|c[fF]{2}([0-9a-fA-F]{6})`)
	qualityByColor = map[string]int64{"9d9d9d": 0, "ffffff": 1, "1eff00": 2, "0070dd": 3, "a335ee": 4, "ff8000": 5}
)

func values(x any) []any {
	if t, ok := x.(*sv.Table); ok {
		return t.List()
	}
	return nil
}

func keysTrue(x any) []any {
	t, ok := x.(*sv.Table)
	if !ok {
		return nil
	}
	var out []any
	if t.IsList() {
		for i, v := range t.List() {
			if sv.Truthy(v) {
				out = append(out, int64(i+1))
			}
		}
		return out
	}
	for i, k := range t.Keys() {
		if sv.Truthy(t.Values()[i]) {
			out = append(out, k)
		}
	}
	return out
}

func num(v any) float64 {
	if b, ok := v.(bool); ok {
		if b {
			return 1
		}
		return 0
	}
	f, _ := sv.Num(v)
	return f
}

func round(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return pyfmt.RoundInt(x)
	}
	return int64(num(v))
}

func or(a, b any) any {
	if sv.Truthy(a) {
		return a
	}
	return b
}

func get(t *sv.Table, k string, def any) any {
	if t.Has(k) {
		return t.Get(k)
	}
	return def
}

func tab(v any) *sv.Table {
	if t, ok := v.(*sv.Table); ok {
		return t
	}
	return sv.NewTable()
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// PerHour is copper per hour, nil under a minute.
func PerHour(copper, elapsed float64) any {
	if elapsed != 0 && elapsed >= 60 {
		return copper / elapsed * 3600
	}
	return nil
}

// ItemEntry is an item of a session, its ID, name and quality from the link if the entry lacks them (a 0.2 file).
func ItemEntry(e *sv.Table) *pyjson.Obj {
	l, _ := or(e.Get("link"), "").(string)
	m := link.FindStringSubmatch(l)
	c := color.FindStringSubmatch(l)
	q := e.Get("quality")
	if q == nil && c != nil {
		if v, ok := qualityByColor[strings.ToLower(c[1])]; ok {
			q = v
		}
	}
	var id, name any = e.Get("id"), e.Get("name")
	if !sv.Truthy(id) {
		id = nil
		if m != nil {
			id, _ = pyfmt.ParseInt(m[1])
		}
	}
	if !sv.Truthy(name) {
		name = "?"
		if m != nil {
			name = m[2]
		}
	}
	vv := e.Get("vendorValue")
	if vv == nil {
		vv = e.Get("vendor")
	}
	return pyjson.New(
		"id", id, "name", name, "quality", q,
		"count", or(e.Get("count"), int64(0)),
		"value", round(or(e.Get("value"), int64(0))),
		"vendorValue", round(or(vv, int64(0))),
		"source", e.Get("source"),
	)
}

func zoneList(zones any) []any {
	t, ok := zones.(*sv.Table)
	if !ok || t.IsList() {
		return []any{}
	}
	out := make([]any, 0, t.Len())
	for i, z := range t.Keys() {
		out = append(out, pyjson.New("zone", z, "value", round(t.Values()[i])))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].(*pyjson.Obj).Get("value").(int64) > out[j].(*pyjson.Obj).Get("value").(int64)
	})
	return out
}

// samples is the gold curve as {t, herb, loot, gather, dungeon}.
func samples(raw any) []any {
	out := []any{}
	for _, s := range values(raw) {
		v := values(s)
		if len(v) >= 3 {
			var gather, dungeon any = int64(0), int64(0)
			if len(v) >= 4 {
				gather = v[3]
			}
			if len(v) >= 5 {
				dungeon = v[4]
			}
			out = append(out, pyjson.New("t", v[0], "herb", v[1], "loot", v[2], "gather", gather, "dungeon", dungeon))
		}
	}
	return out
}

func sortedKeys(keys []any) []any {
	out := append([]any{}, keys...)
	sort.SliceStable(out, func(i, j int) bool {
		a, aText := out[i].(string)
		b, bText := out[j].(string)
		if aText && bText {
			return a < b
		}
		if !aText && !bText {
			return num(out[i]) < num(out[j])
		}
		return false
	})
	return out
}

func sessionSummary(gold, coin, items *sv.Table, elapsed any, meta *sv.Table) *pyjson.Obj {
	var parts []any
	for _, c := range Categories {
		parts = append(parts, get(gold, c, int64(0)))
	}
	total := pyfmt.SumAny(parts...)
	cats := pyjson.New()
	var vendorTotal int64
	for _, c := range Categories {
		var lst []any
		for _, e := range values(items.Get(c)) {
			it := ItemEntry(tab(e))
			vendorTotal += it.Get("vendorValue").(int64)
			lst = append(lst, it)
		}
		sort.SliceStable(lst, func(i, j int) bool {
			a, b := lst[i].(*pyjson.Obj), lst[j].(*pyjson.Obj)
			va, vb := a.Get("value").(int64), b.Get("value").(int64)
			if va != vb {
				return va > vb
			}
			return num(or(a.Get("id"), int64(0))) < num(or(b.Get("id"), int64(0)))
		})
		counts := make([]any, len(lst))
		for i, e := range lst {
			counts[i] = e.(*pyjson.Obj).Get("count")
		}
		if lst == nil {
			lst = []any{}
		}
		g := get(gold, c, int64(0))
		cn := round(get(coin, c, int64(0)))
		vendorTotal += cn
		cats.Set(c, pyjson.New("gold", round(g), "coin", cn,
			"perHour", PerHour(num(g), num(elapsed)), "items", lst, "itemCount", pyfmt.SumAny(counts...)))
	}
	chars := sortedKeys(keysTrue(meta.Get("chars")))
	if chars == nil {
		chars = []any{}
	}
	return pyjson.New(
		"elapsed", round(elapsed), "total", round(total), "vendorTotal", vendorTotal,
		"perHour", PerHour(num(total), num(elapsed)),
		"categories", cats, "started", meta.Get("started"), "ended", or(meta.Get("ended"), meta.Get("lastActive")),
		"zones", zoneList(meta.Get("zones")), "chars", chars, "samples", samples(meta.Get("samples")),
	)
}

func parseSpellSet(raw any) ([]int64, error) {
	var spells []int64
	for _, k := range keysTrue(raw) {
		switch x := k.(type) {
		case int64:
			spells = append(spells, x)
		case string:
			n, ok := pyfmt.ParseInt(x)
			if !ok {
				return nil, fmt.Errorf("invalid literal for int() with base 10: '%s'", x)
			}
			spells = append(spells, n)
		default:
			spells = append(spells, int64(num(x)))
		}
	}
	sort.Slice(spells, func(i, j int) bool { return spells[i] < spells[j] })
	if spells == nil {
		spells = []int64{}
	}
	return spells, nil
}

func settingsOf(db *sv.Table) (*pyjson.Obj, error) {
	s := pyjson.New()
	known := map[string]bool{}
	for _, d := range defaults {
		s.Set(d.key, d.val)
		known[d.key] = true
	}
	settings := tab(or(db.Get("settings"), nil))
	for i, k := range settings.Keys() {
		if key, ok := k.(string); ok && known[key] {
			s.Set(key, settings.Values()[i])
		}
	}
	s.Set("shown", !sv.Truthy(db.Get("hidden")))
	herbSpells, err := parseSpellSet(db.Get("herbSpells"))
	if err != nil {
		return nil, err
	}
	miningSpells, err := parseSpellSet(db.Get("miningSpells"))
	if err != nil {
		return nil, err
	}
	s.Set("herbSpells", herbSpells)
	s.Set("miningSpells", miningSpells)
	return s, nil
}

// ---------- Wealth ----------

type tsPoint struct {
	t int64
	v int64
}

func parseHistoryPoints(raw any) []tsPoint {
	var pts []tsPoint
	for _, p := range values(raw) {
		v := values(p)
		if len(v) >= 2 {
			pts = append(pts, tsPoint{t: round(v[0]), v: round(v[1])})
		}
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].t < pts[j].t })
	return pts
}

func encodePoints(pts []tsPoint, valKey string) []any {
	out := make([]any, 0, len(pts))
	for _, p := range pts {
		out = append(out, pyjson.New("t", p.t, valKey, p.v))
	}
	return out
}

func deltaFor(pts []tsPoint, refTs, window, current int64) any {
	if len(pts) == 0 {
		return nil
	}
	cutoff := refTs - window
	base := pts[0].v
	for i := len(pts) - 1; i >= 0; i-- {
		if pts[i].t <= cutoff {
			base = pts[i].v
			break
		}
	}
	return current - base
}

func reconstructAccountHistory(charSeries [][]tsPoint, bankSeries []tsPoint) []tsPoint {
	tsMap := map[int64]bool{}
	for _, s := range charSeries {
		for _, p := range s {
			tsMap[p.t] = true
		}
	}
	for _, p := range bankSeries {
		tsMap[p.t] = true
	}
	if len(tsMap) == 0 {
		return nil
	}
	times := make([]int64, 0, len(tsMap))
	for t := range tsMap {
		times = append(times, t)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })

	valAt := func(series []tsPoint, t int64) int64 {
		var cur int64
		for _, p := range series {
			if p.t <= t {
				cur = p.v
			} else {
				break
			}
		}
		return cur
	}
	out := make([]tsPoint, 0, len(times))
	for _, t := range times {
		var sum int64
		for _, s := range charSeries {
			sum += valAt(s, t)
		}
		sum += valAt(bankSeries, t)
		out = append(out, tsPoint{t: t, v: sum})
	}
	return out
}

func wealthSummary(db *sv.Table, refTs int64) *pyjson.Obj {
	w := tab(db.Get("wealth"))
	charsTab := tab(w.Get("chars"))

	type charRec struct {
		key     string
		name    string
		realm   string
		class   string
		money   int64
		updated int64
		pts     []tsPoint
	}
	var chars []charRec
	var charsTotal int64
	var charSeries [][]tsPoint
	maxTs := refTs

	if !charsTab.IsList() {
		for i, k := range charsTab.Keys() {
			ck := fmt.Sprint(k)
			ct := tab(charsTab.Values()[i])
			pts := parseHistoryPoints(ct.Get("history"))
			m := round(or(ct.Get("money"), int64(0)))
			upd := round(or(ct.Get("updated"), int64(0)))
			if upd > maxTs {
				maxTs = upd
			}
			for _, p := range pts {
				if p.t > maxTs {
					maxTs = p.t
				}
			}
			name := str(ct.Get("name"))
			if name == "" {
				name = ck
			}
			charsTotal += m
			charSeries = append(charSeries, pts)
			chars = append(chars, charRec{
				key: ck, name: name, realm: str(ct.Get("realm")), class: str(ct.Get("class")),
				money: m, updated: upd, pts: pts,
			})
		}
	}

	bankTab := tab(w.Get("accountBank"))
	bankMoney := round(or(bankTab.Get("money"), int64(0)))
	bankUpd := round(or(bankTab.Get("updated"), int64(0)))
	bankPts := parseHistoryPoints(bankTab.Get("history"))
	if bankUpd > maxTs {
		maxTs = bankUpd
	}

	var transitTotal int64
	transitOut := []any{}
	for _, v := range values(w.Get("transit")) {
		tr := tab(v)
		amt := round(or(tr.Get("amount"), int64(0)))
		if amt <= 0 {
			continue
		}
		transitTotal += amt
		transitOut = append(transitOut, pyjson.New(
			"id", round(or(tr.Get("id"), int64(0))),
			"from", str(tr.Get("from")),
			"to", str(tr.Get("to")),
			"amount", amt,
			"sentAt", round(or(tr.Get("sentAt"), int64(0))),
		))
	}

	accPts := parseHistoryPoints(w.Get("accountHistory"))
	if len(accPts) == 0 && (len(charSeries) > 0 || len(bankPts) > 0) {
		accPts = reconstructAccountHistory(charSeries, bankPts)
	}
	for _, p := range accPts {
		if p.t > maxTs {
			maxTs = p.t
		}
	}
	if maxTs == 0 {
		maxTs = time.Now().Unix()
	}

	total := charsTotal + bankMoney + transitTotal
	sort.SliceStable(chars, func(i, j int) bool {
		if chars[i].money != chars[j].money {
			return chars[i].money > chars[j].money
		}
		return chars[i].key < chars[j].key
	})

	charsOut := make([]any, 0, len(chars))
	for _, c := range chars {
		share := 0.0
		if total > 0 {
			share = math.Round(float64(c.money)/float64(total)*1000) / 10
		}
		charsOut = append(charsOut, pyjson.New(
			"key", c.key, "name", c.name, "realm", c.realm, "class", c.class,
			"money", c.money, "share", share, "updated", c.updated,
			"deltas", pyjson.New(
				"day", deltaFor(c.pts, maxTs, 86400, c.money),
				"week", deltaFor(c.pts, maxTs, 7*86400, c.money),
				"month", deltaFor(c.pts, maxTs, 30*86400, c.money),
			),
			"history", encodePoints(c.pts, "money"),
		))
	}

	return pyjson.New(
		"total", total,
		"charsTotal", charsTotal,
		"accountBank", bankMoney,
		"accountBankUpdated", bankUpd,
		"accountBankHistory", encodePoints(bankPts, "money"),
		"transitTotal", transitTotal,
		"transit", transitOut,
		"chars", charsOut,
		"accountHistory", encodePoints(accPts, "total"),
		"deltas", pyjson.New(
			"day", deltaFor(accPts, maxTs, 86400, total),
			"week", deltaFor(accPts, maxTs, 7*86400, total),
			"month", deltaFor(accPts, maxTs, 30*86400, total),
		),
	)
}

// ---------- Daily & Period Aggregations ----------

type itemAgg struct {
	id     any
	name   string
	q      any
	count  int64
	value  int64
	vendor int64
	cat    string
}

type zoneAgg struct {
	zone  string
	value int64
	coin  int64
	time  int64
}

type periodAcc struct {
	days          int
	lootTotal     int64
	vendorTotal   int64
	lootByCat     map[string]int64
	income        int64
	expense       int64
	incomeByKind  map[string]int64
	expenseByKind map[string]int64
	ahSales       int64
	ahGross       int64
	ahCut         int64
	ahDeposit     int64
	ahNet         int64
	ahBuys        int64
	ahBuyTotal    int64
	items         map[string]*itemAgg
	zones         map[string]*zoneAgg
	chars         map[string]int64
}

func newPeriodAcc() *periodAcc {
	return &periodAcc{
		lootByCat:     map[string]int64{},
		incomeByKind:  map[string]int64{},
		expenseByKind: map[string]int64{},
		items:         map[string]*itemAgg{},
		zones:         map[string]*zoneAgg{},
		chars:         map[string]int64{},
	}
}

func mapToObj(m map[string]int64) *pyjson.Obj {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := pyjson.New()
	for _, k := range keys {
		out.Set(k, m[k])
	}
	return out
}

func (p *periodAcc) toJSON() *pyjson.Obj {
	items := make([]*itemAgg, 0, len(p.items))
	for _, it := range p.items {
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].value != items[j].value {
			return items[i].value > items[j].value
		}
		return items[i].name < items[j].name
	})
	itemsOut := make([]any, 0, len(items))
	for _, it := range items {
		itemsOut = append(itemsOut, pyjson.New(
			"id", it.id, "name", it.name, "quality", it.q,
			"count", it.count, "value", it.value, "vendorValue", it.vendor, "cat", it.cat,
		))
	}

	zones := make([]*zoneAgg, 0, len(p.zones))
	for _, z := range p.zones {
		zones = append(zones, z)
	}
	sort.SliceStable(zones, func(i, j int) bool {
		if zones[i].value != zones[j].value {
			return zones[i].value > zones[j].value
		}
		return zones[i].zone < zones[j].zone
	})
	zonesOut := make([]any, 0, len(zones))
	for _, z := range zones {
		zonesOut = append(zonesOut, pyjson.New(
			"zone", z.zone, "value", z.value, "coin", z.coin,
			"time", z.time, "perHour", PerHour(float64(z.value), float64(z.time)),
		))
	}

	type ch struct {
		name string
		val  int64
	}
	chList := make([]ch, 0, len(p.chars))
	for k, v := range p.chars {
		chList = append(chList, ch{name: k, val: v})
	}
	sort.SliceStable(chList, func(i, j int) bool {
		if chList[i].val != chList[j].val {
			return chList[i].val > chList[j].val
		}
		return chList[i].name < chList[j].name
	})
	charsOut := make([]any, 0, len(chList))
	for _, c := range chList {
		charsOut = append(charsOut, pyjson.New("char", c.name, "value", c.val))
	}

	return pyjson.New(
		"days", p.days,
		"lootTotal", p.lootTotal,
		"vendorTotal", p.vendorTotal,
		"lootByCat", mapToObj(p.lootByCat),
		"income", p.income,
		"expense", p.expense,
		"net", p.income-p.expense,
		"incomeByKind", mapToObj(p.incomeByKind),
		"expenseByKind", mapToObj(p.expenseByKind),
		"ah", pyjson.New(
			"sales", p.ahSales, "gross", p.ahGross, "cut", p.ahCut,
			"deposit", p.ahDeposit, "net", p.ahNet, "buys", p.ahBuys, "buyTotal", p.ahBuyTotal,
		),
		"items", itemsOut,
		"zones", zonesOut,
		"chars", charsOut,
	)
}

func dailySummary(db *sv.Table, refTs int64) ([]any, *pyjson.Obj) {
	dailyTab := tab(db.Get("daily"))
	if dailyTab.IsList() || dailyTab.Len() == 0 {
		empty := pyjson.New(
			"today", newPeriodAcc().toJSON(),
			"days7", newPeriodAcc().toJSON(),
			"days30", newPeriodAcc().toJSON(),
			"allTime", newPeriodAcc().toJSON(),
		)
		return []any{}, empty
	}

	type dayEntry struct {
		key string
		t   *sv.Table
	}
	days := make([]dayEntry, 0, dailyTab.Len())
	for i, k := range dailyTab.Keys() {
		ks := fmt.Sprint(k)
		days = append(days, dayEntry{key: ks, t: tab(dailyTab.Values()[i])})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].key > days[j].key })

	anchorTime := time.Unix(refTs, 0).UTC()
	if refTs == 0 {
		if parsed, err := time.Parse("2006-01-02", days[0].key); err == nil {
			anchorTime = parsed
		} else {
			anchorTime = time.Now().UTC()
		}
	}
	todayStr := anchorTime.Format("2006-01-02")
	cut7 := anchorTime.AddDate(0, 0, -6).Format("2006-01-02")
	cut30 := anchorTime.AddDate(0, 0, -29).Format("2006-01-02")

	pToday := newPeriodAcc()
	p7 := newPeriodAcc()
	p30 := newPeriodAcc()
	pAll := newPeriodAcc()

	dailyOut := make([]any, 0, len(days))
	for _, d := range days {
		var targets []*periodAcc
		targets = append(targets, pAll)
		if d.key >= cut30 {
			targets = append(targets, p30)
		}
		if d.key >= cut7 {
			targets = append(targets, p7)
		}
		if d.key == todayStr {
			targets = append(targets, pToday)
		}
		for _, p := range targets {
			p.days++
		}

		// Loot by category
		dayLootTotal, dayVendorTotal := int64(0), int64(0)
		dayLootByCat := map[string]int64{}
		lootTab := tab(d.t.Get("loot"))
		if !lootTab.IsList() {
			for i, k := range lootTab.Keys() {
				cat := fmt.Sprint(k)
				lb := tab(lootTab.Values()[i])
				v := round(or(lb.Get("v"), int64(0)))
				vv := round(or(lb.Get("vv"), int64(0)))
				dayLootTotal += v
				dayVendorTotal += vv
				dayLootByCat[cat] += v
				for _, p := range targets {
					p.lootTotal += v
					p.vendorTotal += vv
					p.lootByCat[cat] += v
				}
			}
		}

		// Cashflow
		cf := tab(d.t.Get("cashflow"))
		dayInc, dayExp := int64(0), int64(0)
		incTab := tab(cf.Get("income"))
		if !incTab.IsList() {
			for i, k := range incTab.Keys() {
				kind := fmt.Sprint(k)
				v := round(incTab.Values()[i])
				dayInc += v
				for _, p := range targets {
					p.income += v
					p.incomeByKind[kind] += v
				}
			}
		}
		expTab := tab(cf.Get("expense"))
		if !expTab.IsList() {
			for i, k := range expTab.Keys() {
				kind := fmt.Sprint(k)
				v := round(expTab.Values()[i])
				dayExp += v
				for _, p := range targets {
					p.expense += v
					p.expenseByKind[kind] += v
				}
			}
		}

		// AH daily totals
		ahTab := tab(d.t.Get("ah"))
		ahSales := round(or(ahTab.Get("sales"), int64(0)))
		ahGross := round(or(ahTab.Get("gross"), int64(0)))
		ahCut := round(or(ahTab.Get("cut"), int64(0)))
		ahDep := round(or(ahTab.Get("deposit"), int64(0)))
		ahNet := round(or(ahTab.Get("net"), int64(0)))
		ahBuys := round(or(ahTab.Get("buys"), int64(0)))
		ahBuyTotal := round(or(ahTab.Get("buyTotal"), int64(0)))
		for _, p := range targets {
			p.ahSales += ahSales
			p.ahGross += ahGross
			p.ahCut += ahCut
			p.ahDeposit += ahDep
			p.ahNet += ahNet
			p.ahBuys += ahBuys
			p.ahBuyTotal += ahBuyTotal
		}

		// Items
		itemsTab := tab(d.t.Get("items"))
		for _, v := range values(itemsTab) {
			it := tab(v)
			id := it.Get("id")
			name := str(it.Get("name"))
			if name == "" {
				name = "?"
			}
			key := fmt.Sprintf("%v|%s", id, name)
			cnt := round(or(it.Get("n"), int64(0)))
			val := round(or(it.Get("v"), int64(0)))
			vv := round(or(it.Get("vv"), int64(0)))
			cat := str(it.Get("cat"))
			q := it.Get("q")
			for _, p := range targets {
				agg := p.items[key]
				if agg == nil {
					agg = &itemAgg{id: id, name: name, q: q, cat: cat}
					p.items[key] = agg
				}
				if agg.q == nil && q != nil {
					agg.q = q
				}
				agg.count += cnt
				agg.value += val
				agg.vendor += vv
			}
		}

		// Zones
		zonesTab := tab(d.t.Get("zones"))
		if !zonesTab.IsList() {
			for i, k := range zonesTab.Keys() {
				zname := fmt.Sprint(k)
				zt := tab(zonesTab.Values()[i])
				zv := round(or(zt.Get("v"), int64(0)))
				zc := round(or(zt.Get("c"), int64(0)))
				ztime := round(or(zt.Get("t"), int64(0)))
				for _, p := range targets {
					agg := p.zones[zname]
					if agg == nil {
						agg = &zoneAgg{zone: zname}
						p.zones[zname] = agg
					}
					agg.value += zv
					agg.coin += zc
					agg.time += ztime
				}
			}
		}

		// Chars
		charsTab := tab(d.t.Get("chars"))
		if !charsTab.IsList() {
			for i, k := range charsTab.Keys() {
				cn := fmt.Sprint(k)
				cv := round(charsTab.Values()[i])
				for _, p := range targets {
					p.chars[cn] += cv
				}
			}
		}

		dailyOut = append(dailyOut, pyjson.New(
			"day", d.key,
			"lootTotal", dayLootTotal,
			"vendorTotal", dayVendorTotal,
			"lootByCat", mapToObj(dayLootByCat),
			"income", dayInc,
			"expense", dayExp,
			"net", dayInc-dayExp,
			"ahNet", ahNet,
			"ahSales", ahSales,
		))
	}

	periods := pyjson.New(
		"today", pToday.toJSON(),
		"days7", p7.toJSON(),
		"days30", p30.toJSON(),
		"allTime", pAll.toJSON(),
	)
	return dailyOut, periods
}

// ---------- Ledger, Auction House, Runs & Drops ----------

func ledgerAndAH(db *sv.Table, periods *pyjson.Obj) (*pyjson.Obj, []any) {
	raw := values(db.Get("ledger"))
	ledgerOut := make([]any, 0, len(raw))
	salesOut := []any{}

	type ahItemAgg struct {
		item   string
		itemID any
		sales  int64
		count  int64
		gross  int64
		cut    int64
		net    int64
	}
	topMap := map[string]*ahItemAgg{}

	// Newest first
	for i := len(raw) - 1; i >= 0; i-- {
		e := tab(raw[i])
		kind := str(e.Get("kind"))
		dir := str(e.Get("dir"))
		amt := round(or(e.Get("amount"), int64(0)))
		entry := pyjson.New(
			"t", round(or(e.Get("t"), int64(0))),
			"char", str(e.Get("char")),
			"zone", str(e.Get("zone")),
			"kind", kind,
			"dir", dir,
			"amount", amt,
			"item", e.Get("item"),
			"itemID", e.Get("itemID"),
			"count", e.Get("count"),
			"counterparty", e.Get("counterparty"),
			"gross", e.Get("gross"),
			"cut", e.Get("cut"),
			"deposit", e.Get("deposit"),
			"note", e.Get("note"),
		)
		ledgerOut = append(ledgerOut, entry)
		if kind == "ah" && dir == "in" {
			salesOut = append(salesOut, entry)
			name := str(e.Get("item"))
			if name == "" {
				name = "?"
			}
			agg := topMap[name]
			if agg == nil {
				agg = &ahItemAgg{item: name, itemID: e.Get("itemID")}
				topMap[name] = agg
			}
			if agg.itemID == nil && e.Get("itemID") != nil {
				agg.itemID = e.Get("itemID")
			}
			cnt := round(or(e.Get("count"), int64(1)))
			if cnt <= 0 {
				cnt = 1
			}
			agg.sales++
			agg.count += cnt
			agg.gross += round(or(e.Get("gross"), amt))
			agg.cut += round(or(e.Get("cut"), int64(0)))
			agg.net += amt
		}
	}

	topList := make([]*ahItemAgg, 0, len(topMap))
	for _, v := range topMap {
		topList = append(topList, v)
	}
	sort.SliceStable(topList, func(i, j int) bool {
		if topList[i].net != topList[j].net {
			return topList[i].net > topList[j].net
		}
		return topList[i].item < topList[j].item
	})
	topOut := make([]any, 0, len(topList))
	for _, it := range topList {
		avgUnit := int64(0)
		if it.count > 0 {
			avgUnit = it.net / it.count
		}
		topOut = append(topOut, pyjson.New(
			"item", it.item, "itemID", it.itemID, "sales", it.sales, "count", it.count,
			"gross", it.gross, "cut", it.cut, "net", it.net, "avgUnit", avgUnit,
		))
	}

	var pendingMail, listedCopper, listedCount int64
	pendingTab := tab(db.Get("ahPending"))
	byChar := []any{}
	if !pendingTab.IsList() {
		for i, k := range pendingTab.Keys() {
			pt := tab(pendingTab.Values()[i])
			pm := round(or(pt.Get("pendingMailCopper"), int64(0)))
			lc := round(or(pt.Get("listedCopper"), int64(0)))
			ln := round(or(pt.Get("listedCount"), int64(0)))
			pendingMail += pm
			listedCopper += lc
			listedCount += ln
			byChar = append(byChar, pyjson.New(
				"char", fmt.Sprint(k), "pendingMailCopper", pm,
				"listedCopper", lc, "listedCount", ln, "updated", round(or(pt.Get("updated"), int64(0))),
			))
		}
	}

	ahSummary := pyjson.New(
		"today", periods.Get("today").(*pyjson.Obj).Get("ah"),
		"days7", periods.Get("days7").(*pyjson.Obj).Get("ah"),
		"days30", periods.Get("days30").(*pyjson.Obj).Get("ah"),
		"allTime", periods.Get("allTime").(*pyjson.Obj).Get("ah"),
	)
	ahObj := pyjson.New(
		"summary", ahSummary,
		"pending", pyjson.New("pendingMailCopper", pendingMail, "listedCopper", listedCopper, "listedCount", listedCount, "byChar", byChar),
		"topItems", topOut,
		"sales", salesOut,
	)
	return ahObj, ledgerOut
}

func runsSummary(db *sv.Table) ([]any, []any) {
	raw := values(db.Get("runs"))
	runsOut := make([]any, 0, len(raw))

	type instAgg struct {
		instance    string
		runs        int64
		elapsed     int64
		total       int64
		coin        int64
		dungeonCoin int64
		rollsWon    int64
	}
	instMap := map[string]*instAgg{}

	for i := len(raw) - 1; i >= 0; i-- {
		r := tab(raw[i])
		inst := str(r.Get("instance"))
		if inst == "" {
			inst = "?"
		}
		elapsed := round(or(r.Get("elapsed"), int64(0)))
		coin := round(or(r.Get("coin"), int64(0)))
		dcoin := round(or(r.Get("dungeonCoin"), int64(0)))
		lootVal := round(or(r.Get("lootValue"), int64(0)))
		vendorVal := round(or(r.Get("vendorValue"), int64(0)))
		total := round(or(r.Get("total"), coin+lootVal))
		rolls := round(or(r.Get("rollsWon"), int64(0)))

		var items []any
		for _, iv := range values(r.Get("items")) {
			items = append(items, ItemEntry(tab(iv)))
		}
		if items == nil {
			items = []any{}
		}

		runsOut = append(runsOut, pyjson.New(
			"started", round(or(r.Get("started"), int64(0))),
			"ended", round(or(r.Get("ended"), int64(0))),
			"elapsed", elapsed,
			"instance", inst,
			"instanceID", round(or(r.Get("instanceID"), int64(0))),
			"difficulty", str(r.Get("difficulty")),
			"char", str(r.Get("char")),
			"coin", coin,
			"dungeonCoin", dcoin,
			"lootValue", lootVal,
			"vendorValue", vendorVal,
			"total", total,
			"perHour", PerHour(float64(total), float64(elapsed)),
			"rollsWon", rolls,
			"items", items,
		))

		agg := instMap[inst]
		if agg == nil {
			agg = &instAgg{instance: inst}
			instMap[inst] = agg
		}
		agg.runs++
		agg.elapsed += elapsed
		agg.total += total
		agg.coin += coin
		agg.dungeonCoin += dcoin
		agg.rollsWon += rolls
	}

	stats := make([]*instAgg, 0, len(instMap))
	for _, a := range instMap {
		stats = append(stats, a)
	}
	sort.SliceStable(stats, func(i, j int) bool {
		if stats[i].total != stats[j].total {
			return stats[i].total > stats[j].total
		}
		return stats[i].instance < stats[j].instance
	})
	statsOut := make([]any, 0, len(stats))
	for _, a := range stats {
		statsOut = append(statsOut, pyjson.New(
			"instance", a.instance, "runs", a.runs, "elapsed", a.elapsed,
			"avgElapsed", a.elapsed/a.runs, "total", a.total, "avgTotal", a.total/a.runs,
			"avgPerHour", PerHour(float64(a.total), float64(a.elapsed)),
			"coin", a.coin, "dungeonCoin", a.dungeonCoin, "rollsWon", a.rollsWon,
		))
	}
	return runsOut, statsOut
}

func dropsSummary(db *sv.Table) []any {
	raw := values(db.Get("drops"))
	out := make([]any, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		dr := tab(raw[i])
		out = append(out, pyjson.New(
			"t", round(or(dr.Get("t"), int64(0))),
			"char", str(dr.Get("char")),
			"zone", str(dr.Get("zone")),
			"cat", str(dr.Get("cat")),
			"id", dr.Get("id"),
			"name", str(dr.Get("name")),
			"quality", dr.Get("quality"),
			"count", round(or(dr.Get("count"), int64(1))),
			"value", round(or(dr.Get("value"), int64(0))),
			"vendorValue", round(or(dr.Get("vendor"), int64(0))),
			"source", dr.Get("source"),
		))
	}
	return out
}

// FromDB builds the complete desktop-app payload from GoldPerHourDB.
func FromDB(db *sv.Table) (*pyjson.Obj, error) {
	meta := tab(db.Get("session"))
	current := sessionSummary(tab(db.Get("gold")), tab(db.Get("coin")), tab(db.Get("items")),
		or(db.Get("elapsed"), int64(0)), meta)
	current.Set("runningAtSave", sv.Truthy(db.Get("resume")))
	history := []any{}
	keys := []float64{}
	for _, v := range values(db.Get("history")) {
		h := tab(v)
		s := sessionSummary(tab(h.Get("gold")), tab(h.Get("coin")), tab(h.Get("items")),
			or(h.Get("elapsed"), int64(0)), h)
		s.Set("archived", h.Get("archived"))
		history = append(history, s)
		keys = append(keys, -num(or(or(s.Get("started"), s.Get("archived")), int64(0))))
	}
	order := make([]int, len(history))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return keys[order[i]] < keys[order[j]] })
	sorted := make([]any, len(history))
	for i, k := range order {
		sorted[i] = history[k]
	}
	history = sorted

	var totalTime, totalGold int64
	var best any
	bestRate, rated := 0.0, false
	for _, v := range history {
		s := v.(*pyjson.Obj)
		totalTime += s.Get("elapsed").(int64)
		totalGold += s.Get("total").(int64)
		if r, ok := s.Get("perHour").(float64); ok && (!rated || r > bestRate) {
			best, bestRate, rated = s.Get("started"), r, true
		}
	}
	settings, err := settingsOf(db)
	if err != nil {
		return nil, err
	}

	refTs := round(or(db.Get("updated"), int64(0)))
	wealth := wealthSummary(db, refTs)
	daily, periods := dailySummary(db, refTs)
	ah, ledger := ledgerAndAH(db, periods)
	runs, runStats := runsSummary(db)
	drops := dropsSummary(db)

	return pyjson.New(
		"found", true, "version", db.Get("version"), "updated", db.Get("updated"),
		"current", current, "history", history, "settings", settings,
		"lastRequest", db.Get("lastRequest"), "legacy", !db.Has("settings"),
		"summary", pyjson.New("sessions", len(history), "elapsed", totalTime, "total", totalGold,
			"perHour", PerHour(float64(totalGold), float64(totalTime)), "best", best),
		"herbGathering", HerbGathering,
		"miningBase", MiningBase,
		"wealth", wealth,
		"daily", daily,
		"periods", periods,
		"auctionHouse", ah,
		"ledger", ledger,
		"runs", runs,
		"runStats", runStats,
		"drops", drops,
	), nil
}

// FindFile returns the most recently written GoldPerHour.lua of the accounts, "" without one.
func FindFile(wow string) string {
	best, bestTime := "", 0.0
	for _, p := range game.AccountFiles(wow, "GoldPerHour.lua") {
		t, _ := game.Mtime(p)
		if best == "" || t > bestTime {
			best, bestTime = p, t
		}
	}
	return best
}

// Compute returns the sessions & analytics of the newest GoldPerHour.lua, or {found: false} without one.
func Compute(wow string) (*pyjson.Obj, error) {
	path := FindFile(wow)
	if path == "" {
		settings, _ := settingsOf(sv.NewTable())
		return pyjson.New("found", false, "settings", settings, "herbGathering", HerbGathering, "miningBase", MiningBase), nil
	}
	db, err := sv.LoadTable(path, "GoldPerHourDB")
	if err != nil {
		return nil, err
	}
	out, err := FromDB(db)
	if err != nil {
		return nil, err
	}
	modified, _ := game.Mtime(path)
	out.Set("file", path)
	out.Set("modified", modified)
	return out, nil
}
