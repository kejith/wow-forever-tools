// Package ingame is scripts/ingame.py: what the WoWMCP addon shows in the game. The results of upgrades,
// writs, recipes and consumables become lists, tooltip lines and vendor hints, written as Lua into the addon
// folder (Interface/AddOns/WoWMCP/AppData.lua), which the client loads on /reload.
//
// The file is the one Python writes, byte for byte (the same `hash` too), so an update of the app does not
// make the game reload a file whose content did not change.
package ingame

import (
	"fmt"
	"sort"

	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/writs"
)

const (
	maxTips    = 4  // lines per item
	topCrafts  = 5  // recipes per profession, each with its reagents
	topMissing = 6  // recipes worth getting per profession
	topCrates  = 3  // crate kinds per tier
	maxPayback = 10 // crafts until a recipe has paid for itself, to make it onto the shopping list
)

// intDict is a Python dict with int keys (item ID -> tooltip lines, item ID -> vendor line), in the order the
// keys were added.
type intDict struct {
	keys []int64
	vals map[int64]any
}

func newIntDict() *intDict { return &intDict{vals: map[int64]any{}} }

func (d *intDict) set(k int64, v any) {
	if _, ok := d.vals[k]; !ok {
		d.keys = append(d.keys, k)
	}
	d.vals[k] = v
}

// Len is the number of keys.
func (d *intDict) Len() int { return len(d.keys) }

// Lines are the tooltip lines of an item.
func (d *intDict) Lines(k int64) []string { s, _ := d.vals[k].([]string); return s }

// sortedKeys are the keys from low to high (json.dumps with sort_keys).
func (d *intDict) sortedKeys() []int64 {
	keys := append([]int64(nil), d.keys...)
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// ---------- Building blocks ----------

// coin is the money placeholder the addon replaces with coins.
func coin(copper any) string { return fmt.Sprintf("{m:%d}", pyRound(or0(copper))) }

// dec is one decimal the way the language writes it: 4.5 or 4,5.
func dec(value any, signed bool) string {
	s := ".1f"
	if signed {
		s = "+.1f"
	}
	text := spec(value, s)
	if i18n.Language() == "de" {
		return replaceDot(text)
	}
	return text
}

func replaceDot(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] == '.' {
			b[i] = ','
		}
	}
	return string(b)
}

// rowOpts are the keyword arguments of ingame.row besides the item and its name.
type rowOpts struct {
	quality any // nil = none
	count   any // nil = 1
	info    string
	price   any
	shop    bool
	sub     bool
}

func row(id, name any, o rowOpts) *pyjson.Obj {
	out := pyjson.New("id", toInt(id), "name", clean(name))
	if o.quality != nil {
		out.Set("q", toInt(o.quality))
	}
	count := o.count
	if count == nil {
		count = int64(1)
	}
	if truthy(count) && num(count) != 1 {
		if isWhole(count) {
			out.Set("n", toInt(count))
		} else {
			out.Set("n", pyfmt.Round(num(count), 1))
		}
	}
	if o.info != "" {
		out.Set("info", o.info)
	}
	if truthy(o.price) {
		out.Set("price", pyRound(o.price))
	}
	if o.shop {
		out.Set("shop", true)
	}
	if o.sub {
		out.Set("sub", true)
	}
	return out
}

func section(title any, note string, rows []any) *pyjson.Obj {
	out := pyjson.New("title", clean(title), "rows", rows)
	if note != "" {
		out.Set("note", note)
	}
	return out
}

func addTip(tips *intDict, id any, line string) {
	k := toInt(id)
	lines, _ := tips.vals[k].([]string)
	if _, ok := tips.vals[k]; !ok {
		tips.set(k, []string{})
	}
	for _, l := range lines {
		if l == line {
			return
		}
	}
	if len(lines) < maxTips {
		tips.vals[k] = append(lines, line)
	}
}

// ---------- Lists ----------

// owned is where an item the account already has lies, as the end of a row's info; nothing for an AH item.
func owned(item any) string {
	if truthy(get(item, "where")) {
		return " · " + clean(get(item, "where"))
	}
	return ""
}

// upgradeList is the AH upgrades: the package for the budget (the shopping list), then the candidates per
// slot. With the combat engine the gains are DPS or HPS, with the weight scale points.
func upgradeList(r any, tips *intDict) *pyjson.Obj {
	eng := get(r, "engine")
	healer := truthy(eng) && isStr(get(eng, "metric"), "hps")
	var sections []any
	pkg := get(r, "package")
	bought := map[int64]bool{}
	for _, it := range items(get(pkg, "items")) {
		bought[toInt(get(it, "id"))] = true
	}
	if len(bought) > 0 {
		var rows []any
		for _, it := range items(get(pkg, "items")) {
			rows = append(rows, row(get(it, "id"), get(it, "name"), rowOpts{quality: get(it, "quality"),
				info:  clean(get(it, "slotName")) + " " + dec(get(it, "gain"), true) + owned(it),
				price: get(it, "price"), shop: truthy(get(it, "price"))}))
		}
		gain, m := dec(or0(get(pkg, "gain")), true), coin(get(pkg, "cost"))
		var note string
		switch {
		case healer:
			note = i18n.T("{gain} HPS for {m}", "gain", gain, "m", m)
		case truthy(eng):
			note = i18n.T("{gain} DPS for {m}", "gain", gain, "m", m)
		default:
			note = i18n.T("{gain} points for {m}", "gain", gain, "m", m)
		}
		sections = append(sections, section(i18n.T("Best package for the budget"), note, rows))
	}
	for _, s := range items(get(r, "slots")) {
		var rows []any
		rank := 0
		for _, c := range items(get(s, "candidates")) {
			later := ""
			if truthy(get(c, "soon")) {
				later = " · " + i18n.T("from level {level}", "level", get(c, "reqLevel"))
			}
			rows = append(rows, row(get(c, "id"), get(c, "name"), rowOpts{quality: get(c, "quality"),
				info: dec(get(c, "gain"), true) + later + owned(c), price: get(c, "price")}))
			slot, gain, m := clean(get(s, "slotName")), dec(get(c, "gain"), true), coin(get(c, "price"))
			if truthy(get(c, "price")) {
				rank++ // the rank among what the auction house has
			}
			var tip string
			switch {
			case !truthy(get(c, "price")):
				where := clean(get(c, "where"))
				switch {
				case healer:
					tip = i18n.T("You own this upgrade for {slot}: {gain} HPS ({where})", "slot", slot, "gain", gain,
						"where", where)
				case truthy(eng):
					tip = i18n.T("You own this upgrade for {slot}: {gain} DPS ({where})", "slot", slot, "gain", gain,
						"where", where)
				default:
					tip = i18n.T("You own this upgrade for {slot}: {gain} points ({where})", "slot", slot,
						"gain", gain, "where", where)
				}
			case healer:
				tip = i18n.T("Upgrade for {slot}: {gain} HPS, no. {rank} in the AH scan ({m})",
					"slot", slot, "gain", gain, "rank", rank, "m", m)
			case truthy(eng):
				tip = i18n.T("Upgrade for {slot}: {gain} DPS, no. {rank} in the AH scan ({m})",
					"slot", slot, "gain", gain, "rank", rank, "m", m)
			default:
				tip = i18n.T("Upgrade for {slot}: {gain} points, no. {rank} in the AH scan ({m})",
					"slot", slot, "gain", gain, "rank", rank, "m", m)
			}
			addTip(tips, get(c, "id"), tip)
			if bought[toInt(get(c, "id"))] {
				addTip(tips, get(c, "id"), i18n.T("Part of the best package for your budget"))
			}
		}
		if len(rows) > 0 {
			current := get(get(s, "current"), "name")
			note := i18n.T("empty")
			if truthy(current) {
				note = i18n.T("now: {name}", "name", clean(current))
			}
			sections = append(sections, section(get(s, "slotName"), note, rows))
		}
	}
	var review []any
	for _, c := range items(get(r, "review")) {
		review = append(review, row(get(c, "id"), get(c, "name"), rowOpts{quality: get(c, "quality"),
			info: clean(get(c, "slot")), price: get(c, "price")}))
	}
	if len(review) > 0 {
		sections = append(sections, section(i18n.T("With a special effect"), i18n.T("the scale cannot rate these"),
			review))
	}
	if len(sections) == 0 {
		return nil
	}
	var note string
	switch {
	case healer:
		note = i18n.T("Budget {m} · combat model: {hps} HPS now", "m", coin(get(r, "budget")),
			"hps", dec(or0(get(eng, "value")), false))
		if spell := get(get(eng, "downrank"), "spell"); truthy(spell) {
			note += " · " + i18n.T("heal with {spell}", "spell", clean(spell))
		}
	case truthy(eng):
		note = i18n.T("Budget {m} · combat model: {dps} DPS now", "m", coin(get(r, "budget")),
			"dps", dec(or0(get(eng, "value")), false))
	default:
		note = i18n.T("Budget {m} · scale: {profile}", "m", coin(get(r, "budget")),
			"profile", clean(get(r, "profileLabel")))
	}
	return pyjson.New("key", "upgrades", "title", i18n.T("AH upgrades"), "note", note, "sections", sections)
}

// have marks a row the character does not need to buy.
func have(own bool, info string) string {
	if !own {
		return info
	}
	if info != "" {
		return i18n.T("you have it") + " · " + info
	}
	return i18n.T("you have it")
}

// writList is the writs per tier, cheapest reputation first; the first one of each tier is the shopping list.
func writList(r any, tips *intDict) *pyjson.Obj {
	var sections []any
	for _, t := range items(get(r, "tiers")) {
		var rows []any
		for i, w := range items(get(t, "best")) {
			per := i18n.T("{m} per reputation", "m", coin(get(w, "per")))
			var net string
			if num(get(w, "net")) < 0 {
				net = i18n.T("profit {m}", "m", coin(neg(get(w, "net"))))
			} else {
				net = i18n.T("net {m}", "m", coin(get(w, "net")))
			}
			wcost, icost := get(w, "wcost"), get(w, "icost")
			rows = append(rows, row(get(w, "writId"), writs.ItemName(toInt(get(w, "writId"))), rowOpts{
				quality: int64(1), info: have(!truthy(wcost), net+" · "+per), price: wcost,
				shop: i == 0 && num(wcost) > 0}))
			rows = append(rows, row(get(w, "itemId"), get(w, "name"), rowOpts{count: get(w, "qty"),
				info: have(!truthy(icost), clean(get(w, "prof"))), price: icost, shop: i == 0 && num(icost) > 0,
				sub: true}))
			addTip(tips, get(w, "writId"), i18n.T("Writ ({tier}): needs {n}x {item}, {net}, {per}",
				"tier", clean(get(t, "name")), "n", get(w, "qty"), "item", clean(get(w, "name")), "net", net,
				"per", per))
			addTip(tips, get(w, "itemId"), i18n.T("For a writ ({tier}): hand in {n}x, {net}",
				"tier", clean(get(t, "name")), "n", get(w, "qty"), "net", net))
		}
		if len(rows) > 0 {
			sections = append(sections, section(get(t, "name"), "", rows))
		}
	}
	if len(sections) == 0 {
		return nil
	}
	note := ""
	if rep := get(r, "reputation"); truthy(rep) {
		note = fmt.Sprintf("%s: %s %s/%s", clean(get(rep, "name")), clean(get(rep, "standing")),
			pyfmt.Str(get(rep, "value")), pyfmt.Str(get(rep, "max")))
	}
	return pyjson.New("key", "writs", "title", i18n.T("Writs"), "note", note, "sections", sections)
}

func crateRows(c any, shop bool) []any {
	best := get(c, "best")
	per := i18n.T("{m} per favor", "m", coin(get(c, "per")))
	return []any{
		row(get(c, "crateId"), get(c, "name"), rowOpts{quality: int64(1), info: have(truthy(get(c, "haveCrate")), per),
			price: get(c, "crateCost"), shop: shop && truthy(get(c, "crateCost"))}),
		row(get(best, "id"), get(best, "name"), rowOpts{count: get(best, "qty"),
			info: have(!truthy(get(best, "cost")), ""), price: get(best, "cost"),
			shop: shop && truthy(get(best, "cost")), sub: true}),
	}
}

// crateList is the trade crates: the cheapest Merchant's Favor over all tiers (the shopping list), then per
// tier.
func crateList(r any, tips *intDict) *pyjson.Obj {
	crates := get(r, "crates")
	var sections []any
	var best []any
	for _, c := range items(get(crates, "best")) {
		if truthy(get(c, "best")) {
			best = append(best, c)
		}
	}
	if len(best) > 0 {
		var rows []any
		for _, c := range best {
			rows = append(rows, crateRows(c, true)...)
		}
		sections = append(sections, section(i18n.T("Cheapest Merchant's Favor"), "", rows))
	}
	for _, t := range items(get(crates, "tiers")) {
		var complete []any
		for _, c := range items(get(t, "rows")) {
			if truthy(get(c, "complete")) && truthy(get(c, "best")) {
				complete = append(complete, c)
			}
		}
		for _, c := range complete {
			b := get(c, "best")
			addTip(tips, get(c, "crateId"), i18n.T("Fill with {n}x {item} ({m}): {per} per Merchant's Favor",
				"n", get(b, "qty"), "item", clean(get(b, "name")), "m", coin(get(b, "cost")), "per", coin(get(c, "per"))))
			for _, o := range items(get(c, "options")) {
				addTip(tips, get(o, "id"), i18n.T("Fills {crate}: {n}x", "crate", clean(get(c, "name")), "n", get(o, "qty")))
			}
		}
		var rows []any
		for i, c := range complete {
			if i >= topCrates {
				break
			}
			rows = append(rows, crateRows(c, false)...)
		}
		if len(rows) > 0 {
			var note string
			if truthy(get(t, "tooLow")) {
				note = i18n.T("from level {level}", "level", get(t, "level"))
			} else {
				note = i18n.T("{n} favor each", "n", spec(get(t, "favor"), "g"))
			}
			sections = append(sections, section(get(t, "name"), note, rows))
		}
	}
	if len(sections) == 0 {
		return nil
	}
	note := ""
	if favor := get(r, "favor"); favor != nil {
		note = i18n.T("Merchant's Favor: {n}", "n", favor)
	}
	return pyjson.New("key", "crates", "title", i18n.T("Trade crates"), "note", note, "sections", sections)
}

func trendText(trend any) string {
	switch {
	case isStr(trend, "falling"):
		return " · " + i18n.T("falling")
	case isStr(trend, "rising"):
		return " · " + i18n.T("rising")
	}
	return ""
}

// recipeList is the professions: the best crafts with their reagents (the first one is the shopping list)
// and the recipes worth getting, with tooltip lines for results, reagents and recipe items.
func recipeList(r any, tips, vendor *intDict) *pyjson.Obj {
	var sections []any
	for _, p := range items(get(r, "professions")) {
		name := clean(get(p, "name"))
		var crafts []any
		for _, x := range items(get(p, "learned")) {
			if num(or0(get(x, "profit"))) > 0 && truthy(get(x, "output")) {
				crafts = append(crafts, x)
			}
		}
		sort.SliceStable(crafts, func(i, j int) bool { return num(get(crafts[i], "profit")) > num(get(crafts[j], "profit")) })
		var rows []any
		for i, x := range crafts {
			profit := i18n.T("profit {m}", "m", coin(get(x, "profit")))
			addTip(tips, get(x, "output"), i18n.T("{profession}: {profit} per craft", "profession", name,
				"profit", profit)+trendText(get(x, "trend")))
			for _, g := range items(get(x, "reagents")) {
				addTip(tips, get(g, "id"), i18n.T("Reagent for {item}: {profit} per craft",
					"item", clean(get(x, "outputName")), "profit", profit))
			}
			if i >= topCrafts {
				continue
			}
			cooldown := ""
			if truthy(get(x, "cooldown")) {
				cooldown = " · " + clean(get(x, "cooldown"))
			}
			output := get(x, "outputName")
			if !truthy(output) {
				output = get(x, "name")
			}
			qty := get(x, "qty")
			if !truthy(qty) {
				qty = int64(1)
			}
			rows = append(rows, row(get(x, "output"), output, rowOpts{quality: get(x, "quality"), count: qty,
				info: profit + trendText(get(x, "trend")) + cooldown}))
			for _, g := range items(get(x, "reagents")) {
				missing := subtract(or0(get(g, "count")), or0(get(g, "owned")))
				source := get(g, "source")
				info := ""
				if isStr(source, "vendor") {
					info = i18n.T("vendor")
				}
				count := get(g, "count")
				if !truthy(count) {
					count = int64(1)
				}
				rows = append(rows, row(get(g, "id"), get(g, "name"), rowOpts{quality: get(g, "quality"),
					count: count, info: have(missing == 0, info), price: get(g, "cost"),
					shop: i == 0 && isStr(source, "ah") && missing > 0, sub: true}))
			}
		}
		if len(rows) > 0 {
			sections = append(sections, section(i18n.T("{profession}: best crafts", "profession", name), "", rows))
		}
		rows = nil
		for _, x := range items(get(p, "missing")) {
			acq := get(x, "acquire")
			if num(or0(get(x, "profit"))) <= 0 {
				continue
			}
			profit := i18n.T("profit {m}", "m", coin(get(x, "profit")))
			for _, o := range items(get(acq, "options")) {
				if !truthy(get(o, "id")) {
					continue
				}
				line := i18n.T("Teaches {recipe}: {profit} per craft", "recipe", clean(get(x, "name")), "profit", profit)
				if truthy(get(x, "payback")) {
					line += ", " + i18n.T("paid off after {n} crafts", "n", spec(get(x, "payback"), "g"))
				}
				addTip(tips, get(o, "id"), line)
				if truthy(get(x, "better")) {
					addTip(tips, get(o, "id"), i18n.T("Pays more than your best recipe"))
				}
				if get(acq, "learnable") == false && truthy(get(acq, "reqSkill")) {
					addTip(tips, get(o, "id"), i18n.T("Needs {profession} {n}", "profession", name,
						"n", get(acq, "reqSkill")))
				}
				vendor.set(toInt(get(o, "id")), line)
			}
			b := get(acq, "best")
			if len(rows) < topMissing && truthy(get(b, "id")) && truthy(get(b, "price")) && get(acq, "learnable") != false {
				where := ""
				if isStr(get(b, "kind"), "vendor") {
					where = " · " + i18n.T("vendor")
				}
				payback := get(x, "payback")
				after := ""
				if truthy(payback) {
					after = " · " + i18n.T("paid off after {n} crafts", "n", spec(payback, "g"))
				}
				rows = append(rows, row(get(b, "id"), get(b, "name"), rowOpts{quality: get(b, "quality"),
					info: profit + after + where, price: get(b, "price"),
					shop: truthy(get(x, "better")) && isStr(get(b, "kind"), "ah") && payback != nil &&
						num(payback) <= maxPayback}))
			}
		}
		if len(rows) > 0 {
			sections = append(sections, section(i18n.T("{profession}: worth getting", "profession", name),
				i18n.T("profit per craft"), rows))
		}
	}
	if len(sections) == 0 {
		return nil
	}
	return pyjson.New("key", "recipes", "title", i18n.T("Professions"), "note", "", "sections", sections)
}

// subtract is max(0, a - b) on numbers read from a result.
func subtract(a, b any) float64 {
	d := num(a) - num(b)
	if d < 0 {
		return 0
	}
	return d
}

// consumableList is the consumables per category; "best now" items from the AH you do not carry are the
// shopping list.
func consumableList(r any, tips *intDict) *pyjson.Obj {
	var sections []any
	type group struct {
		g    any
		buff bool
	}
	var groups []group
	for _, g := range items(get(r, "categories")) {
		groups = append(groups, group{g, false})
	}
	for _, b := range items(get(r, "buffs")) {
		if truthy(get(b, "relevant")) {
			groups = append(groups, group{b, true})
		}
	}
	for _, gr := range groups {
		g := gr.g
		var rows []any
		for _, x := range items(get(g, "items")) {
			if !truthy(get(x, "available")) && !truthy(get(x, "price")) {
				continue
			}
			info := clean(get(x, "effect"))
			if truthy(get(x, "duration")) && gr.buff { // food and bandages name their time in the effect
				info += " (" + clean(get(x, "duration")) + ")"
			}
			if truthy(get(x, "owned")) {
				info = i18n.T("{n} in bags", "n", get(x, "owned")) + " · " + info
			}
			rows = append(rows, row(get(x, "id"), get(x, "name"), rowOpts{quality: get(x, "quality"), info: info,
				price: get(x, "price"),
				shop:  truthy(get(x, "best")) && isStr(get(x, "source"), "ah") && !truthy(get(x, "owned"))}))
			if truthy(get(x, "best")) {
				addTip(tips, get(x, "id"), i18n.T("Best now ({category}): {effect}", "category", clean(get(g, "label")),
					"effect", clean(get(x, "effect"))))
			}
		}
		if len(rows) > 0 {
			sections = append(sections, section(get(g, "label"), "", rows))
		}
	}
	if len(sections) == 0 {
		return nil
	}
	return pyjson.New("key", "consumables", "title", i18n.T("Consumables"),
		"note", i18n.T("Level {n}", "n", get(r, "level")), "sections", sections)
}

// neg is -v, an int staying an int.
func neg(v any) any {
	switch x := v.(type) {
	case int64:
		return -x
	case int:
		return int64(-x)
	}
	return -num(v)
}
