// Package pricehistory is the Go side of scripts/pricehistory.py: the price history of every item in
// Auctionator's price database, for the desktop app.
//
// Auctionator keeps per item and day the lowest (l) and the highest (h) minimum buyout it saw, the most
// offers (a) and the last minimum price (m); days count from 2020-01-01. Every item it has is listed, also
// those only seen in a single search.
//
// Names come from the DB2 table ItemSparse in the client's language (cached as
// itemnames-<build>-<locale>.json, a file the Python side reads and writes too), then from the addon's item
// export (WoWMCP_Items), then from the bags and banks of the exported characters.
package pricehistory

import (
	"errors"
	"sort"
	"time"
	"unicode"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/internal/tables"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
)

// FullScanShare: a day with at least this share of the items of the biggest day was a full scan.
const FullScanShare = 0.3

// Day0 is Auctionator.Constants.SCAN_DAY_0.
var Day0 = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// DayDate is pricehistory.day_date as an ISO date: day 2513 is 2026-11-18.
func DayDate(day int64) string { return Day0.AddDate(0, 0, int(day)).Format("2006-01-02") }

// Day is one day of an item: the lowest and the highest minimum price and the offers (nil: none noted).
type Day struct {
	Day, Low, High int64
	Listed         *int64
}

func (d Day) json() []any {
	var listed any
	if d.Listed != nil {
		listed = *d.Listed
	}
	return []any{d.Day, d.Low, d.High, listed}
}

// number is isinstance(v, (int, float)) of a value from the price database (a bool is an int in Python).
func number(v any) (float64, bool) {
	if b, ok := v.(bool); ok {
		if b {
			return 1, true
		}
		return 0, true
	}
	return sv.Num(v)
}

// round is Python's round(x) of such a number.
func round(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return pyfmt.RoundInt(x)
	}
	f, _ := number(v)
	return int64(f)
}

// isDigit is str.isdigit() of a key of the price database (ASCII: Auctionator writes item IDs).
func isDigit(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// object is a table that Python sees as a dict (not a list).
func object(v any) *sv.Table {
	if t, ok := v.(*sv.Table); ok && !t.IsList() {
		return t
	}
	return nil
}

// DaysOf is pricehistory.days_of: the days of an item, oldest first. A lowest price above the day's highest
// is no lowest price.
func DaysOf(entry *sv.Table) []Day {
	highs, lows, offers := object(entry.Get("h")), object(entry.Get("l")), object(entry.Get("a"))
	var out []Day
	for i, k := range highs.Keys() {
		key := pyfmt.Str(k)
		high, ok := number(highs.Values()[i])
		if !isDigit(key) || !ok || high <= 0 {
			continue
		}
		lowV := lows.Get(k)
		if low, ok := number(lowV); !ok || !(0 < low && low <= high) {
			lowV = highs.Values()[i]
		}
		d := Day{Low: round(lowV), High: round(highs.Values()[i])}
		d.Day, _ = pyfmt.ParseInt(key)
		if n, ok := number(offers.Get(k)); ok {
			listed := int64(n) // int() truncates
			d.Listed = &listed
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Day != b.Day {
			return a.Day < b.Day
		}
		if a.Low != b.Low {
			return a.Low < b.Low
		}
		if a.High != b.High {
			return a.High < b.High
		}
		return a.Listed == nil && b.Listed != nil || a.Listed != nil && b.Listed != nil && *a.Listed < *b.Listed
	})
	return out
}

// known is what the addon saw of an item in the game.
type known struct{ name, q, cls any }

// intKey is int() of a table key.
func intKey(k any) (int64, error) {
	switch x := k.(type) {
	case int64:
		return x, nil
	case float64:
		return int64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		if n, ok := pyfmt.ParseInt(x); ok {
			return n, nil
		}
		return 0, errors.New("invalid literal for int() with base 10: '" + x + "'")
	}
	return 0, errors.New("int() argument must be a string or a number")
}

// tab is `x or {}` for a value that should be a table.
func tab(v any) *sv.Table {
	if t, ok := v.(*sv.Table); ok {
		return t
	}
	return sv.NewTable()
}

// KnownItems is pricehistory.known_items: name and quality the addon saw in the game, from the item export,
// then from bags and banks. Exports of another client language are left out.
func KnownItems(wow, locale string) (map[int64]known, error) {
	out := map[int64]known{}
	for _, path := range game.AccountFiles(wow, "WoWMCP.lua") {
		store, err := sv.LoadTable(path, "WoWMCP_Items")
		if err != nil {
			return nil, err
		}
		if l := store.Get("locale"); sv.Truthy(l) && l != any(locale) {
			continue // names of another client language
		}
		items := tab(store.Get("items"))
		for i, iid := range items.Keys() {
			rec := object(items.Values()[i])
			if rec == nil || !sv.Truthy(rec.Get("name")) {
				continue
			}
			id, err := intKey(iid)
			if err != nil {
				return nil, err
			}
			out[id] = known{rec.Get("name"), rec.Get("quality"), rec.Get("classID")}
		}
	}
	for _, path := range game.FindChars(wow, "") {
		c, err := upgrades.LoadChar(path)
		if err != nil { // a half-written file while the game is saving
			continue
		}
		l := tab(c.Get("client")).Get("locale")
		if !sv.Truthy(l) {
			l = locale
		}
		if l != any(locale) {
			continue
		}
		var bags []any
		if t, ok := c.Get("bags").(*sv.Table); ok {
			bags = append(bags, t.Iter()...)
		}
		if t, ok := tab(c.Get("bank")).Get("containers").(*sv.Table); ok {
			bags = append(bags, t.Iter()...)
		}
		for _, bag := range bags {
			items, ok := tab(bag).Get("items").(*sv.Table)
			if !ok {
				continue
			}
			for _, v := range items.Iter() {
				it, ok := v.(*sv.Table)
				if !ok {
					continue
				}
				// Python keys the dict by the ID as the file has it; only a whole number is ever looked up.
				f, isNum := sv.Num(it.Get("id"))
				id := int64(f)
				if !isNum || float64(id) != f || id == 0 || !sv.Truthy(it.Get("name")) {
					continue
				}
				if _, there := out[id]; !there {
					out[id] = known{it.Get("name"), it.Get("quality"), nil}
				}
			}
		}
	}
	return out, nil
}

// Lower is Python's str.lower(): Go's lower case plus the two rules Python adds, İ to "i̇" and a final Σ to ς.
func Lower(s string) string {
	rs := []rune(s)
	out := make([]rune, 0, len(rs))
	for i, r := range rs {
		switch r {
		case 'İ':
			out = append(out, 'i', '̇')
		case 'Σ':
			if finalSigma(rs, i) {
				out = append(out, 'ς')
			} else {
				out = append(out, 'σ')
			}
		default:
			out = append(out, unicode.ToLower(r))
		}
	}
	return string(out)
}

// cased and caseIgnorable approximate the Unicode properties of the Final_Sigma rule.
func cased(r rune) bool { return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) }

func caseIgnorable(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk) ||
		r == '\'' || r == '.' || r == ':' || r == '^' || r == '`' || r == '·' || r == '’'
}

// finalSigma: a cased letter before it and none after it (case-ignorable characters skipped).
func finalSigma(rs []rune, i int) bool {
	j := i - 1
	for j >= 0 && caseIgnorable(rs[j]) {
		j--
	}
	if j < 0 || !cased(rs[j]) {
		return false
	}
	j = i + 1
	for j < len(rs) && caseIgnorable(rs[j]) {
		j++
	}
	return j == len(rs) || !cased(rs[j])
}

// Compute is pricehistory.compute: every item of the price database with its days, named, sorted by name
// (items without one last). Without prices it reads Auctionator.lua of the game folder.
func Compute(wow string, prices *sv.Table) (*pyjson.Obj, error) {
	var modified any
	if prices == nil {
		path := game.FindAuctionator(wow)
		if path == "" {
			return nil, errors.New(i18n.T("Auctionator.lua not found under {wow}", "wow", wow))
		}
		var err error
		if prices, err = sv.Prices(path); err != nil {
			if errors.Is(err, sv.ErrNoPrices) {
				return nil, errors.New(i18n.T("No price data found in Auctionator.lua."))
			}
			return nil, err
		}
		modified, _ = game.Mtime(path)
	}
	locale := i18n.ClientLocale(wow)
	if locale == "" {
		locale = tables.DefaultLocale
	}
	names, err := LoadNames(tables.CacheDir(), tables.ClientBuild(wow), locale)
	if err != nil {
		return nil, err
	}
	seen, err := KnownItems(wow, locale)
	if err != nil {
		return nil, err
	}

	type item struct {
		id           int64
		name, q, cls any
		m            int64
		days         []Day
	}
	var items []item
	var lastDay int64
	unnamed := 0
	for i, k := range prices.Keys() {
		key, _ := k.(string)
		entry := object(prices.Values()[i])
		if !isDigit(key) || entry == nil {
			continue // "version", and keys of other item kinds (battle pets "p:", suffixes "gr:")
		}
		days := DaysOf(entry)
		if len(days) == 0 {
			continue
		}
		iid, _ := pyfmt.ParseInt(key)
		it := item{id: iid, days: days}
		e := names.Get(key)
		it.name, it.q, it.cls = e[0], e[1], e[2]
		own := seen[iid]
		if !sv.Truthy(it.name) {
			it.name = own.name
		}
		if it.q == nil {
			it.q = own.q
		}
		if it.cls == nil {
			it.cls = own.cls
		}
		if !sv.Truthy(it.name) {
			unnamed++
		}
		last := days[len(days)-1]
		if last.Day > lastDay {
			lastDay = last.Day
		}
		it.m = last.Low
		if m, ok := number(entry.Get("m")); ok && m > 0 {
			it.m = round(entry.Get("m"))
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return nil, errors.New(i18n.T("No price data found in Auctionator.lua."))
	}
	// Auctionator also notes the items of single searches; "the last scan" is the last day that has a good
	// share of the items of the biggest day (as recipes.py counts full scan days).
	perDay := map[int64]int{}
	for _, it := range items {
		for _, d := range it.days {
			perDay[d.Day]++
		}
	}
	biggest := 0
	for _, n := range perDay {
		if n > biggest {
			biggest = n
		}
	}
	lastScan, firstDay := int64(-1), items[0].days[0].Day
	for day, n := range perDay {
		if float64(n) >= FullScanShare*float64(biggest) && day > lastScan {
			lastScan = day
		}
	}
	for _, it := range items {
		if it.days[0].Day < firstDay {
			firstDay = it.days[0].Day
		}
	}
	text := func(v any) string {
		if s, ok := v.(string); ok && s != "" {
			return Lower(s)
		}
		return ""
	}
	keys := make([]string, len(items))
	for i, it := range items {
		keys[i] = text(it.name)
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := items[order[a]], items[order[b]]
		nx, ny := !sv.Truthy(x.name), !sv.Truthy(y.name)
		if nx != ny {
			return ny
		}
		if keys[order[a]] != keys[order[b]] {
			return keys[order[a]] < keys[order[b]]
		}
		return x.id < y.id
	})
	list := make([]any, len(items))
	for i, k := range order {
		it := items[k]
		days := make([]any, len(it.days))
		for j, d := range it.days {
			days[j] = d.json()
		}
		list[i] = pyjson.New("id", it.id, "name", it.name, "q", it.q, "cls", it.cls, "m", it.m, "days", days)
	}
	return pyjson.New("day0", Day0.Format("2006-01-02"), "lastDay", lastDay, "lastDate", DayDate(lastDay),
		"firstDay", firstDay, "lastScanDay", lastScan, "lastScanDate", DayDate(lastScan), "items", list,
		"count", len(items), "unnamed", unnamed, "locale", locale, "modified", modified), nil
}
