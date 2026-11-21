// Package recipes is the Go side of scripts/recipes.py: what every learned profession recipe earns in the
// auction house right now, and which recipes the character has not learned yet would earn more, with where
// they come from and what they cost.
//
// Sources: the recipe catalog from Forever's DB2 tables (BuildCatalog, cached as JSON per build and client
// locale and shared with the Python scripts), the learned recipes from the addon (WoWMCP_Char.recipes),
// Auctionator's prices and the vendor prices the addon saw (WoWMCP_Account.vendor).
//
// Profit per craft = AH price × amount × (1 − 5 % AH cut) − reagents. Reagents at the cheaper price of AH
// and vendor; own reagents count at the market price.
package recipes

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/internal/tables"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
)

// AHCut is recipes.AH_CUT. A variable, so that 1 − AHCut is computed in float64 as Python computes it.
var AHCut = 0.05

// Gathering are recipes.GATHERING: Herbalism, Mining, Skinning, Fishing have nothing to craft.
var Gathering = map[int64]bool{182: true, 186: true, 393: true, 356: true}

// NewID is recipes.NEW_ID: items above it are new in Forever.
const NewID = 200000

// keyInt is Python's int(k) of a table key; false for a key that is no integer.
func keyInt(k any) (int64, bool) {
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

// ColorOf is recipes.color_of: 0 orange, 1 yellow, 2 green, 3 grey, like TradeskillRelativeDifficulty in
// the game; nil (None) without a grey threshold.
func ColorOf(rank float64, yellow, grey int64) any {
	switch {
	case grey == 0:
		return nil
	case rank >= float64(grey):
		return int64(3)
	case rank >= float64(floorDiv(yellow+grey, 2)):
		return int64(2)
	case rank >= float64(yellow):
		return int64(1)
	}
	return int64(0)
}

// FmtCD is recipes.fmt_cd: a cooldown as "47 h" or "20 min"; nil (None) without one.
func FmtCD(seconds *int64) any {
	if seconds == nil || *seconds == 0 {
		return nil
	}
	hours := float64(*seconds) / 3600
	if hours >= 1 {
		s, _ := pyfmt.Spec(hours, ".0f")
		return s + " h"
	}
	return strconv.FormatInt(floorDiv(*seconds, 60), 10) + " min"
}

// Pair is one reagent: item and count.
type Pair struct{ ID, Count int64 }

// HistoryPoint is one day of recipes.profit_history.
type HistoryPoint struct {
	Date   string // ISO date
	Profit int64
	Price  int64
	Listed any
}

func (h HistoryPoint) obj() *pyjson.Obj {
	return pyjson.New("date", h.Date, "profit", h.Profit, "price", h.Price, "listed", h.Listed)
}

func historyList(hs []HistoryPoint) []any {
	out := make([]any, len(hs))
	for i, h := range hs {
		out[i] = h.obj()
	}
	return out
}

// ProfitHistory is recipes.profit_history: the profit per craft on every full scan day, calculated like
// the current profit. Days on which the result or a reagent has no price are left out.
func ProfitHistory(m *Market, output int64, qty float64, reagents []Pair) []HistoryPoint {
	out := []HistoryPoint{}
	keep := 1 - AHCut
	for _, day := range m.ScanDays {
		price := m.DayPrice(output, day)
		if price == 0 {
			continue
		}
		cost, complete := 0.0, true
		for _, r := range reagents {
			var options []float64
			for _, p := range []float64{m.DayPrice(r.ID, day), first(m.VendorPrice(r.ID))} {
				if p != 0 {
					options = append(options, p)
				}
			}
			if len(options) == 0 {
				complete = false
				break
			}
			low := options[0]
			for _, p := range options[1:] {
				if p < low {
					low = p
				}
			}
			cost += low * float64(r.Count)
		}
		if complete {
			out = append(out, HistoryPoint{
				Date:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(day)).Format("2006-01-02"),
				Profit: pyfmt.RoundInt(price*qty*keep - cost), Price: pyfmt.RoundInt(price),
				Listed: m.DayListed(output, day)})
		}
	}
	return out
}

func first(f float64, _ any) float64 { return f }

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TrendOf is recipes.trend_of: "falling", "rising" or "stable" (the change of the profit over the last
// three full scan days; less than 20 % of the largest amount in that window, at least 5 % of the reagent
// cost or 1 silver, counts as stable), nil (None) without three scan days.
func TrendOf(history []HistoryPoint, cost float64) any {
	if len(history) < 3 {
		return nil
	}
	w := history[len(history)-3:]
	change := float64(w[2].Profit - w[0].Profit)
	scale := max(abs(float64(w[0].Profit)), abs(float64(w[1].Profit)), abs(float64(w[2].Profit)), 0.05*cost, 100)
	if abs(change) < 0.2*scale {
		return "stable"
	}
	if change < 0 {
		return "falling"
	}
	return "rising"
}

// Row is one recipe as recipes.evaluate returns it (Obj, in Python's key order), with the numbers the
// sorting needs. Acquire is set for a recipe that is not learned.
type Row struct {
	Obj     *pyjson.Obj
	Profit  *int64 // the rounded profit; nil = None
	Listed  float64
	Acquire *Acquisition
}

// Acquisition is recipes.acquisition: where a recipe that is not learned yet comes from and what it costs.
type Acquisition struct {
	Obj       *pyjson.Obj
	Best      *pyjson.Obj // the cheapest priced option, nil = None
	BestPrice float64
	Learnable any // true, false or nil (unknown)
}

// Evaluate is recipes.evaluate: the reagents with their cheapest price, cost, sale, profit, margin, how
// often the bags and bank allow the craft, the colour, the profit history and its trend, and notes. rec is
// the catalog's recipe (nil = not in the catalog), scan the addon's entry for the spell (nil = None), names
// the item names of the profession export.
func Evaluate(sid int64, rec *Recipe, scan *sv.Table, rank float64, m *Market, owned map[int64]int64,
	names map[int64]string) *Row {
	item := m.Catalog.Item
	nameOf := func(i int64) string {
		if n := names[i]; n != "" {
			return n
		}
		if n := item(i).Name; n != nil && *n != "" {
			return *n
		}
		return "Item " + strconv.FormatInt(i, 10)
	}

	var reagents []Pair
	if list, ok := scan.Get("reagents").(*sv.Table); ok {
		for _, r := range list.Tables() {
			if !sv.Truthy(r.Get("optional")) {
				reagents = append(reagents, Pair{int64(num(r.Get("id"))), int64(num(r.Get("count")))})
			}
		}
	}
	if len(reagents) == 0 && rec != nil {
		for _, x := range rec.Reagents {
			if len(x) >= 2 {
				reagents = append(reagents, Pair{x[0], x[1]})
			}
		}
	}
	var output int64
	if v := scan.Get("output"); sv.Truthy(v) {
		output = int64(num(v))
	} else if rec != nil {
		output = rec.Output
	}
	var qty any = int64(1)
	qtyF := 1.0
	if rec != nil && rec.Qty != 0 {
		qty, qtyF = rec.Qty, float64(rec.Qty)
	}
	if qmin := scan.Get("qtyMin"); sv.Truthy(qmin) {
		qmax := scan.Get("qtyMax")
		if !sv.Truthy(qmax) {
			qmax = qmin
		}
		qtyF = (num(qmin) + num(qmax)) / 2
		qty = qtyF
	}

	rows := []any{}
	cost := 0.0
	var missingPrice []string
	var craftable *int64
	for _, r := range reagents {
		p := m.Reagent(r.ID)
		var unit, line any
		if p.Unit != nil {
			l := *p.Unit * float64(r.Count)
			cost += l
			unit, line = pyfmt.RoundInt(*p.Unit), pyfmt.RoundInt(l)
		} else {
			missingPrice = append(missingPrice, nameOf(r.ID))
		}
		have := owned[r.ID]
		n := int64(0)
		if r.Count != 0 {
			n = floorDiv(have, r.Count)
		}
		if craftable == nil || n < *craftable {
			craftable = &n
		}
		rows = append(rows, pyjson.New("id", r.ID, "name", nameOf(r.ID), "quality", item(r.ID).Quality,
			"count", r.Count, "unit", unit, "cost", line, "source", p.Source, "vendor", p.Vendor, "owned", have))
	}

	outItem := item(output)
	var ah *AH
	if output != 0 {
		ah = m.AH(output)
	}
	bound := outItem.Bind == 1
	var sell *float64
	var sellSource any
	if ah != nil && !bound {
		s := ah.PriceF() * qtyF * (1 - AHCut)
		sell, sellSource = &s, "ah"
	} else if outItem.Sell != 0 {
		s := float64(outItem.Sell) * qtyF
		sell, sellSource = &s, "vendor"
	}
	var profit *float64
	if sell != nil && len(missingPrice) == 0 {
		p := *sell - cost
		profit = &p
	}
	history := []HistoryPoint{}
	if sellSource == "ah" {
		history = ProfitHistory(m, output, qtyF, reagents)
	}
	trend := TrendOf(history, cost)
	difficulty := scan.Get("difficulty")
	if difficulty == nil {
		var yellow, grey int64
		if rec != nil {
			yellow, grey = rec.Yellow, rec.Grey
		}
		difficulty = ColorOf(rank, yellow, grey)
	}
	notes := []any{}
	if bound {
		notes = append(notes, i18n.T("binds when picked up, cannot be sold"))
	}
	if ah != nil && sv.Truthy(ah.Age) {
		if ah.Age == int64(1) {
			notes = append(notes, i18n.T("price is 1 day old"))
		} else {
			notes = append(notes, i18n.T("price is {days} days old", "days", ah.Age))
		}
	}
	if trend == "falling" {
		// A price below the weekly average is then no outlier but the end of a decline.
		was := history[len(history)-3]
		gold, _ := pyfmt.Spec(float64(was.Profit)/10000, "+.2f")
		notes = append(notes, i18n.T("profit is falling (on {date} still {gold}g)", "date", i18n.ShortDate(was.Date),
			"gold", gold))
	} else if ah != nil && ah.PriceF() < 0.6*num(ah.Median) {
		gold, _ := pyfmt.Spec(num(ah.Median)/10000, ".2f")
		notes = append(notes, i18n.T("cheap right now (7-day average {gold}g)", "gold", gold))
	}
	if len(missingPrice) > 0 {
		notes = append(notes, i18n.T("no price for {items}", "items", strings.Join(missingPrice, ", ")))
	}

	name := scan.Get("name")
	if !sv.Truthy(name) {
		if rec != nil && rec.Name != "" {
			name = rec.Name
		} else {
			name = i18n.T("Recipe {id}", "id", sid)
		}
	}
	var outputName, costV, sellV, unitPrice, median, profitV, margin any
	if output != 0 {
		outputName = nameOf(output)
	}
	if len(missingPrice) == 0 {
		costV = pyfmt.RoundInt(cost)
	}
	if sell != nil {
		sellV = pyfmt.RoundInt(*sell)
	}
	var listed any = int64(0)
	if ah != nil {
		unitPrice, median, listed = ah.Price, ah.Median, ah.Listed
	}
	row := &Row{Listed: num(listed)}
	if profit != nil {
		p := pyfmt.RoundInt(*profit)
		row.Profit, profitV = &p, p
		if cost != 0 {
			margin = pyfmt.RoundInt(*profit / cost * 100)
		}
	}
	var craft int64
	if craftable != nil {
		craft = *craftable
	}
	var yellow, grey, cooldown any
	if rec != nil {
		yellow, grey, cooldown = rec.Yellow, rec.Grey, FmtCD(rec.Cooldown)
	}
	row.Obj = pyjson.New(
		"id", sid, "name", name, "output", output, "outputName", outputName, "quality", outItem.Quality,
		"qty", qty, "reagents", rows, "cost", costV, "sell", sellV, "sellSource", sellSource,
		"unitPrice", unitPrice, "median", median, "listed", listed, "profit", profitV, "margin", margin,
		"craftable", craft, "difficulty", difficulty, "yellow", yellow, "grey", grey,
		"skillUps", scan.Get("skillUps"), "cooldown", cooldown, "isNew", output > NewID, "notes", notes,
		"history", historyList(history), "trend", trend)
	return row
}

// Acquire is recipes.acquisition for a recipe that is not learned yet: the options (recipe item in the
// AH, at a vendor the addon saw, a recipe item of unknown source, the trainer), the cheapest priced one,
// the required skill and whether the character can learn it now (nil = unknown).
func Acquire(sid int64, rec *Recipe, scan *sv.Table, rank float64, m *Market) *Acquisition {
	options := []any{}
	type priced struct {
		obj   *pyjson.Obj
		price float64
	}
	var prices []priced
	var req int64
	var recipeItems []int64
	recName := any(nil)
	if rec != nil {
		recipeItems, recName = rec.RecipeItems, rec.Name
	}
	for _, iid := range recipeItems {
		it := m.Catalog.Item(iid)
		if it.ReqSkill != 0 {
			req = it.ReqSkill
		}
		ah := m.AH(iid)
		seen := m.seen(iid)
		var name any
		if it.Name != nil && *it.Name != "" {
			name = *it.Name
		} else {
			name = i18n.T("Recipe: {name}", "name", recName)
		}
		if ah != nil {
			o := pyjson.New("kind", "ah", "id", iid, "name", name, "price", ah.Price, "listed", ah.Listed,
				"age", ah.Age, "quality", it.Quality)
			options = append(options, o)
			if sv.Truthy(ah.Price) {
				prices = append(prices, priced{o, ah.PriceF()})
			}
		}
		if s, ok := seen.(*sv.Table); ok && !s.IsList() && sv.Truthy(s.Get("price")) {
			price := pyfmt.RoundInt(num(s.Get("price")))
			o := pyjson.New("kind", "vendor", "id", iid, "name", name, "price", price, "npc", s.Get("npc"),
				"limited", sv.Truthy(s.Get("limited")), "quality", it.Quality)
			options = append(options, o)
			if price != 0 {
				prices = append(prices, priced{o, float64(price)})
			}
		}
		if ah == nil && !sv.Truthy(seen) {
			options = append(options, pyjson.New("kind", "item", "id", iid, "name", name, "price", nil,
				"quality", it.Quality))
		}
	}
	trainer := len(recipeItems) == 0 && (rec == nil || rec.Acquire != 3)
	if trainer {
		options = append(options, pyjson.New("kind", "trainer", "name", i18n.T("Trainer"), "price", nil))
	}
	a := &Acquisition{}
	if len(prices) > 0 {
		best := prices[0]
		for _, p := range prices[1:] {
			if p.price < best.price {
				best = p
			}
		}
		a.Best, a.BestPrice = best.obj, best.price
	}
	var reqV any
	switch {
	case req != 0:
		reqV = req
		a.Learnable = rank >= float64(req)
	case trainer && rec != nil && rec.Yellow != 0:
		if rank >= float64(rec.Yellow) { // the skill to learn it is never above "yellow"
			a.Learnable = true
		}
	}
	var best any
	if a.Best != nil {
		best = a.Best
	}
	a.Obj = pyjson.New("options", options, "best", best, "reqSkill", reqV, "learnable", a.Learnable,
		"sourceText", scan.Get("source"), "trainer", trainer)
	return a
}

// ProfessionRows is recipes.profession_rows: the recipes of a skill line split into learned and not
// learned (with Acquire), in spell order. scan is the addon's export of the profession (nil = none).
func ProfessionRows(line int64, cat *Catalog, scan *sv.Table, rank float64, m *Market,
	owned map[int64]int64) (learned, missing []*Row) {
	recipes, _ := cat.Lines.Get(strconv.FormatInt(line, 10))
	scanned, _ := scan.Get("recipes").(*sv.Table)
	names := map[int64]string{}
	if nt, ok := scan.Get("names").(*sv.Table); ok {
		for i, k := range nt.Keys() {
			n := nt.Values()[i]
			if id, ok := keyInt(k); ok && sv.Truthy(n) {
				names[id] = pyfmt.Str(n)
			}
		}
	}
	idSet := map[int64]bool{}
	if recipes != nil {
		for _, k := range recipes.Keys {
			if id, ok := pyfmt.ParseInt(k); ok {
				idSet[id] = true
			}
		}
	}
	for _, k := range scanned.Keys() {
		if id, ok := keyInt(k); ok {
			idSet[id] = true
		}
	}
	ids := make([]int64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	scanTruthy := sv.Truthy(scan)
	learned, missing = []*Row{}, []*Row{}
	for _, sid := range ids {
		rec, _ := recipes.Get(strconv.FormatInt(sid, 10))
		v := scanned.Get(sid)
		if !sv.Truthy(v) {
			v = scanned.Get(strconv.FormatInt(sid, 10))
		}
		s, _ := v.(*sv.Table)
		if rec == nil && !(sv.Truthy(s) && sv.Truthy(s.Get("output"))) {
			continue
		}
		var isLearned any
		if scanTruthy {
			isLearned = sv.Truthy(s) && sv.Truthy(s.Get("learned"))
		}
		noSource := rec != nil && rec.Acquire == 3 && len(rec.RecipeItems) == 0
		if !scanTruthy && noSource {
			continue
		}
		row := Evaluate(sid, rec, s, rank, m, owned, names)
		row.Obj.Set("learned", isLearned)
		if isLearned == true {
			learned = append(learned, row)
			continue
		}
		if noSource && !(sv.Truthy(s) && sv.Truthy(s.Get("source"))) {
			continue // no longer available
		}
		row.Acquire = Acquire(sid, rec, s, rank, m)
		row.Obj.Set("acquire", row.Acquire.Obj)
		missing = append(missing, row)
	}
	return learned, missing
}

// less is recipes.sort_key: rows with a profit first, the biggest profit first.
func less(a, b *Row) bool {
	if (a.Profit == nil) != (b.Profit == nil) {
		return a.Profit != nil
	}
	var pa, pb int64
	if a.Profit != nil {
		pa, pb = *a.Profit, *b.Profit
	}
	return -pa < -pb
}

// Compute is recipes.compute: per crafting profession of the character the learned recipes and the ones
// not learned yet (each at most top, 0 = all), with the counts and the best learned profit. profession
// filters by part of the profession name or its skill line ID; prices nil reads Auctionator's database.
func Compute(wow, char, profession string, top int, prices *sv.Table) (*pyjson.Obj, error) {
	files := game.FindChars(wow, char)
	if len(files) == 0 {
		return nil, errors.New(i18n.T("No WoWMCP export found for \"{char}\".", "char", char))
	}
	c, err := upgrades.LoadChar(files[0])
	if err != nil {
		return nil, err
	}
	if prices == nil {
		path := game.FindAuctionator(wow)
		if path == "" {
			return nil, errors.New(i18n.T("Auctionator.lua not found under {wow}", "wow", wow))
		}
		if prices, err = sv.Prices(path); err != nil {
			if errors.Is(err, sv.ErrNoPrices) {
				return nil, errors.New(i18n.T("No price data found in Auctionator.lua."))
			}
			return nil, err
		}
	}
	vendor := sv.NewTable()
	for _, path := range game.AccountFiles(wow, "WoWMCP.lua") {
		acct, err := sv.LoadTable(path, "WoWMCP_Account")
		if err != nil {
			return nil, err
		}
		if v, ok := acct.Get("vendor").(*sv.Table); ok {
			for i, k := range v.Keys() {
				vendor.Set(k, v.Values()[i])
			}
		}
	}
	build := ""
	cl := c.Tab("client")
	if sv.Truthy(cl.Get("version")) && sv.Truthy(cl.Get("build")) {
		if n, ok := keyInt(cl.Get("build")); ok {
			build = pyfmt.Str(cl.Get("version")) + "." + strconv.FormatInt(n, 10)
		}
	}
	// Names in the catalog are in the client's language, like everything else that comes from the game.
	locale := ""
	if v := cl.Get("locale"); sv.Truthy(v) {
		locale = pyfmt.Str(v)
	} else if l := i18n.ClientLocale(wow); l != "" {
		locale = l
	} else {
		locale = tables.DefaultLocale
	}
	if build == "" {
		build = tables.ClientBuild(wow)
	}
	cat, err := LoadCatalog(build, locale)
	if err != nil {
		return nil, err
	}
	if cat.Lines.Len() == 0 {
		return nil, errors.New(i18n.T("The recipe data could not be loaded from wago.tools. Check the network " +
			"connection and refresh."))
	}
	m := NewMarket(prices, vendor, cat)
	owned := OwnedCounts(c)
	scans := map[int64]*sv.Table{}
	if t, ok := c.Get("recipes").(*sv.Table); ok {
		for i, k := range t.Keys() {
			if id, ok := keyInt(k); ok {
				s, _ := t.Values()[i].(*sv.Table)
				scans[id] = s
			}
		}
	}

	out := []any{}
	for _, p := range c.Tab("skills").Tab("professions").Tables() {
		lineV := p.Get("skillLine")
		if !sv.Truthy(lineV) {
			continue
		}
		line := int64(num(lineV))
		if _, ok := cat.Lines.Get(pyfmt.Str(lineV)); Gathering[line] || !ok {
			continue
		}
		pname := ""
		if v := p.Get("name"); sv.Truthy(v) {
			pname = pyfmt.Str(v)
		}
		if profession != "" && profession != pyfmt.Str(lineV) &&
			!strings.Contains(strings.ToLower(pname), strings.ToLower(profession)) {
			continue
		}
		scan := scans[line]
		rankV := scan.Get("rank")
		if !sv.Truthy(rankV) {
			rankV = p.Get("rank")
		}
		if !sv.Truthy(rankV) {
			rankV = int64(0)
		}
		rank := num(rankV)
		learned, missing := ProfessionRows(line, cat, scan, rank, m, owned)
		sort.SliceStable(learned, func(i, j int) bool { return less(learned[i], learned[j]) })
		var bestLearned int64
		found := false
		for _, r := range learned {
			if r.Profit != nil && r.Listed >= 3 && (!found || *r.Profit > bestLearned) {
				bestLearned, found = *r.Profit, true
			}
		}
		for _, r := range missing {
			r.Obj.Set("better", sv.Truthy(scan) && r.Profit != nil && *r.Profit > bestLearned)
			var payback any
			if cost := r.Acquire.BestPrice; r.Acquire.Best != nil && cost != 0 && r.Profit != nil && *r.Profit > 0 {
				payback = pyfmt.Round(cost/float64(*r.Profit), 1)
			}
			r.Obj.Set("payback", payback)
		}
		// First what you can learn now, then by profit.
		sort.SliceStable(missing, func(i, j int) bool {
			a, b := missing[i].Acquire.Learnable != true, missing[j].Acquire.Learnable != true
			if a != b {
				return !a
			}
			return less(missing[i], missing[j])
		})
		learnedRows, missingRows := learned, missing
		if top > 0 {
			learnedRows, missingRows = learned[:min(top, len(learned))], missing[:min(top, len(missing))]
		}
		profitable := 0
		for _, r := range learned {
			if r.Profit != nil && *r.Profit > 0 {
				profitable++
			}
		}
		maxV := scan.Get("max")
		if !sv.Truthy(maxV) {
			maxV = p.Get("max")
		}
		out = append(out, pyjson.New(
			"skillLine", lineV, "name", p.Get("name"), "rank", rankV, "max", maxV, "scanned", scan.Get("updated"),
			"learnedCount", len(learned), "missingCount", len(missing), "bestLearned", bestLearned,
			"learned", objs(learnedRows), "missing", objs(missingRows), "profitableCount", profitable))
	}
	var scanDate any
	if d, ok := sv.ScanDate(prices); ok {
		scanDate = d.Format("2006-01-02")
	}
	locOut := cat.Locale
	if !sv.Truthy(locOut) {
		locOut = locale
	}
	return pyjson.New("character", game.CharKey(files[0]), "level", c.Tab("character").Get("level"),
		"scanDate", scanDate, "build", cat.Build, "locale", locOut, "vendorPrices", vendor.Len(),
		"addonVersion", c.Tab("addon").Get("version"), "professions", out, "ahCut", AHCut), nil
}

func objs(rows []*Row) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r.Obj
	}
	return out
}
