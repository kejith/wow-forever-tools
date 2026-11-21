// Package goldperhour is the Go side of scripts/goldperhour.py: the GoldPerHour sessions the addon keeps in
// GoldPerHourDB (account SavedVariables GoldPerHour.lua) for the desktop app.
//
// The addon (from 0.3) keeps the running session in GoldPerHourDB.gold/coin/items/elapsed, its metadata
// (start, zones, characters, gold curve) in .session and finished sessions in .history. The game writes the
// file only on /reload or logout, so everything here is the state at that moment.
package goldperhour

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

// Categories are the two kinds of income the addon tells apart.
var Categories = []string{"herb", "loot"}

// HerbGathering is the spell ID of Herb Gathering.
const HerbGathering = 2366

// defaults are DEFAULTS in GoldPerHour.lua, in their order.
var defaults = []struct {
	key string
	val any
}{{"scale", int64(1)}, {"tooltipItems", int64(6)}, {"priceSource", "auctionator"}, {"resumeAfterReload", true},
	{"autoStart", false}}

var (
	// link is LINK of the Python side. Its \d would also match other scripts' digits; item links have
	// ASCII digits only.
	link = regexp.MustCompile(`\|Hitem:([0-9]+)[^|]*\|h\[(.*?)\]\|h`)
	// color is COLOR of the Python side.
	color          = regexp.MustCompile(`\|c[fF]{2}([0-9a-fA-F]{6})`)
	qualityByColor = map[string]int64{"9d9d9d": 0, "ffffff": 1, "1eff00": 2, "0070dd": 3, "a335ee": 4, "ff8000": 5}
)

// values is goldperhour.values: a list table's values, an object's values in file order, nothing else.
func values(x any) []any {
	if t, ok := x.(*sv.Table); ok {
		return t.List()
	}
	return nil
}

// keysTrue is goldperhour.keys_true: the positions (from 1) of a list's truthy values, or an object's keys
// whose values are truthy.
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

// num is a value as Python adds and divides it: numbers, and a bool as 0 or 1.
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

// round is Python's round(x) of a number: an int stays, a float goes to the nearest int (a half to even).
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

// or is Python's `a or b`.
func or(a, b any) any {
	if sv.Truthy(a) {
		return a
	}
	return b
}

// get is d.get(k, def) of a table.
func get(t *sv.Table, k string, def any) any {
	if t.Has(k) {
		return t.Get(k)
	}
	return def
}

// tab is `x or {}` for a value that should be a table.
func tab(v any) *sv.Table {
	if t, ok := v.(*sv.Table); ok {
		return t
	}
	return sv.NewTable()
}

// PerHour is goldperhour.per_hour: copper per hour, nil under a minute.
func PerHour(copper, elapsed float64) any {
	if elapsed != 0 && elapsed >= 60 {
		return copper / elapsed * 3600
	}
	return nil
}

// ItemEntry is goldperhour.item_entry: an item of a session, its ID, name and quality from the link if the
// entry lacks them (a 0.2 file).
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
	return pyjson.New("id", id, "name", name, "quality", q, "count", or(e.Get("count"), int64(0)),
		"value", round(or(e.Get("value"), int64(0))))
}

// zoneList is goldperhour.zone_list: the zones of a session, the most gold first.
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

// samples is goldperhour.samples: the gold curve as {t, herb, loot}.
func samples(raw any) []any {
	out := []any{}
	for _, s := range values(raw) {
		v := values(s)
		if len(v) >= 3 {
			out = append(out, pyjson.New("t", v[0], "herb", v[1], "loot", v[2]))
		}
	}
	return out
}

// sortedKeys is sorted() of keys: numbers by value, texts by code point.
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
		return false // Python cannot compare the two
	})
	return out
}

// sessionSummary is goldperhour.session_summary.
func sessionSummary(gold, coin, items *sv.Table, elapsed any, meta *sv.Table) *pyjson.Obj {
	var parts []any
	for _, c := range Categories {
		parts = append(parts, get(gold, c, int64(0)))
	}
	total := pyfmt.SumAny(parts...)
	cats := pyjson.New()
	for _, c := range Categories {
		var lst []any
		for _, e := range values(items.Get(c)) {
			lst = append(lst, ItemEntry(tab(e)))
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
		cats.Set(c, pyjson.New("gold", round(g), "coin", round(get(coin, c, int64(0))),
			"perHour", PerHour(num(g), num(elapsed)), "items", lst, "itemCount", pyfmt.SumAny(counts...)))
	}
	chars := sortedKeys(keysTrue(meta.Get("chars")))
	if chars == nil {
		chars = []any{}
	}
	return pyjson.New("elapsed", round(elapsed), "total", round(total), "perHour", PerHour(num(total), num(elapsed)),
		"categories", cats, "started", meta.Get("started"), "ended", or(meta.Get("ended"), meta.Get("lastActive")),
		"zones", zoneList(meta.Get("zones")), "chars", chars, "samples", samples(meta.Get("samples")))
}

// settingsOf is goldperhour.settings_of: the addon's settings over its defaults, whether the window is
// shown and the extra herb spells.
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
	var spells []int64
	for _, k := range keysTrue(db.Get("herbSpells")) {
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
	s.Set("herbSpells", spells)
	return s, nil
}

// FromDB is goldperhour.from_db: the running session, the history, the settings and a summary.
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
	return pyjson.New(
		"found", true, "version", db.Get("version"), "updated", db.Get("updated"),
		"current", current, "history", history, "settings", settings,
		"lastRequest", db.Get("lastRequest"), "legacy", !db.Has("settings"),
		"summary", pyjson.New("sessions", len(history), "elapsed", totalTime, "total", totalGold,
			"perHour", PerHour(float64(totalGold), float64(totalTime)), "best", best),
		"herbGathering", HerbGathering,
	), nil
}

// FindFile is goldperhour.find_file: the most recently written GoldPerHour.lua of the accounts, "" without one.
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

// Compute is goldperhour.compute: the sessions of the newest GoldPerHour.lua, or {found: false} without one.
func Compute(wow string) (*pyjson.Obj, error) {
	path := FindFile(wow)
	if path == "" {
		settings, _ := settingsOf(sv.NewTable())
		return pyjson.New("found", false, "settings", settings, "herbGathering", HerbGathering), nil
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
