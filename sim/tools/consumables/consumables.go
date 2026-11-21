// Package consumables is the Go side of scripts/consumables.py: the consumables for the level of a
// character (the strongest healing and mana potions, food, drinks, bandages and buffs it can use now, with
// the price in the auction house or at a vendor, what is in the bags and which ones it can craft) and, with
// the combat engine, the consumables for a fight rated by what they add to the DPS or HPS, with the best set.
//
// Compute is consumables.compute (the bridge command "consumables"); FightBuffs is consumables.fight_buffs,
// the best set as upgrades and rotations take it (their --consumables).
package consumables

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/internal/tables"
	"github.com/kejith/wow-forever-tools/sim/tools/enginebundle"
	"github.com/kejith/wow-forever-tools/sim/tools/recipes"
	"github.com/kejith/wow-forever-tools/sim/tools/rotations"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
)

const newID = 200000

// Item subclasses of class 0 (consumable).
const (
	potion      = 1
	elixir      = 2
	flask       = 3
	scroll      = 4
	food        = 5
	enhancement = 6
	bandage     = 7
	other       = 8
)

var typeLabel = map[int64]string{potion: i18n.Mark("Potion"), elixir: i18n.Mark("Elixir"), flask: i18n.Mark("Flask"),
	scroll: i18n.Mark("Scroll"), food: i18n.Mark("Food"), bandage: i18n.Mark("Bandage"), other: i18n.Mark("Other")}

var oilLabel = i18n.Mark("Weapon oil")

// categories are the restoring categories: key and title.
var categories = [][2]string{
	{"healing", i18n.Mark("Healing potions")},
	{"mana", i18n.Mark("Mana potions")},
	{"food", i18n.Mark("Food")},
	{"drink", i18n.Mark("Drinks")},
	{"bandage", i18n.Mark("Bandages")},
}

// stats are the buff stats in the order of the page.
var stats = [][2]string{
	{"sp", i18n.Mark("Spell power")}, {"sp_shadow", i18n.Mark("Shadow spell power")},
	{"sp_fire", i18n.Mark("Fire spell power")}, {"sp_frost", i18n.Mark("Frost spell power")},
	{"sp_arcane", i18n.Mark("Arcane spell power")}, {"sp_nature", i18n.Mark("Nature spell power")},
	{"sp_holy", i18n.Mark("Holy spell power")}, {"healing", i18n.Mark("Healing")},
	{"spell_crit", i18n.Mark("Spell crit")}, {"intellect", i18n.Mark("Intellect")}, {"spirit", i18n.Mark("Spirit")},
	{"mp5", i18n.Mark("Mana per 5 sec.")}, {"stamina", i18n.Mark("Stamina")},
	{"health", i18n.Mark("Maximum health")}, {"hp5", i18n.Mark("Health per 5 sec.")}, {"armor", i18n.Mark("Armor")},
	{"strength", i18n.Mark("Strength")}, {"agility", i18n.Mark("Agility")}, {"ap", i18n.Mark("Attack power")},
	{"rap", i18n.Mark("Ranged attack power")}, {"crit", i18n.Mark("Crit")}, {"all_stats", i18n.Mark("All attributes")},
}

var statLabel = func() map[string]string {
	m := map[string]string{}
	for _, s := range stats {
		m[s[0]] = s[1]
	}
	return m
}()

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func union(a map[string]bool, xs ...string) map[string]bool {
	m := set(xs...)
	for k := range a {
		m[k] = true
	}
	return m
}

var caster = set("sp", "intellect", "spirit", "mp5", "stamina", "health", "spell_crit", "all_stats")
var melee = set("strength", "agility", "ap", "crit", "stamina", "health", "armor", "hp5", "all_stats")

// relevant are the stats that help a class; Paladin, Shaman, Druid and unknown classes: every stat.
var relevant = map[string]map[string]bool{
	"WARLOCK": union(caster, "sp_shadow", "sp_fire"),
	"PRIEST":  union(caster, "sp_shadow", "sp_holy", "healing"),
	"MAGE":    union(caster, "sp_fire", "sp_frost", "sp_arcane"),
	"WARRIOR": melee, "ROGUE": melee,
	"HUNTER": set("agility", "ap", "rap", "crit", "intellect", "mp5", "stamina", "health", "all_stats"),
}

var classID = map[string]int64{"WARRIOR": 1, "PALADIN": 2, "HUNTER": 3, "ROGUE": 4, "PRIEST": 5, "DEATHKNIGHT": 6,
	"SHAMAN": 7, "MAGE": 8, "WARLOCK": 9, "MONK": 10, "DRUID": 11}

// skillAlias: Forever's bandages ask for this skill line; the character has First Aid (129).
var skillAlias = map[int64]int64{2942: 129}

var profession = map[int64]string{171: i18n.Mark("Alchemy"), 185: i18n.Mark("Cooking"), 129: i18n.Mark("First Aid"),
	202: i18n.Mark("Engineering"), 333: i18n.Mark("Enchanting"), 197: i18n.Mark("Tailoring"),
	165: i18n.Mark("Leatherworking"), 164: i18n.Mark("Blacksmithing")}

var skipWords = []string{"deprecated", "test", "[ph]", "qa ", "monster -", "unused", "zzold"}

// extraItems are consumables the game files under another item class: Demonic Rune and Dark Rune.
var extraItems = map[int64]bool{12662: true, 20520: true}

// Spell categories that say what shares a cooldown.
const (
	catFood    = 11
	catDrink   = 59
	catPotion  = 4
	catRune    = 1153
	catZanza   = 79
	catBlasted = 103
)

// vendorFood is the food and water every innkeeper or food vendor sells.
var vendorFood = func() map[int64]bool {
	m := map[int64]bool{}
	for _, id := range []int64{159, 1179, 1205, 1708, 1645, 8766,
		4540, 4541, 4542, 4544, 4601, 8950, 117, 2287, 3770, 3771, 4599, 8952,
		787, 4592, 4593, 4594, 21552, 8957, 2070, 414, 422, 1707, 3927, 8932,
		4536, 4537, 4538, 4539, 4602, 8953, 4604, 4605, 4606, 4607, 4608, 8948} {
		m[id] = true
	}
	return m
}()

var percent = set("spell_crit", "crit")

// ---------- texts ----------

// fmtDuration is consumables.fmt_duration: nil (None) without a duration.
func fmtDuration(seconds int64) any {
	switch {
	case seconds == 0:
		return nil
	case seconds >= 3600 && seconds%3600 == 0:
		return i18n.T("{n} h", "n", seconds/3600)
	case seconds >= 60:
		return i18n.T("{n} min", "n", pyfmt.RoundInt(float64(seconds)/60))
	}
	return i18n.T("{n} sec.", "n", seconds)
}

// restoreText is consumables.restore_text.
func restoreText(r Restore) string {
	amount := r.Amount
	if r.Kind == "healing" || r.Kind == "mana" {
		v := r.variance()
		low, high := pyfmt.RoundInt(float64(amount)*(1-v/2)), pyfmt.RoundInt(float64(amount)*(1+v/2))
		span := strconv.FormatInt(amount, 10)
		if low != high {
			span = strconv.FormatInt(low, 10) + "–" + strconv.FormatInt(high, 10)
		}
		if r.Kind == "healing" {
			return i18n.T("restores {amount} health", "amount", span)
		}
		return i18n.T("restores {amount} mana", "amount", span)
	}
	if r.Kind == "drink" {
		return i18n.T("{amount} mana over {seconds} sec.", "amount", amount, "seconds", r.seconds())
	}
	return i18n.T("{amount} health over {seconds} sec.", "amount", amount, "seconds", r.seconds())
}

// buffText is consumables.buff_text.
func buffText(buffs []Buff) string {
	parts := make([]string, len(buffs))
	for i, b := range buffs {
		pct := ""
		if percent[b.Stat] {
			pct = "%"
		}
		parts[i] = "+" + strconv.FormatInt(b.Amount, 10) + pct + " " + i18n.T(statLabel[b.Stat])
	}
	return strings.Join(parts, ", ")
}

// ---------- calculation ----------

// classAllowed is consumables.class_allowed.
func classAllowed(mask int64, classFile string) bool {
	cid := classID[classFile]
	return mask == 0 || mask == -1 || cid == 0 || mask&(1<<(cid-1)) != 0
}

// ctx is what row_of needs to know about the character and the market (consumables.context).
type ctx struct {
	market    *recipes.Market
	vendor    *sv.Table
	owned     map[int64]int64
	madeBy    map[int64]int64
	learned   map[int64]bool
	classFile string        // `ch.get("classFile") or ""`
	skills    map[int64]any // skill line -> rank (`p.get("rank") or 0`)
	profNames map[int64]any // skill line -> name
}

// lineKey is a skill line read from a snapshot as the int a Python dict lookup with an int would match.
func lineKey(v any) (int64, bool) {
	f, ok := sv.Num(v)
	if !ok || f != math.Trunc(f) {
		return 0, false
	}
	return int64(f), true
}

func num(v any) float64 { f, _ := sv.Num(v); return f }

// craftSources is consumables.craft_sources: item -> skill line of a recipe that makes it, and the items the
// character can craft (learned).
func craftSources(cat *recipes.Catalog, c *sv.Table) (map[int64]int64, map[int64]bool) {
	madeBy := map[int64]int64{}
	if cat != nil && cat.Lines != nil {
		for _, line := range cat.Lines.Keys {
			n, _ := pyfmt.ParseInt(line)
			recs := cat.Lines.ByKey[line]
			for _, sid := range recs.Keys {
				out := recs.ByKey[sid].Output
				if _, ok := madeBy[out]; !ok {
					madeBy[out] = n
				}
			}
		}
	}
	learned := map[int64]bool{}
	for _, scan := range c.Tab("recipes").Values() {
		s, ok := scan.(*sv.Table)
		if !ok {
			continue
		}
		for _, rec := range s.Tab("recipes").Values() {
			r, ok := rec.(*sv.Table)
			if ok && sv.Truthy(r.Get("learned")) && sv.Truthy(r.Get("output")) {
				learned[int64(num(r.Get("output")))] = true
			}
		}
	}
	return madeBy, learned
}

func newCtx(c *sv.Table, recipeCat *recipes.Catalog, prices, vendor *sv.Table) *ctx {
	ch := c.Tab("character")
	madeBy, learned := craftSources(recipeCat, c)
	x := &ctx{market: recipes.NewMarket(prices, vendor, nil), vendor: vendor, owned: recipes.OwnedCounts(c),
		madeBy: madeBy, learned: learned, classFile: ch.Str("classFile"), skills: map[int64]any{},
		profNames: map[int64]any{}}
	for _, p := range c.Tab("skills").Tab("professions").Tables() {
		line, ok := lineKey(p.Get("skillLine"))
		if !ok {
			continue
		}
		var rank any = int64(0)
		if v := p.Get("rank"); sv.Truthy(v) {
			rank = v
		}
		x.skills[line] = rank
		if name := p.Get("name"); sv.Truthy(name) {
			x.profNames[line] = name
		}
	}
	return x
}

// professionName is `ctx["profNames"].get(line) or (tr(PROFESSION[line]) if line in PROFESSION else
// str(line))`.
func (x *ctx) professionName(line int64) any {
	if n, ok := x.profNames[line]; ok && sv.Truthy(n) {
		return n
	}
	if p, ok := profession[line]; ok {
		return i18n.T(p)
	}
	return strconv.FormatInt(line, 10)
}

// skill is the rank a character has in a skill line, 0 without.
func (x *ctx) skill(line int64) (any, float64) {
	if v, ok := x.skills[line]; ok {
		return v, num(v)
	}
	return int64(0), 0
}

// Craft is a row's "craft": the profession that makes the item and whether the character learned it.
type Craft struct {
	Profession any
	Learned    bool
}

// Skill is a row's "reqSkill".
type Skill struct {
	Name any
	Rank int64
	Have any
	OK   bool
}

func (s *Skill) obj() any {
	if s == nil {
		return nil
	}
	return pyjson.New("name", s.Name, "rank", s.Rank, "have", s.Have, "ok", s.OK)
}

// Row is one consumable as row_of returns it, plus what the fight rows add. Fields set only in some rows
// say so; Obj writes them in the order Python sets them.
type Row struct {
	ID           int64
	Name         string
	Quality      int64
	Type         string
	ReqLevel     int64
	ReqSkill     *Skill
	Amount       int64
	Effect       string
	Duration     any
	Owned        int64
	Craft        *Craft
	Available    bool
	IsNew        bool
	Alternatives []any
	// offer
	Price       *int64 // nil = None
	Source      any
	AHPrice     any
	Listed      any
	AHAge       any
	VendorPrice any
	VendorNpc   any
	Best        bool

	// The fight rows (fight_candidates): no amount and no alternatives.
	fight        bool
	OnYou        string
	onYouAt      int // 0 none, 1 set by fight_candidates, 2 set by choose
	Kind         string
	Slots        []string
	Active       bool
	Gain         float64
	Delta        *Delta
	Uses         int64 // 0 = no "uses"
	HealthPerSec *float64
	TooCostly    bool
	BlockedBy    string
	Chosen       int
	Adds         float64
	item         *Item
}

// price is `r["price"] or 0`.
func (r *Row) price() int64 {
	if r.Price == nil {
		return 0
	}
	return *r.Price
}

// Obj is the row as Python's dict, keys in its order.
func (r *Row) Obj() *pyjson.Obj {
	var craft any
	if r.Craft != nil {
		craft = pyjson.New("profession", r.Craft.Profession, "learned", r.Craft.Learned)
	}
	var price any
	if r.Price != nil {
		price = *r.Price
	}
	o := pyjson.New("id", r.ID, "name", r.Name, "quality", r.Quality, "type", r.Type, "reqLevel", r.ReqLevel,
		"reqSkill", r.ReqSkill.obj())
	if !r.fight {
		o.Set("amount", r.Amount)
	}
	o.Set("effect", r.Effect)
	o.Set("duration", r.Duration)
	o.Set("owned", r.Owned)
	o.Set("craft", craft)
	o.Set("available", r.Available)
	o.Set("isNew", r.IsNew)
	if !r.fight {
		alt := r.Alternatives
		if alt == nil {
			alt = []any{}
		}
		o.Set("alternatives", alt)
	}
	o.Set("price", price)
	o.Set("source", r.Source)
	o.Set("ahPrice", r.AHPrice)
	o.Set("listed", r.Listed)
	o.Set("ahAge", r.AHAge)
	o.Set("vendorPrice", r.VendorPrice)
	o.Set("vendorNpc", r.VendorNpc)
	if r.Best {
		o.Set("best", true)
	}
	if !r.fight {
		return o
	}
	if r.onYouAt == 1 {
		o.Set("onYou", r.OnYou)
	}
	slots := make([]any, len(r.Slots))
	for i, s := range r.Slots {
		slots[i] = s
	}
	o.Set("kind", r.Kind)
	o.Set("slots", slots)
	o.Set("active", r.Active)
	o.Set("gain", r.Gain)
	o.Set("delta", r.Delta.Obj())
	if r.Uses != 0 {
		o.Set("uses", r.Uses)
	}
	if r.HealthPerSec != nil {
		o.Set("healthPerSec", *r.HealthPerSec)
		o.Set("tooCostly", r.TooCostly)
	}
	if r.Chosen != 0 {
		o.Set("chosen", r.Chosen)
		o.Set("adds", r.Adds)
	}
	if r.onYouAt == 2 {
		o.Set("onYou", r.OnYou)
	} else if r.BlockedBy != "" {
		o.Set("blockedBy", r.BlockedBy)
	}
	return o
}

// offer is consumables.offer: AH and vendor price of one item, the cheaper of the two as "price".
func (x *ctx) offer(iid int64, it *Item, r *Row) {
	ah := x.market.AH(iid)
	seen := x.vendor.Get(iid)
	if !sv.Truthy(seen) {
		seen = x.vendor.Get(strconv.FormatInt(iid, 10))
	}
	var vp any
	var npc any
	if s, ok := seen.(*sv.Table); ok && !s.IsList() && sv.Truthy(s.Get("price")) && !sv.Truthy(s.Get("limited")) {
		vp, npc = s.Get("price"), s.Get("npc")
	} else if vendorFood[iid] && it.Buy != 0 {
		vp = float64(it.Buy) / float64(it.BuyStack)
	}
	// min of (price, source) pairs with a price: the lower price, "ah" before "vendor" when they are equal.
	var price *float64
	var source any
	if ah != nil && sv.Truthy(ah.Price) {
		p := ah.PriceF()
		price, source = &p, "ah"
	}
	if sv.Truthy(vp) {
		v := num(vp)
		if price == nil || v < *price {
			price, source = &v, "vendor"
		}
	}
	if price != nil {
		p := pyfmt.RoundInt(*price)
		r.Price = &p
	}
	r.Source = source
	r.Listed = int64(0)
	if ah != nil {
		r.AHPrice, r.Listed, r.AHAge = ah.Price, ah.Listed, ah.Age
	}
	if vp != nil {
		r.VendorPrice = pyfmt.RoundInt(num(vp))
	}
	r.VendorNpc = npc
}

// rowOf is consumables.row_of.
func (x *ctx) rowOf(iid int64, it *Item, amount int64, effect string) *Row {
	r := &Row{ID: iid, Name: it.Name, Quality: it.Quality, ReqLevel: it.ReqLevel, Amount: amount, Effect: effect,
		Duration: fmtDuration(it.duration()), Owned: x.owned[iid], IsNew: iid > newID, item: it}
	x.offer(iid, it, r)
	if line, ok := x.madeBy[iid]; ok && line != 0 {
		r.Craft = &Craft{Profession: x.professionName(line), Learned: x.learned[iid]}
	}
	r.Available = r.Owned != 0 || r.price() != 0 || (r.Craft != nil && r.Craft.Learned)
	label, ok := typeLabel[it.Subclass]
	if !ok {
		label = typeLabel[other]
	}
	r.Type = i18n.T(label)
	if it.Oil {
		r.Type = i18n.T(oilLabel)
	}
	if it.WellFed {
		r.Type = i18n.T("Buff food")
	}
	if it.ReqSkill != 0 && it.ReqSkillRank != 0 {
		have, haveN := x.skill(it.ReqSkill)
		r.ReqSkill = &Skill{Name: x.professionName(it.ReqSkill), Rank: it.ReqSkillRank, Have: have,
			OK: haveN >= float64(it.ReqSkillRank)}
	}
	return r
}

// usable is consumables.usable.
func (x *ctx) usable(it *Item, level float64) bool {
	if float64(it.ReqLevel) > level || !classAllowed(it.Classes, x.classFile) {
		return false
	}
	if it.ReqSkill != 0 && it.ReqSkillRank != 0 {
		_, have := x.skill(it.ReqSkill)
		return !(have < float64(it.ReqSkillRank))
	}
	return true
}

// obtainable: something the character can get at all: owned, sold, or made by a profession.
func obtainable(r *Row) bool { return r.Available || r.Craft != nil }

type alikeKey struct {
	amount    int64
	effect    string
	available bool
}

// mergeAlike is consumables.merge_alike: items with the same effect as one row; items in the bags or
// craftable by the character keep their own row.
func mergeAlike(rows []*Row) []*Row {
	out := []*Row{}
	seen := map[alikeKey]*Row{}
	for _, r := range rows {
		key := alikeKey{r.Amount, r.Effect, r.Available}
		own := r.Owned != 0 || (r.Craft != nil && r.Craft.Learned)
		if first, ok := seen[key]; ok && !own {
			var price any
			if r.Price != nil {
				price = *r.Price
			}
			first.Alternatives = append(first.Alternatives,
				pyjson.New("id", r.ID, "name", r.Name, "price", price, "source", r.Source))
			continue
		}
		if _, ok := seen[key]; !ok {
			seen[key] = r
		}
		out = append(out, r)
	}
	return out
}

// pySlice is rows[:top] (a negative top counts from the end).
func pySlice[T any](xs []T, top int) []T {
	if top < 0 {
		top += len(xs)
		if top < 0 {
			top = 0
		}
	}
	if top > len(xs) {
		top = len(xs)
	}
	return xs[:top]
}

// rank is consumables.rank: strongest first, cut to top; the strongest one you can get now is "best".
func rank(rows []*Row, top int) ([]*Row, *Row) {
	sorted := append([]*Row{}, rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Amount != b.Amount {
			return a.Amount > b.Amount
		}
		if a.Available != b.Available {
			return a.Available
		}
		return a.price() < b.price()
	})
	merged := mergeAlike(sorted)
	var best *Row
	for _, r := range merged {
		if r.Available {
			best = r
			break
		}
	}
	if best != nil {
		best.Best = true
	}
	if top != 0 {
		merged = pySlice(merged, top)
	}
	return merged, best
}

// nextBetter is consumables.next_better: the first item above the level (or skill) that beats the best one
// now.
func nextBetter(candidates []*Row, bestAmount int64) any {
	var best *Row
	key := func(r *Row) [3]int64 {
		skill := int64(0)
		if r.ReqSkill != nil {
			skill = r.ReqSkill.Rank
		}
		return [3]int64{r.ReqLevel, skill, -r.Amount}
	}
	less := func(a, b [3]int64) bool {
		for i := range a {
			if a[i] != b[i] {
				return a[i] < b[i]
			}
		}
		return false
	}
	for _, r := range candidates {
		if r.Amount > bestAmount && obtainable(r) && (best == nil || less(key(r), key(best))) {
			best = r
		}
	}
	if best == nil {
		return nil
	}
	return pyjson.New("id", best.ID, "name", best.Name, "quality", best.Quality, "reqLevel", best.ReqLevel,
		"reqSkill", best.ReqSkill.obj(), "amount", best.Amount, "effect", best.Effect)
}

func objs(rows []*Row) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r.Obj()
	}
	return out
}

// starter starts the combat engine for a metric (rotations.character's start with the fight asked for).
type starter func(metric string) (*enginebundle.Engine, error)

// evaluate is consumables.evaluate: the page for one character export c; with start also the consumables
// for a fight of seconds, rated by the engine.
func evaluate(c *sv.Table, cat *Catalog, recipeCat *recipes.Catalog, prices, vendor *sv.Table, top int,
	allBuffs bool, start starter, seconds float64, metrics []string) (*pyjson.Obj, []*plan, error) {
	ch := c.Tab("character")
	var levelOut any = int64(1)
	if v := ch.Get("level"); sv.Truthy(v) {
		levelOut = v
	}
	level := num(levelOut)
	x := newCtx(c, recipeCat, prices, vendor)

	now, later := map[string][]*Row{}, map[string][]*Row{}
	buffsNow, buffsLater := map[string][]*Row{}, map[string][]*Row{}
	for _, key := range cat.Items.Keys {
		it := cat.Items.ByKey[key]
		iid, _ := strconv.ParseInt(key, 10, 64)
		if !classAllowed(it.Classes, x.classFile) {
			continue
		}
		ok := x.usable(it, level)
		for _, r := range it.Restore {
			row := x.rowOf(iid, it, r.Amount, restoreText(r))
			if obtainable(row) {
				if ok {
					now[r.Kind] = append(now[r.Kind], row)
				} else {
					later[r.Kind] = append(later[r.Kind], row)
				}
			}
		}
		for _, b := range it.Buffs {
			row := x.rowOf(iid, it, b.Amount, buffText(it.Buffs))
			if obtainable(row) {
				if ok {
					buffsNow[b.Stat] = append(buffsNow[b.Stat], row)
				} else {
					buffsLater[b.Stat] = append(buffsLater[b.Stat], row)
				}
			}
		}
	}

	group := func(key, label string, rowsNow, rowsLater []*Row) *pyjson.Obj {
		rows, best := rank(rowsNow, top)
		bestAmount := int64(0)
		if best != nil {
			bestAmount = best.Amount
		}
		return pyjson.New("key", key, "label", i18n.T(label), "items", objs(rows),
			"next", nextBetter(rowsLater, bestAmount))
	}
	cats := []any{}
	for _, k := range categories {
		cats = append(cats, group(k[0], k[1], now[k[0]], later[k[0]]))
	}
	rel, ok := relevant[x.classFile]
	if !ok {
		rel = map[string]bool{}
		for _, s := range stats {
			rel[s[0]] = true
		}
	}
	buffs := []any{}
	for _, s := range stats {
		key := s[0]
		if (len(buffsNow[key]) > 0 || len(buffsLater[key]) > 0) && (allBuffs || rel[key]) {
			buffs = append(buffs, group(key, s[1], buffsNow[key], buffsLater[key]).Set("relevant", rel[key]))
		}
	}
	var scanDate any
	if prices.Len() > 0 {
		if d, ok := sv.ScanDate(prices); ok {
			scanDate = d.Format("2006-01-02")
		}
	}
	out := pyjson.New("name", ch.Get("name"), "level", levelOut, "class", ch.Get("class"),
		"classFile", ch.Get("classFile"), "updated", c.Get("updated"), "scanDate", scanDate, "build", cat.Build,
		"locale", cat.Locale, "vendorPrices", vendor.Len(), "recipesScanned", sv.Truthy(c.Get("recipes")),
		"categories", cats, "buffs", buffs)
	if start == nil {
		return out, nil, nil
	}
	plans, err := fightPlans(c, cat, x, start, seconds, top, metrics)
	if err != nil {
		return nil, nil, err
	}
	list := make([]any, len(plans))
	for i, p := range plans {
		list[i] = p.Obj()
	}
	out.Set("plans", list)
	return out, plans, nil
}

// Compute is consumables.compute: the page as plain data. engine "on" or "auto" adds the consumables for a
// fight, rated by the combat engine ("plans"); "auto" leaves them out when the engine does not run or does
// not model the class and says why in "planNote". metrics nil = every metric of the class.
func Compute(wow, char string, top int, allBuffs bool, engine string, fightSeconds float64, targetDelta int,
	metrics []string) (*pyjson.Obj, error) {
	out, _, err := compute(wow, char, top, allBuffs, engine, fightSeconds, targetDelta, metrics)
	var l *lookupError
	if errors.As(err, &l) {
		return nil, l.err
	}
	return out, err
}

// lookupError marks an error Python raises as a LookupError (the engine did not run): upgrades falls back to
// the weight scale on it with --engine auto.
type lookupError struct{ err error }

func (l *lookupError) Error() string { return l.err.Error() }
func (l *lookupError) Unwrap() error { return l.err }

func compute(wow, char string, top int, allBuffs bool, engine string, fightSeconds float64, targetDelta int,
	metrics []string) (*pyjson.Obj, []*plan, error) {
	files := game.FindChars(wow, char)
	if len(files) == 0 {
		return nil, nil, errors.New(i18n.T("No WoWMCP export found for \"{char}\".", "char", char))
	}
	c, err := upgrades.LoadChar(files[0])
	if err != nil {
		return nil, nil, err
	}
	if c == nil {
		c = sv.NewTable()
	}
	prices, err := upgrades.LoadPrices(wow)
	if err != nil || prices == nil {
		prices = sv.NewTable() // without an AH scan the page still shows vendor food, bags and recipes
	}
	vendor := sv.NewTable()
	for _, path := range game.AccountFiles(wow, "WoWMCP.lua") {
		acct, err := sv.LoadTable(path, "WoWMCP_Account")
		if err != nil {
			return nil, nil, err
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
		if n, ok := buildNumber(cl.Get("build")); ok {
			build = pyfmt.Str(cl.Get("version")) + "." + strconv.FormatInt(n, 10)
		}
	} else {
		build = tables.ClientBuild(wow)
	}
	// Names in the catalog are in the client's language, like everything else that comes from the game.
	locale := tables.DefaultLocale
	if v := cl.Get("locale"); sv.Truthy(v) {
		locale = pyfmt.Str(v)
	} else if l := i18n.ClientLocale(wow); l != "" {
		locale = l
	}
	cat, err := LoadCatalog(build, locale)
	if err != nil {
		return nil, nil, err
	}
	if cat.Items.Len() == 0 {
		return nil, nil, errors.New(i18n.T("The item data could not be loaded from wago.tools. Check the network " +
			"connection and refresh."))
	}
	var start starter
	note := ""
	if engine != "off" {
		setup, err := rotations.Character(wow, char, fightSeconds, targetDelta, -1, nil)
		if err != nil {
			if engine == "on" {
				return nil, nil, &lookupError{err}
			}
			note = err.Error()
		} else {
			start = func(metric string) (*enginebundle.Engine, error) { return setup.Start(metric, 0, nil) }
		}
	}
	recipeCat, err := recipes.LoadCatalog(build, locale)
	if err != nil {
		return nil, nil, err
	}
	out, plans, err := evaluate(c, cat, recipeCat, prices, vendor, top, allBuffs, start, fightSeconds, metrics)
	if err != nil { // the engine did not run
		if engine == "on" || start == nil {
			return nil, nil, &lookupError{err}
		}
		note = err.Error()
		if out, plans, err = evaluate(c, cat, recipeCat, prices, vendor, top, allBuffs, nil, fightSeconds, nil); err != nil {
			return nil, nil, err
		}
	}
	if note != "" {
		out.Set("planNote", note)
	}
	out.Set("character", game.CharKey(files[0]))
	return out, plans, nil
}

// buildNumber is int(cl["build"]) of a number or a text.
func buildNumber(v any) (int64, bool) {
	if s, ok := v.(string); ok {
		return pyfmt.ParseInt(s)
	}
	f, ok := sv.Num(v)
	return int64(f), ok
}

// FightBuffs is consumables.fight_buffs: the best set for one metric ("" = the spec's own) over a fight of
// fightSeconds against enemies targetDelta levels above (-1 = the profile's), as the other calculations take
// it (upgrades and rotations with --consumables): the engine's stat delta, the names and what the set adds.
// An error the Python side raises as a LookupError (the engine did not run) satisfies IsLookup; upgrades
// then falls back to the weight scale with --engine auto, every other error is returned whatever --engine
// says.
func FightBuffs(wow, char, metric string, fightSeconds float64, targetDelta int) (*upgrades.Consumed, error) {
	_, plans, err := compute(wow, char, 5, false, "on", fightSeconds, targetDelta, []string{metric})
	if err != nil {
		return nil, err
	}
	p := plans[0]
	names := make([]string, len(p.chosen))
	for i, r := range p.chosen {
		names[i] = r.Name
	}
	text := i18n.T("No consumable you can get now adds anything.")
	if len(names) > 0 {
		text = i18n.T("With consumables: {names} (+{gain:.1f} {unit}).", "names", strings.Join(names, ", "),
			"gain", p.gain, "unit", p.unit)
	}
	return &upgrades.Consumed{Delta: p.total.Map(), Names: names, Gain: p.gain, Unit: p.unit, Text: text}, nil
}

// IsLookup tells whether an error of FightBuffs is what Python raises as a LookupError.
func IsLookup(err error) bool {
	var l *lookupError
	return errors.As(err, &l)
}
