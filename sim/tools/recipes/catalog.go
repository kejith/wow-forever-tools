package recipes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/internal/tables"
)

// CatalogVersion is recipes.CATALOG_VERSION: a cached catalog of another version is built again.
const CatalogVersion = 2

// Ordered is a JSON object read or built in order, like a Python dict: Keys in insertion order, ByKey for
// the lookups. A key set again keeps its place and takes the new value.
type Ordered[V any] struct {
	Keys  []string
	ByKey map[string]V
}

// NewOrdered is an empty Ordered.
func NewOrdered[V any]() *Ordered[V] { return &Ordered[V]{ByKey: map[string]V{}} }

// Set sets a key as a Python dict assignment does.
func (o *Ordered[V]) Set(k string, v V) {
	if _, ok := o.ByKey[k]; !ok {
		o.Keys = append(o.Keys, k)
	}
	o.ByKey[k] = v
}

// Get is o.get(k): the value and whether the key exists. A nil Ordered has no keys.
func (o *Ordered[V]) Get(k string) (V, bool) {
	var zero V
	if o == nil {
		return zero, false
	}
	v, ok := o.ByKey[k]
	return v, ok
}

// Len is the number of keys.
func (o *Ordered[V]) Len() int {
	if o == nil {
		return 0
	}
	return len(o.Keys)
}

// Recipe is one recipe of the catalog (catalog["lines"][line][spell]).
type Recipe struct {
	Name        string    `json:"name"`
	Output      int64     `json:"output"`
	Qty         int64     `json:"qty"`
	Reagents    [][]int64 `json:"reagents"` // [item, count] pairs
	Yellow      int64     `json:"yellow"`
	Grey        int64     `json:"grey"`
	Acquire     int64     `json:"acquire"`
	RecipeItems []int64   `json:"recipeItems"`
	Cooldown    *int64    `json:"cooldown"` // seconds; nil = None
}

func (r *Recipe) obj() *pyjson.Obj {
	reagents := make([]any, len(r.Reagents))
	for i, x := range r.Reagents {
		reagents[i] = x
	}
	var cd any
	if r.Cooldown != nil {
		cd = *r.Cooldown
	}
	items := r.RecipeItems
	if items == nil {
		items = []int64{}
	}
	return pyjson.New("name", r.Name, "output", r.Output, "qty", r.Qty, "reagents", reagents,
		"yellow", r.Yellow, "grey", r.Grey, "acquire", r.Acquire, "recipeItems", items, "cooldown", cd)
}

// Item is one item of the catalog (catalog["items"][id]). A field the file does not have reads as Python's
// it.get(field, default) would: quality and buyStack 1, the rest 0, the name nil.
type Item struct {
	Name         *string `json:"name"`
	Quality      int64   `json:"quality"`
	Buy          int64   `json:"buy"`
	BuyStack     int64   `json:"buyStack"`
	Sell         int64   `json:"sell"`
	Bind         int64   `json:"bind"`
	ReqSkill     int64   `json:"reqSkill"`
	ReqSkillLine int64   `json:"reqSkillLine"`
}

// NewItem is an item without fields ({} in Python): quality 1, a stack of 1.
func NewItem() *Item { return &Item{Quality: 1, BuyStack: 1} }

func (it *Item) obj() *pyjson.Obj {
	var name any
	if it.Name != nil {
		name = *it.Name
	}
	return pyjson.New("name", name, "quality", it.Quality, "buy", it.Buy, "buyStack", it.BuyStack,
		"sell", it.Sell, "bind", it.Bind, "reqSkill", it.ReqSkill, "reqSkillLine", it.ReqSkillLine)
}

// Catalog is what recipes.load_catalog returns: {"version", "build", "locale", "lines": {skill line:
// {spell: recipe}}, "items": {item: info}}. Keys are texts, as in the JSON file; Lines and Items keep the
// file's order (consumables walks the lines in order).
type Catalog struct {
	Version any // as in the file: int64 2 for a current catalog
	Build   any // string or nil
	Locale  any // string or nil
	Lines   *Ordered[*Ordered[*Recipe]]
	Items   *Ordered[*Item]
}

// Item is catalog["items"].get(str(id)) or {}: never nil.
func (c *Catalog) Item(id int64) *Item {
	if c != nil {
		if it, ok := c.Items.Get(strconv.FormatInt(id, 10)); ok && it != nil {
			return it
		}
	}
	return NewItem()
}

// Encode writes the catalog as recipes.load_catalog stores it (json.dump with ensure_ascii=False).
func (c *Catalog) Encode() ([]byte, error) {
	lines := pyjson.New()
	for _, k := range c.Lines.Keys {
		recs := c.Lines.ByKey[k]
		o := pyjson.New()
		for _, sid := range recs.Keys {
			o.Set(sid, recs.ByKey[sid].obj())
		}
		lines.Set(k, o)
	}
	items := pyjson.New()
	for _, k := range c.Items.Keys {
		items.Set(k, c.Items.ByKey[k].obj())
	}
	return pyjson.Encode(pyjson.New("version", c.Version, "build", c.Build, "locale", c.Locale,
		"lines", lines, "items", items))
}

// DecodeCatalog reads a catalog file, keeping the order of its objects.
func DecodeCatalog(data []byte) (*Catalog, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	c := &Catalog{Lines: NewOrdered[*Ordered[*Recipe]](), Items: NewOrdered[*Item]()}
	err := object(d, func(key string) error {
		switch key {
		case "version":
			var n json.Number
			if err := d.Decode(&n); err != nil {
				return err
			}
			c.Version = number(n) // an int stays an int, as json.load reads it
			return nil
		case "build":
			return d.Decode(&c.Build)
		case "locale":
			return d.Decode(&c.Locale)
		case "lines":
			c.Lines = NewOrdered[*Ordered[*Recipe]]()
			return object(d, func(line string) error {
				recs := NewOrdered[*Recipe]()
				c.Lines.Set(line, recs)
				return object(d, func(sid string) error {
					r := &Recipe{}
					if err := d.Decode(r); err != nil {
						return err
					}
					recs.Set(sid, r)
					return nil
				})
			})
		case "items":
			c.Items = NewOrdered[*Item]()
			return object(d, func(id string) error {
				it := NewItem()
				if err := d.Decode(it); err != nil {
					return err
				}
				c.Items.Set(id, it)
				return nil
			})
		}
		var skip json.RawMessage
		return d.Decode(&skip)
	})
	return c, err
}

// number is a JSON number as Python's json reads it: an int without a fraction or exponent, else a float.
func number(n json.Number) any {
	if i, err := n.Int64(); err == nil {
		return i
	}
	f, _ := n.Float64()
	return f
}

// object reads one JSON object, calling field for every key with the decoder at its value. A null stands
// for an empty object.
func object(d *json.Decoder, field func(key string) error) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	if t == nil {
		return nil
	}
	if delim, ok := t.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected an object, got %v", t)
	}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return err
		}
		key, _ := t.(string)
		if err := field(key); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

// pyInt is recipes._int: int(float(x or 0)), 0 for a text that is no number.
func pyInt(s string) int64 {
	if s == "" {
		return 0
	}
	f, ok := pyfmt.ParseFloat(s)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int64(math.Trunc(f))
}

// BuildCatalog is recipes.build_catalog: the recipes per skill line plus the items they use, from the DB2
// tables of a build ("" = the newest cached ones) with the names in locale.
func BuildCatalog(build, locale string) (*Catalog, error) {
	cache := tables.CacheDir()
	read := func(name string) ([]tables.Row, error) {
		t, err := tables.Read(cache, name, build, locale)
		if err != nil || t == nil {
			return nil, err
		}
		return t.Rows(), nil
	}
	var abilities []tables.Row
	spells := map[int64]bool{}
	rows, err := read("SkillLineAbility")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if pyInt(r.Str("SkillLine")) != 0 && pyInt(r.Str("Spell")) != 0 {
			abilities = append(abilities, r)
			spells[pyInt(r.Str("Spell"))] = true
		}
	}
	names := map[int64]string{}
	if rows, err = read("SpellName"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if id := pyInt(r.Str("ID")); spells[id] {
			names[id] = r.Str("Name_lang")
		}
	}
	reagents := map[int64][][]int64{}
	if rows, err = read("SpellReagents"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		sid := pyInt(r.Str("SpellID"))
		if !spells[sid] {
			continue
		}
		list := [][]int64{}
		for i := 0; i < 8; i++ {
			id, n := pyInt(r.Str(fmt.Sprintf("Reagent_%d", i))), pyInt(r.Str(fmt.Sprintf("ReagentCount_%d", i)))
			if id > 0 && n > 0 {
				list = append(list, []int64{id, n})
			}
		}
		reagents[sid] = list
	}
	creates := map[int64][2]int64{}
	teaches := map[int64]int64{}
	if rows, err = read("SpellEffect"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		sid, effect := pyInt(r.Str("SpellID")), pyInt(r.Str("Effect"))
		if effect == 24 && spells[sid] && pyInt(r.Str("EffectItemType")) != 0 { // CREATE_ITEM
			creates[sid] = [2]int64{pyInt(r.Str("EffectItemType")), max(1, pyInt(r.Str("EffectBasePointsF")))}
		} else if effect == 36 && spells[pyInt(r.Str("EffectTriggerSpell"))] { // LEARN_SPELL (recipe item)
			teaches[sid] = pyInt(r.Str("EffectTriggerSpell"))
		}
	}
	effectSpell := map[int64]int64{}
	if rows, err = read("ItemEffect"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		effectSpell[pyInt(r.Str("ID"))] = pyInt(r.Str("SpellID"))
	}
	recipeItems := map[int64][]int64{}
	if rows, err = read("ItemXItemEffect"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		sp, ok := effectSpell[pyInt(r.Str("ItemEffectID"))]
		if !ok {
			continue // effect_spell.get(...) is None: nothing teaches through it
		}
		// Recipe items teach through a learn effect or carry the recipe spell itself (as in Classic).
		target, taught := teaches[sp]
		if !taught || target == 0 {
			target = 0
			if spells[sp] {
				target = sp
			}
		}
		if target != 0 {
			recipeItems[target] = append(recipeItems[target], pyInt(r.Str("ItemID")))
		}
	}
	cooldown := map[int64]int64{}
	if rows, err = read("SpellCooldowns"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		sid := pyInt(r.Str("SpellID"))
		if spells[sid] {
			ms := max(pyInt(r.Str("CategoryRecoveryTime")), pyInt(r.Str("RecoveryTime")))
			if ms >= 600000 {
				cooldown[sid] = floorDiv(ms, 1000)
			}
		}
	}

	lines := NewOrdered[*Ordered[*Recipe]]()
	for _, r := range abilities {
		sid, line := pyInt(r.Str("Spell")), pyInt(r.Str("SkillLine"))
		_, creating := creates[sid]
		_, hasReagents := reagents[sid]
		if !creating || !hasReagents || names[sid] == "" {
			continue
		}
		key := strconv.FormatInt(line, 10)
		recs, ok := lines.Get(key)
		if !ok {
			recs = NewOrdered[*Recipe]()
			lines.Set(key, recs)
		}
		rec := &Recipe{Name: names[sid], Output: creates[sid][0], Qty: creates[sid][1], Reagents: reagents[sid],
			Yellow: pyInt(r.Str("TrivialSkillLineRankLow")), Grey: pyInt(r.Str("TrivialSkillLineRankHigh")),
			Acquire: pyInt(r.Str("AcquireMethod")), RecipeItems: sortedSet(recipeItems[sid])}
		if cd, ok := cooldown[sid]; ok {
			rec.Cooldown = &cd
		}
		recs.Set(strconv.FormatInt(sid, 10), rec)
	}
	wanted := map[int64]bool{}
	for _, line := range lines.Keys {
		recs := lines.ByKey[line]
		for _, sid := range recs.Keys {
			rec := recs.ByKey[sid]
			wanted[rec.Output] = true
			for _, id := range rec.RecipeItems {
				wanted[id] = true
			}
			for _, x := range rec.Reagents {
				wanted[x[0]] = true
			}
		}
	}
	items := NewOrdered[*Item]()
	if rows, err = read("ItemSparse"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		id := pyInt(r.Str("ID"))
		if !wanted[id] {
			continue
		}
		it := &Item{Quality: pyInt(r.Str("OverallQualityID")), Buy: pyInt(r.Str("BuyPrice")),
			BuyStack: max(1, pyInt(r.Str("VendorStackCount"))), Sell: pyInt(r.Str("SellPrice")),
			Bind: pyInt(r.Str("Bonding")), ReqSkill: pyInt(r.Str("RequiredSkillRank")),
			ReqSkillLine: pyInt(r.Str("RequiredSkill"))}
		if name := r.Str("Display_lang"); name != "" {
			it.Name = &name
		}
		items.Set(strconv.FormatInt(id, 10), it)
	}
	var b any
	if build != "" {
		b = build
	}
	return &Catalog{Version: CatalogVersion, Build: b, Locale: locale, Lines: lines, Items: items}, nil
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func sortedSet(xs []int64) []int64 {
	seen := map[int64]bool{}
	out := []int64{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func readCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeCatalog(data)
}

// LoadCatalog is recipes.load_catalog: the catalog of a build in a locale ("" = enUS), from the cache
// (scripts/cache/recipes-<build>-<locale>.json, shared with the Python scripts) or built from the DB2 tables
// and then cached. Without a build ("") the newest cached catalog is used, the same locale first.
func LoadCatalog(build, locale string) (*Catalog, error) {
	if locale == "" {
		locale = tables.DefaultLocale
	}
	cache := tables.CacheDir()
	path := ""
	if build != "" {
		path = filepath.Join(cache, fmt.Sprintf("recipes-%s-%s.json", build, locale))
		if _, err := os.Stat(path); err == nil {
			c, err := readCatalog(path)
			if err != nil {
				return nil, err
			}
			if v, ok := sv.Num(c.Version); ok && v == CatalogVersion {
				return c, nil
			}
		}
	}
	if build == "" {
		for _, pattern := range []string{"recipes-*-" + locale + ".json", "recipes-*.json"} {
			cached := game.Glob(cache, pattern)
			if len(cached) == 0 {
				continue
			}
			times := map[string]float64{}
			for _, p := range cached {
				times[p], _ = game.Mtime(p)
			}
			sort.SliceStable(cached, func(i, j int) bool { return times[cached[i]] < times[cached[j]] })
			return readCatalog(cached[len(cached)-1])
		}
	}
	c, err := BuildCatalog(build, locale)
	if err != nil {
		return nil, err
	}
	if path != "" && c.Lines.Len() > 0 {
		if err := os.MkdirAll(cache, 0o755); err != nil {
			return nil, err
		}
		data, err := c.Encode()
		if err != nil {
			return nil, err
		}
		// Written in one step: Python and Go processes may build the same catalog at once.
		tmp := fmt.Sprintf("%s.%d.part", path, os.Getpid())
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return nil, err
		}
		if err := os.Rename(tmp, path); err != nil {
			return nil, err
		}
	}
	return c, nil
}
