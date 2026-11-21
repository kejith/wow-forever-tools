package consumables

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/internal/tables"
	"github.com/kejith/wow-forever-tools/sim/tools/recipes"
)

// CatalogVersion is consumables.CATALOG_VERSION: a cached catalog of another version is built again.
const CatalogVersion = 2

// Restore is one restoring effect of an item: healing or mana with its variance, or food, drink and
// bandages with the seconds they take. Var is set for healing and mana, Seconds for the others.
type Restore struct {
	Kind    string   `json:"kind"`
	Amount  int64    `json:"amount"`
	Var     *float64 `json:"var"`
	Seconds *int64   `json:"seconds"`
}

func (r Restore) obj() *pyjson.Obj {
	o := pyjson.New("kind", r.Kind, "amount", r.Amount)
	if r.Var != nil {
		o.Set("var", *r.Var)
	}
	if r.Seconds != nil {
		o.Set("seconds", *r.Seconds)
	}
	return o
}

// seconds is r.get("seconds") or 0.
func (r Restore) seconds() int64 {
	if r.Seconds == nil {
		return 0
	}
	return *r.Seconds
}

// variance is r["var"] (0 without).
func (r Restore) variance() float64 {
	if r.Var == nil {
		return 0
	}
	return *r.Var
}

// Buff is one stat an item raises.
type Buff struct {
	Stat   string `json:"stat"`
	Amount int64  `json:"amount"`
}

// Item is one consumable of the catalog (catalog["items"][id]), in the key order of build_catalog.
type Item struct {
	Name         string    `json:"name"`
	Quality      int64     `json:"quality"`
	Subclass     int64     `json:"subclass"`
	ReqLevel     int64     `json:"reqLevel"`
	ReqSkill     int64     `json:"reqSkill"`
	ReqSkillRank int64     `json:"reqSkillRank"`
	Classes      int64     `json:"classes"`
	Buy          int64     `json:"buy"`
	BuyStack     int64     `json:"buyStack"`
	Sell         int64     `json:"sell"`
	Restore      []Restore `json:"restore"`
	Buffs        []Buff    `json:"buffs"`
	Duration     *int64    `json:"duration"` // seconds; nil = None
	Oil          bool      `json:"oil"`
	WellFed      bool      `json:"wellFed"`
	Auras        []int64   `json:"auras"` // the spells whose aura is the buff
	HealthCost   int64     `json:"healthCost"`
	Cat          int64     `json:"cat"` // the cooldown category
}

// duration is it.get("duration") or 0.
func (it *Item) duration() int64 {
	if it.Duration == nil {
		return 0
	}
	return *it.Duration
}

func (it *Item) obj() *pyjson.Obj {
	restore := make([]any, len(it.Restore))
	for i, r := range it.Restore {
		restore[i] = r.obj()
	}
	buffs := make([]any, len(it.Buffs))
	for i, b := range it.Buffs {
		buffs[i] = pyjson.New("stat", b.Stat, "amount", b.Amount)
	}
	var duration any
	if it.Duration != nil {
		duration = *it.Duration
	}
	auras := it.Auras
	if auras == nil {
		auras = []int64{}
	}
	return pyjson.New("name", it.Name, "quality", it.Quality, "subclass", it.Subclass, "reqLevel", it.ReqLevel,
		"reqSkill", it.ReqSkill, "reqSkillRank", it.ReqSkillRank, "classes", it.Classes, "buy", it.Buy,
		"buyStack", it.BuyStack, "sell", it.Sell, "restore", restore, "buffs", buffs, "duration", duration,
		"oil", it.Oil, "wellFed", it.WellFed, "auras", auras, "healthCost", it.HealthCost, "cat", it.Cat)
}

// Catalog is what consumables.load_catalog returns: {"version", "build", "locale", "items": {id: item}},
// the items in the file's order (ItemSparse's).
type Catalog struct {
	Version any // as in the file: int64 2 for a current catalog
	Build   any // string or nil
	Locale  any // string or nil
	Items   *recipes.Ordered[*Item]
}

// Encode writes the catalog as load_catalog stores it (json.dump with ensure_ascii=False).
func (c *Catalog) Encode() ([]byte, error) {
	items := pyjson.New()
	for _, k := range c.Items.Keys {
		items.Set(k, c.Items.ByKey[k].obj())
	}
	return pyjson.Encode(pyjson.New("version", c.Version, "build", c.Build, "locale", c.Locale, "items", items))
}

// DecodeCatalog reads a catalog file, keeping the order of its items.
func DecodeCatalog(data []byte) (*Catalog, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	c := &Catalog{Items: recipes.NewOrdered[*Item]()}
	err := object(d, func(key string) error {
		switch key {
		case "version":
			var n json.Number
			if err := d.Decode(&n); err != nil {
				return err
			}
			if i, err := n.Int64(); err == nil {
				c.Version = i
			} else {
				c.Version, _ = n.Float64()
			}
			return nil
		case "build":
			return d.Decode(&c.Build)
		case "locale":
			return d.Decode(&c.Locale)
		case "items":
			c.Items = recipes.NewOrdered[*Item]()
			return object(d, func(id string) error {
				it := &Item{BuyStack: 1}
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

// pyInt is consumables._int: int(float(x or 0)), 0 for a text that is no number.
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

// pyFloat is consumables._float: float(x or 0), 0.0 for a text that is no number.
func pyFloat(s string) float64 {
	if s == "" {
		return 0
	}
	f, ok := pyfmt.ParseFloat(s)
	if !ok {
		return 0
	}
	return f
}

// effect is one row of SpellEffect as build_catalog keeps it.
type effect struct {
	kind, aura, bp, misc, trigger, period int64
	variance                              float64
}

// enchant is one SpellItemEnchantment: its duration and (type, points, arg) per effect.
type enchant struct {
	duration int64
	effects  [][3]int64
}

// decoded is what decode returns.
type decoded struct {
	restore      []Restore
	buffs        []Buff
	duration     *int64
	oil, wellFed bool
	auras        []int64
	healthCost   int64
	buffDuration *int64 // d.get("buffDuration"): nil when not set or set to None
}

// or is Python's `a or b` for an optional int.
func or(a, b *int64) *int64 {
	if a != nil && *a != 0 {
		return a
	}
	return b
}

// spellData is what decode reads: effects per spell, durations per spell (seconds), enchantments.
type spellData struct {
	effects   map[int64][]effect
	durations map[int64]int64
	enchants  map[int64]enchant
}

func (s *spellData) duration(sid int64) *int64 {
	if d, ok := s.durations[sid]; ok {
		return &d
	}
	return nil
}

// statOfAura is AURA[aura](misc): the stat an aura raises, "" for none; false when the aura is not in AURA.
func statOfAura(aura, misc int64) (string, bool) {
	switch aura {
	case 13: // MOD_DAMAGE_DONE, misc = school mask
		if misc&126 == 126 {
			return "sp", true
		}
		return school[misc], true
	case 135:
		return "healing", true
	case 29:
		return attribute[misc], true
	case 34:
		return "health", true
	case 99:
		return "ap", true
	case 124:
		return "rap", true
	case 85:
		if misc == 0 {
			return "mp5", true
		}
		return "", true
	case 161, 84:
		return "hp5", true
	case 57, 71:
		return "spell_crit", true
	case 52, 290:
		return "crit", true
	case 22, 674:
		if misc == 1 {
			return "armor", true
		}
		return "", true
	}
	return "", false
}

var school = map[int64]string{2: "sp_holy", 4: "sp_fire", 8: "sp_nature", 16: "sp_frost", 32: "sp_shadow",
	64: "sp_arcane"}
var attribute = map[int64]string{0: "strength", 1: "agility", 2: "stamina", 3: "intellect", 4: "spirit",
	-1: "all_stats"}
var itemMod = map[int64]string{3: "agility", 4: "strength", 5: "intellect", 6: "spirit", 7: "stamina"}

// decode is consumables.decode: what a spell does, following triggered spells (Forever's "Nutritious Food"
// triggers the eating spell and, after 10 s, Well Fed). eating: the spell is in the cooldown category of food
// or drink, whatever the item's subclass says.
func decode(sid int64, s *spellData, subclass int64, depth int, eating bool) decoded {
	out := decoded{restore: []Restore{}, buffs: []Buff{}, auras: []int64{}, duration: s.duration(sid)}
	if depth > 2 {
		return out
	}
	for _, e := range s.effects[sid] {
		kind, aura, bp, misc := e.kind, e.aura, e.bp, e.misc
		ench, hasEnchant := s.enchants[misc]
		switch {
		case kind == 10 && bp > 0: // HEAL
			v := e.variance
			out.restore = append(out.restore, Restore{Kind: "healing", Amount: bp, Var: &v})
		case kind == 30 && misc == 0 && bp > 0: // ENERGIZE mana
			v := e.variance
			out.restore = append(out.restore, Restore{Kind: "mana", Amount: bp, Var: &v})
		case kind == 2 && bp > 0 && depth == 0: // SCHOOL_DAMAGE of an item's own spell: a rune burns its user
			out.healthCost += bp
		case kind == 64 && e.trigger != 0: // TRIGGER_SPELL
			sub := decode(e.trigger, s, subclass, depth+1, eating)
			out.restore = append(out.restore, sub.restore...)
			out.buffs = append(out.buffs, sub.buffs...)
			out.auras = append(out.auras, sub.auras...)
			out.duration = or(out.duration, sub.duration)
			out.wellFed = out.wellFed || sub.wellFed
		case kind == 54 && hasEnchant: // ENCHANT_ITEM_TEMPORARY: weapon oil
			for _, x := range ench.effects {
				etype, points, arg := x[0], x[1], x[2]
				if etype == 3 { // equip spell
					out.buffs = append(out.buffs, decode(arg, s, other, depth+1, false).buffs...)
				} else if etype == 5 && itemMod[arg] != "" {
					out.buffs = append(out.buffs, Buff{Stat: itemMod[arg], Amount: points})
				}
			}
			if len(out.buffs) > 0 {
				d := ench.duration
				out.oil, out.duration = true, &d
			}
		case kind == 6: // APPLY_AURA
			seconds := int64(0)
			if d := s.duration(sid); d != nil {
				seconds = *d
			}
			stat, inAura := statOfAura(aura, misc)
			switch {
			case (subclass == food || eating) && (aura == 84 || aura == 85) && depth <= 1:
				// Eating and drinking: the aura holds the amount per 5 seconds.
				amount := bp
				if seconds != 0 {
					amount = pyfmt.RoundInt(float64(bp*seconds) / 5)
				}
				k := "food"
				if aura != 84 {
					k = "drink"
				}
				secs := seconds
				out.restore = append(out.restore, Restore{Kind: k, Amount: amount, Seconds: &secs})
			case subclass == bandage && aura == 8 && e.period != 0: // PERIODIC_HEAL
				amount := bp
				if seconds != 0 {
					amount = pyfmt.RoundInt(float64(bp*seconds*1000) / float64(e.period))
				}
				secs := seconds
				out.restore = append(out.restore, Restore{Kind: "bandage", Amount: amount, Seconds: &secs})
			case (aura == 23 || aura == 227) && e.trigger != 0: // periodic trigger: Well Fed after some seconds
				sub := decode(e.trigger, s, other, depth+1, false)
				for _, b := range sub.buffs {
					// "Nutritious Food" keeps the amount on its own effect ($s2), the Well Fed aura holds 1.
					out.buffs = append(out.buffs, Buff{Stat: b.Stat, Amount: max(b.Amount, bp)})
				}
				if len(sub.buffs) > 0 {
					out.wellFed = true
					out.buffDuration = sub.duration
					out.auras = append(out.auras, sub.auras...)
				}
			case inAura && bp > 0:
				if stat != "" {
					out.buffs = append(out.buffs, Buff{Stat: stat, Amount: bp})
					out.auras = append(out.auras, sid)
				}
			}
		}
	}
	if len(out.restore) == 0 {
		out.healthCost = 0 // damage that pays for nothing is no cost
	}
	return out
}

// reader reads one DB2 table's rows (tables.Read, or a test's rows).
type reader func(name string) ([]tables.Row, error)

// BuildCatalog is consumables.build_catalog: every consumable of the DB2 tables of a build ("" = the newest
// cached ones), names in locale.
func BuildCatalog(build, locale string) (*Catalog, error) {
	cache := tables.CacheDir()
	return buildCatalog(func(name string) ([]tables.Row, error) {
		t, err := tables.Read(cache, name, build, locale)
		if err != nil || t == nil {
			return nil, err
		}
		return t.Rows(), nil
	}, build, locale)
}

func buildCatalog(read reader, build, locale string) (*Catalog, error) {
	rows, err := read("Item")
	if err != nil {
		return nil, err
	}
	base := map[int64]int64{}
	for _, r := range rows {
		id, class := pyInt(r.Str("ID")), pyInt(r.Str("ClassID"))
		if class == 0 || extraItems[id] {
			if class == 0 {
				base[id] = pyInt(r.Str("SubclassID"))
			} else {
				base[id] = other
			}
		}
	}
	if rows, err = read("ItemSparse"); err != nil {
		return nil, err
	}
	sparse := recipes.NewOrdered[tables.Row]()
	for _, r := range rows {
		if id := pyInt(r.Str("ID")); hasKey(base, id) {
			sparse.Set(strconv.FormatInt(id, 10), r)
		}
	}
	// The cooldown category of an item's spell: on the item effect or, when that has none, on the spell.
	if rows, err = read("SpellCategories"); err != nil {
		return nil, err
	}
	spellCategory := map[int64]int64{}
	for _, r := range rows {
		spellCategory[pyInt(r.Str("SpellID"))] = pyInt(r.Str("Category"))
	}
	if rows, err = read("ItemEffect"); err != nil {
		return nil, err
	}
	effectSpell, effectCategory := map[int64]int64{}, map[int64]int64{}
	for _, r := range rows {
		effectSpell[pyInt(r.Str("ID"))] = pyInt(r.Str("SpellID"))
		effectCategory[pyInt(r.Str("ID"))] = pyInt(r.Str("SpellCategoryID"))
	}
	if rows, err = read("ItemXItemEffect"); err != nil {
		return nil, err
	}
	spellOf, categories := map[int64][]int64{}, map[int64][]int64{}
	for _, r := range rows {
		iid, eid := pyInt(r.Str("ItemID")), pyInt(r.Str("ItemEffectID"))
		_, inSparse := sparse.Get(strconv.FormatInt(iid, 10))
		if !inSparse || effectSpell[eid] == 0 {
			continue
		}
		spellOf[iid] = append(spellOf[iid], effectSpell[eid])
		if categories[iid] == nil {
			categories[iid] = []int64{}
		}
		if x := effectCategory[eid]; x != 0 {
			categories[iid] = append(categories[iid], x)
		}
		if x := spellCategory[effectSpell[eid]]; x != 0 {
			categories[iid] = append(categories[iid], x)
		}
	}
	s := &spellData{effects: map[int64][]effect{}, durations: map[int64]int64{}, enchants: map[int64]enchant{}}
	if rows, err = read("SpellEffect"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		sid := pyInt(r.Str("SpellID"))
		s.effects[sid] = append(s.effects[sid], effect{kind: pyInt(r.Str("Effect")), aura: pyInt(r.Str("EffectAura")),
			bp: pyInt(r.Str("EffectBasePointsF")), variance: pyFloat(r.Str("Variance")),
			misc: pyInt(r.Str("EffectMiscValue_0")), trigger: pyInt(r.Str("EffectTriggerSpell")),
			period: pyInt(r.Str("EffectAuraPeriod"))})
	}
	if rows, err = read("SpellDuration"); err != nil {
		return nil, err
	}
	length := map[int64]int64{}
	for _, r := range rows {
		length[pyInt(r.Str("ID"))] = pyInt(r.Str("Duration"))
	}
	if rows, err = read("SpellMisc"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if ms, ok := length[pyInt(r.Str("DurationIndex"))]; ok && ms > 0 {
			s.durations[pyInt(r.Str("SpellID"))] = ms / 1000
		}
	}
	if rows, err = read("SpellItemEnchantment"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		e := enchant{duration: pyInt(r.Str("Duration"))}
		for i := 0; i < 3; i++ {
			if t := pyInt(r.Str(fmt.Sprintf("Effect_%d", i))); t != 0 {
				e.effects = append(e.effects, [3]int64{t, pyInt(r.Str(fmt.Sprintf("EffectPointsMin_%d", i))),
					pyInt(r.Str(fmt.Sprintf("EffectArg_%d", i)))})
			}
		}
		s.enchants[pyInt(r.Str("ID"))] = e
	}

	items := recipes.NewOrdered[*Item]()
	for _, key := range sparse.Keys {
		r := sparse.ByKey[key]
		iid, _ := strconv.ParseInt(key, 10, 64)
		name := r.Str("Display_lang")
		if name == "" || skipped(name) || pyInt(r.Str("Flags_0"))&0x2 != 0 { // conjured
			continue
		}
		sub := base[iid]
		cats := categories[iid]
		eating := false
		for _, x := range cats {
			eating = eating || x == catFood || x == catDrink
		}
		it := &Item{Restore: []Restore{}, Buffs: []Buff{}, Auras: []int64{}}
		if len(cats) > 0 {
			it.Cat = cats[0]
		}
		seen := map[int64]bool{}
		for _, sid := range spellOf[iid] {
			if seen[sid] {
				continue
			}
			seen[sid] = true
			d := decode(sid, s, sub, 0, eating)
			it.Restore = append(it.Restore, d.restore...)
			it.Buffs = append(it.Buffs, d.buffs...)
			it.Duration = or(or(it.Duration, d.buffDuration), d.duration)
			it.Oil = it.Oil || d.oil
			it.WellFed = it.WellFed || d.wellFed
			it.Auras = sortedUnion(it.Auras, d.auras)
			it.HealthCost += d.healthCost
		}
		var keep []Restore
		for _, x := range it.Restore {
			if sub == bandage && x.Kind == "bandage" ||
				sub != bandage && (sub == food || eating || x.Kind == "healing" || x.Kind == "mana") {
				keep = append(keep, x)
			}
		}
		it.Restore = append([]Restore{}, keep...)
		if len(it.Restore) == 0 && len(it.Buffs) == 0 {
			continue
		}
		skill := pyInt(r.Str("RequiredSkill"))
		if alias, ok := skillAlias[skill]; ok {
			skill = alias
		}
		it.Name, it.Quality, it.Subclass = name, pyInt(r.Str("OverallQualityID")), sub
		it.ReqLevel, it.ReqSkill, it.ReqSkillRank = pyInt(r.Str("RequiredLevel")), skill, pyInt(r.Str("RequiredSkillRank"))
		it.Classes, it.Buy = pyInt(r.Str("AllowableClass")), pyInt(r.Str("BuyPrice"))
		it.BuyStack, it.Sell = max(1, pyInt(r.Str("VendorStackCount"))), pyInt(r.Str("SellPrice"))
		items.Set(key, it)
	}
	var b any
	if build != "" {
		b = build
	}
	var loc any
	if locale != "" {
		loc = locale
	}
	return &Catalog{Version: int64(CatalogVersion), Build: b, Locale: loc, Items: items}, nil
}

func hasKey(m map[int64]int64, k int64) bool { _, ok := m[k]; return ok }

// skipped tells whether a name holds one of SKIP_WORDS (deprecated, test and the like; "test" also matches
// inside a word, as in Python).
func skipped(name string) bool {
	low := strings.ToLower(name)
	for _, w := range skipWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// sortedUnion is sorted(set(a) | set(b)).
func sortedUnion(a, b []int64) []int64 {
	seen := map[int64]bool{}
	out := []int64{}
	for _, xs := range [][]int64{a, b} {
		for _, x := range xs {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
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

// LoadCatalog is consumables.load_catalog: the catalog of a build in a locale ("" = enUS), from the cache
// (scripts/cache/consumables-<build>-<locale>.json, shared with the Python scripts) or built from the DB2
// tables and then cached. Without a build ("") the newest cached catalog is used, the same locale first.
func LoadCatalog(build, locale string) (*Catalog, error) {
	if locale == "" {
		locale = tables.DefaultLocale
	}
	cache := tables.CacheDir()
	path := ""
	if build != "" {
		path = filepath.Join(cache, fmt.Sprintf("consumables-%s-%s.json", build, locale))
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
		for _, pattern := range []string{"consumables-*-" + locale + ".json", "consumables-*.json"} {
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
	if path != "" && c.Items.Len() > 0 {
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
