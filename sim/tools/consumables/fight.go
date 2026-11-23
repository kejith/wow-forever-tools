package consumables

import (
	"math"
	"sort"
	"strconv"

	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/tools/enginebundle"
)

// ---------- consumables in a fight, rated by the combat engine ----------

const (
	maxHealthCost  = 0.5   // of the health pool: what costs more is never part of the set
	potionCooldown = 120.0 // seconds; the potions share one, the runes and roots another
	eps            = 0.05  // a consumable that adds less to the metric is not worth a place in the set
)

// kindInfo is one kind of KINDS: key, title, what can be active at once, and what of that is an assumption
// ("" = the game's data says it).
type kindInfo struct{ key, title, rule, assumed string }

var kinds = []kindInfo{
	{"flask", i18n.Mark("Flask"),
		i18n.Mark("One flask at a time; its own text says so. It works next to elixirs, buff food and a weapon oil."), ""},
	{"elixir", i18n.Mark("Elixirs"),
		i18n.Mark("Elixirs for different stats work together: the game has no battle and guardian elixirs here."),
		i18n.Mark("Whether two elixirs for the same stat add up is not confirmed for Forever. The set takes one per stat.")},
	{"food", i18n.Mark("Buff food"), i18n.Mark("One Well Fed buff at a time: the next meal replaces it."), ""},
	{"weapon", i18n.Mark("Weapon oil"),
		i18n.Mark("One temporary enchantment per weapon. By their texts a warlock's Spellstone and Firestone take the " +
			"same place."), ""},
	{"scroll", i18n.Mark("Scrolls"), i18n.Mark("One scroll per stat."),
		i18n.Mark("In the original game a scroll did not add to the class buff of the same stat (Arcane Intellect, " +
			"Divine Spirit, Power Word: Fortitude). Not checked for Forever: the set counts the scroll in full.")},
	{"potion", i18n.Mark("Potions in the fight"),
		i18n.Mark("All potions share one cooldown of 2 minutes. The gain is what the potion gives spread over the " +
			"fight: its mana or health, or its buff for the seconds it lasts."), ""},
	{"rune", i18n.Mark("Runes and roots"),
		i18n.Mark("Runes and the roots from Felwood share a cooldown of 2 minutes of their own, next to the potions."), ""},
	{"other", i18n.Mark("Other buffs"),
		i18n.Mark("The buffs from the Blasted Lands share a cooldown of an hour, and one Zanza potion works at a time."),
		i18n.Mark("For the others nothing says what they exclude: the set counts each of them on top.")},
}

// engineKeys are a consumable's stat as the combat engine's keys. "sp" is "damage done by spells" here (aura
// 13), which does not heal; "crit" is melee crit and has no key.
var engineKeys = map[string][]string{
	"sp": {"spDamage"}, "sp_holy": {"spHoly"}, "sp_fire": {"spFire"}, "sp_nature": {"spNature"},
	"sp_frost": {"spFrost"}, "sp_shadow": {"spShadow"}, "sp_arcane": {"spArcane"}, "healing": {"healing"},
	"spell_crit": {"crit"}, "intellect": {"int"}, "spirit": {"spi"}, "stamina": {"sta"}, "mp5": {"mp5"},
	"hp5": {"hp5"}, "all_stats": {"int", "sta", "spi"},
}

var planUnits = map[string]string{"dps": "DPS", "hps": "HPS"}

// planTitle is tr(PLAN_TITLES.get(metric, metric)).
func planTitle(metric string) string {
	switch metric {
	case "dps":
		return i18n.T("Damage")
	case "hps":
		return i18n.T("Healing")
	}
	return i18n.T(metric)
}

// Delta is a stat delta in the engine's keys, in the order its keys were added (a Python dict).
type Delta struct {
	keys []string
	vals map[string]float64
}

func newDelta() *Delta { return &Delta{vals: map[string]float64{}} }

func (d *Delta) set(k string, v float64) {
	if _, ok := d.vals[k]; !ok {
		d.keys = append(d.keys, k)
	}
	d.vals[k] = v
}

// Len is the number of keys.
func (d *Delta) Len() int {
	if d == nil {
		return 0
	}
	return len(d.keys)
}

// Map is the delta as the engine takes it (a copy).
func (d *Delta) Map() map[string]float64 {
	m := map[string]float64{}
	if d != nil {
		for k, v := range d.vals {
			m[k] = v
		}
	}
	return m
}

// Obj is the delta as JSON, keys in order.
func (d *Delta) Obj() *pyjson.Obj {
	o := pyjson.New()
	if d != nil {
		for _, k := range d.keys {
			o.Set(k, d.vals[k])
		}
	}
	return o
}

// addDelta is consumables.add_delta: a plus b, every key b touches rounded to 3 decimals.
func addDelta(a, b *Delta) *Delta {
	out := newDelta()
	for _, k := range a.keys {
		out.set(k, a.vals[k])
	}
	for _, k := range b.keys {
		out.set(k, pyfmt.Round(out.vals[k]+b.vals[k], 3))
	}
	return out
}

// fightKind is consumables.fight_kind: the kind of a consumable that works during a fight; "" for eating,
// drinking, bandages and items whose buff has no end the tables know.
func fightKind(it *Item) string {
	cat, sub := it.Cat, it.Subclass
	restores := false
	for _, r := range it.Restore {
		restores = restores || r.Kind == "healing" || r.Kind == "mana"
	}
	if cat == catRune {
		if restores {
			return "rune"
		}
		return ""
	}
	if cat == catPotion || sub == potion {
		if restores || (len(it.Buffs) > 0 && it.duration() != 0) {
			return "potion"
		}
		return ""
	}
	if len(it.Buffs) == 0 || it.duration() == 0 {
		return ""
	}
	if it.WellFed || sub == food {
		return "food"
	}
	if it.Oil {
		return "weapon"
	}
	switch sub {
	case flask:
		return "flask"
	case elixir:
		return "elixir"
	case scroll:
		return "scroll"
	}
	return "other"
}

// fightSlots is consumables.fight_slots: what the item occupies; two consumables that share a slot cannot
// both be in the set.
func fightSlots(iid int64, it *Item, kind string) []string {
	switch kind {
	case "flask", "food", "weapon", "potion", "rune":
		return []string{kind}
	case "elixir", "scroll":
		seen := map[string]bool{}
		out := []string{}
		for _, b := range it.Buffs {
			s := kind + ":" + b.Stat
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
		sort.Strings(out)
		return out
	}
	if it.Cat == catZanza || it.Cat == catBlasted {
		return []string{"cat:" + strconv.FormatInt(it.Cat, 10)}
	}
	return []string{"item:" + strconv.FormatInt(iid, 10)}
}

// floorDiv is Python's float // (float_floor_div in CPython).
func floorDiv(vx, wx float64) float64 {
	mod := math.Mod(vx, wx)
	div := (vx - mod) / wx
	if mod != 0 && (wx < 0) != (mod < 0) {
		div -= 1.0
	}
	if div == 0 {
		return math.Copysign(0, vx/wx)
	}
	floor := math.Floor(div)
	if div-floor > 0.5 {
		floor += 1.0
	}
	return floor
}

// usesIn is consumables.uses_in: how often something with the potion cooldown fits into a fight, at its
// start and every 2 minutes.
func usesIn(seconds float64) int64 {
	return 1 + int64(floorDiv(math.Max(0, seconds-0.001), potionCooldown))
}

// fightDelta is consumables.fight_delta: what the item adds over a fight of seconds, in the combat engine's
// stat keys. A buff shorter than the fight counts for its share of the time; mana and health from a potion or
// a rune count as mana and health per 5 seconds over the fight, a rune's damage to its user as health lost.
func fightDelta(it *Item, kind string, seconds, hpPerStamina float64) *Delta {
	out := newDelta()
	add := func(k string, v float64) { out.set(k, out.vals[k]+v) }
	seconds = math.Max(1.0, seconds)
	duration := it.duration()
	share := 1.0
	if duration != 0 {
		share = math.Min(1.0, float64(duration)/seconds)
	}
	if kind == "potion" || kind == "rune" {
		uses := usesIn(seconds)
		parts := make([]float64, uses)
		for n := int64(0); n < uses; n++ {
			parts[n] = math.Min(float64(duration), seconds-float64(n)*potionCooldown)
		}
		share = pyfmt.Sum(parts...) / seconds
		for _, r := range it.Restore {
			if r.Kind == "mana" {
				add("mp5", float64(r.Amount*uses*5)/seconds)
			} else if r.Kind == "healing" {
				add("hp5", float64(r.Amount*uses*5)/seconds)
			}
		}
		if it.HealthCost != 0 {
			add("hp5", float64(-it.HealthCost*uses*5)/seconds)
		}
	}
	if hpPerStamina == 0 {
		hpPerStamina = 10
	}
	seen := map[Buff]bool{}
	for _, b := range it.Buffs {
		if seen[b] {
			continue
		}
		seen[b] = true
		if b.Stat == "health" {
			add("sta", float64(b.Amount)*share/hpPerStamina)
		}
		for _, k := range engineKeys[b.Stat] {
			add(k, float64(b.Amount)*share)
		}
	}
	rounded := newDelta()
	for _, k := range out.keys {
		if v := pyfmt.Round(out.vals[k], 3); v != 0 {
			rounded.set(k, v)
		}
	}
	return rounded
}

// fightCandidates is consumables.fight_candidates: every consumable for a fight the character can use and
// get, as a row with its kind, slots and delta. A buff that was on the character when the snapshot was taken
// holds its slot (onYou); if one item gives that buff, the item is "active" and adds nothing. One that costs
// more than half of the character's health is "tooCostly" and never part of the set.
func fightCandidates(c *sv.Table, cat *Catalog, x *ctx, seconds float64) []*Row {
	ch := c.Tab("character")
	level := 1.0
	if v := ch.Get("level"); sv.Truthy(v) {
		level = num(v)
	}
	st := ch.Tab("stats")
	hpPerStamina := 10.0
	if v := st.Get("hpPerStamina"); sv.Truthy(v) {
		hpPerStamina = num(v)
	}
	health := num(st.Get("healthMax"))
	on := map[int64]string{}
	for _, b := range ch.Tab("buffs").Iter() {
		t, ok := b.(*sv.Table)
		if !ok || t.IsList() {
			continue
		}
		id, ok := lineKey(t.Get("id"))
		if !ok {
			continue
		}
		on[id] = t.Str("name")
	}
	givers := map[int64]int{} // buff spell -> how many consumables give it
	for _, key := range cat.Items.Keys {
		it := cat.Items.ByKey[key]
		if fightKind(it) != "" {
			for _, a := range it.Auras {
				givers[a]++
			}
		}
	}
	rows := []*Row{}
	for _, key := range cat.Items.Keys {
		it := cat.Items.ByKey[key]
		iid, _ := strconv.ParseInt(key, 10, 64)
		kind := fightKind(it)
		if kind == "" || !classAllowed(it.Classes, x.classFile) {
			continue
		}
		var mine []int64
		for _, a := range it.Auras {
			if _, ok := on[a]; ok {
				mine = append(mine, a)
			}
		}
		active := false
		for _, a := range mine {
			active = active || givers[a] == 1
		}
		if len(mine) == 0 && !x.usable(it, level) {
			continue
		}
		var effect string
		if len(it.Buffs) > 0 {
			var uniq []Buff
			seen := map[Buff]bool{}
			for _, b := range it.Buffs {
				if !seen[b] {
					seen[b] = true
					uniq = append(uniq, b)
				}
			}
			effect = buffText(uniq)
		} else {
			var parts []string
			for _, r := range it.Restore {
				if r.Kind == "healing" || r.Kind == "mana" {
					parts = append(parts, restoreText(r))
				}
			}
			effect = joinComma(parts)
		}
		if it.HealthCost != 0 {
			effect += ", " + i18n.T("costs about {n} health", "n", it.HealthCost)
		}
		row := x.rowOf(iid, it, 0, effect)
		if !active && !obtainable(row) {
			continue
		}
		row.fight = true
		if len(mine) > 0 {
			row.OnYou, row.onYouAt = on[mine[0]], 1
			if row.OnYou == "" {
				row.OnYou = row.Name
			}
		}
		row.Kind, row.Slots, row.Active = kind, fightSlots(iid, it, kind), active
		row.Delta = newDelta()
		if !active {
			row.Delta = fightDelta(it, kind, seconds, hpPerStamina)
		}
		uses := int64(1)
		if kind == "potion" || kind == "rune" {
			row.Uses = usesIn(seconds)
			uses = row.Uses
		}
		if it.HealthCost != 0 {
			hps := pyfmt.Round(float64(it.HealthCost*uses)/math.Max(1.0, seconds), 2)
			row.HealthPerSec = &hps
			row.TooCostly = health != 0 && float64(it.HealthCost) > maxHealthCost*health
		}
		rows = append(rows, row)
	}
	return rows
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// batchFunc is Engine.Batch: the metric gain per key for a list of stat deltas.
type batchFunc func(evals []enginebundle.Eval, reoptimize bool) (map[string]float64, error)

// healthCost is the health a row costs per second, as far as it is taken off what the row adds: for a healer
// somebody has to heal it.
func healthCost(healer bool) func(*Row) float64 {
	return func(r *Row) float64 {
		if healer && r.HealthPerSec != nil {
			return *r.HealthPerSec
		}
		return 0
	}
}

// rateAlone sets what each row adds alone (Gain) and returns the slots a buff of the snapshot holds
// (slot -> the buff's name).
func rateAlone(batch batchFunc, rows []*Row, cost func(*Row) float64) (*slots, error) {
	var evals []enginebundle.Eval
	for _, r := range rows {
		if r.Delta.Len() > 0 {
			evals = append(evals, enginebundle.Eval{Key: strconv.FormatInt(r.ID, 10), Delta: r.Delta.Map()})
		}
	}
	alone, err := batch(evals, true)
	if err != nil {
		return nil, err
	}
	held := newSlots()
	for _, r := range rows {
		c := 0.0
		if r.Delta.Len() > 0 {
			c = cost(r)
		}
		r.Gain = pyfmt.Round(alone[strconv.FormatInt(r.ID, 10)]-c, 2)
		if r.OnYou != "" {
			for _, s := range r.Slots {
				held.set(s, r.OnYou)
			}
		}
	}
	return held, nil
}

// markTaken says for every row outside the set what holds its place: a buff of the snapshot (held) or an
// item of the set (used).
func markTaken(rows []*Row, held, used *slots) {
	for _, r := range rows {
		if r.Chosen != 0 || r.Active {
			continue
		}
		buff, holder := "", ""
		for _, s := range r.Slots {
			if v, ok := held.m[s]; ok && buff == "" {
				buff = v
			}
			if v, ok := used.m[s]; ok && holder == "" {
				holder = v
			}
		}
		if buff != "" {
			if r.onYouAt == 0 {
				r.onYouAt = 2
			}
			r.OnYou = buff
		} else if holder != "" {
			r.BlockedBy = holder
		}
	}
}

// choose is consumables.choose: the best set. What each row adds alone (gain), then greedy by what a row adds
// on top of the ones chosen before it, among the rows the character can get now whose slots are free. Of rows
// that add the same, the one in the bags wins, then the cheaper one. For a healer the health an item costs
// is taken off what it adds. Returns the chosen rows in order, their summed delta and what they add together.
func choose(batch batchFunc, rows []*Row, healer bool) ([]*Row, *Delta, float64, error) {
	cost := healthCost(healer)
	held, err := rateAlone(batch, rows, cost)
	if err != nil {
		return nil, nil, 0, err
	}
	used := held.copy() // slot -> name of what holds it
	var chosen []*Row
	total, gained, paid := newDelta(), 0.0, 0.0 // paid: the health the set costs, per second
	var pool []*Row
	for _, r := range rows {
		if r.Available && r.Delta.Len() > 0 && !r.Active && !r.TooCostly {
			pool = append(pool, r)
		}
	}
	for {
		var free []*Row
		for _, r := range pool {
			if !used.any(r.Slots) {
				free = append(free, r)
			}
		}
		pool = free
		if len(pool) == 0 {
			break
		}
		evals := make([]enginebundle.Eval, len(pool))
		for i, r := range pool {
			evals[i] = enginebundle.Eval{Key: strconv.FormatInt(r.ID, 10), Delta: addDelta(total, r.Delta).Map()}
		}
		gains, err := batch(evals, true)
		if err != nil {
			return nil, nil, 0, err
		}
		more := func(r *Row) float64 { return gains[strconv.FormatInt(r.ID, 10)] - paid - cost(r) - gained }
		type key struct {
			more  float64
			owned bool
			price int64
			id    int64
		}
		keyOf := func(r *Row) key { return key{pyfmt.Round(more(r), 1), r.Owned != 0, -r.price(), -r.ID} }
		greater := func(a, b key) bool {
			if a.more != b.more {
				return a.more > b.more
			}
			if a.owned != b.owned {
				return a.owned
			}
			if a.price != b.price {
				return a.price > b.price
			}
			return a.id > b.id
		}
		best := pool[0]
		for _, r := range pool[1:] {
			if greater(keyOf(r), keyOf(best)) {
				best = r
			}
		}
		adds := more(best)
		if adds < eps {
			break
		}
		best.Chosen, best.Adds = len(chosen)+1, pyfmt.Round(adds, 2)
		chosen = append(chosen, best)
		total, gained, paid = addDelta(total, best.Delta), gained+adds, paid+cost(best)
		for _, s := range best.Slots {
			used.set(s, best.Name)
		}
	}
	markTaken(rows, held, used)
	return chosen, total, gained, nil
}

// slots is a dict slot -> name.
type slots struct{ m map[string]string }

func newSlots() *slots           { return &slots{m: map[string]string{}} }
func (s *slots) set(k, v string) { s.m[k] = v }
func (s *slots) any(keys []string) bool {
	for _, k := range keys {
		if _, ok := s.m[k]; ok {
			return true
		}
	}
	return false
}
func (s *slots) copy() *slots {
	c := newSlots()
	for k, v := range s.m {
		c.m[k] = v
	}
	return c
}

// plan is one plan of fight_plan.
type plan struct {
	metric, unit, title string
	seconds             any
	targetDelta         int
	base, value, gain   float64
	gainPct             float64
	cost                int64
	unpriced            int
	chosen              []*Row
	kinds               []*pyjson.Obj
	total               *Delta
	engine              string
	active              []string
	rows                []*Row
	set                 Set
	perHour             int64   // by value: what an hour of fighting with the set costs, in copper
	steps               []*step // by value: how the set was built, the buying order
}

// Obj is the plan as fight_plan returns it.
func (p *plan) Obj() *pyjson.Obj {
	objOf := map[*Row]*pyjson.Obj{}
	obj := func(r *Row) *pyjson.Obj {
		if o, ok := objOf[r]; ok {
			return o
		}
		o := r.Obj()
		objOf[r] = o
		return o
	}
	chosen := make([]any, len(p.chosen))
	for i, r := range p.chosen {
		chosen[i] = obj(r)
	}
	kindsOut := make([]any, len(p.kinds))
	for i, k := range p.kinds {
		items := k.Get("items").([]*Row)
		list := make([]any, len(items))
		for j, r := range items {
			list[j] = obj(r)
		}
		o := pyjson.New()
		for _, key := range k.Keys() {
			if key == "items" {
				o.Set(key, list)
			} else {
				o.Set(key, k.Get(key))
			}
		}
		kindsOut[i] = o
	}
	active := make([]any, len(p.active))
	for i, a := range p.active {
		active[i] = a
	}
	o := pyjson.New("metric", p.metric, "unit", p.unit, "title", p.title, "seconds", p.seconds,
		"targetDelta", p.targetDelta, "base", p.base, "value", p.value, "gain", p.gain, "gainPct", p.gainPct,
		"cost", p.cost, "unpriced", p.unpriced, "chosen", chosen, "kinds", kindsOut, "delta", p.total.Obj(),
		"engine", p.engine, "active", active)
	if p.set.Value {
		var budget any
		if p.set.Budget > 0 {
			budget = pyfmt.RoundInt(p.set.Budget * copperPerGold)
		}
		replacedBy := map[*Row]string{}
		for _, s := range p.steps {
			for _, x := range s.out {
				replacedBy[x] = s.row.Name
			}
		}
		steps := make([]any, len(p.steps))
		for i, s := range p.steps {
			replaces := make([]any, len(s.out))
			for j, x := range s.out {
				replaces[j] = pyjson.New("id", x.ID, "name", x.Name, "quality", x.Quality)
			}
			var worth, later any
			if g := perGold(s.more, s.cost); g != nil {
				worth = *g
			}
			if name, ok := replacedBy[s.row]; ok {
				later = name
			}
			steps[i] = pyjson.New("n", i+1, "item", obj(s.row), "adds", pyfmt.Round(s.more, 2),
				"costPerHour", pyfmt.RoundInt(s.cost), "perGold", worth, "value", pyfmt.Round(p.base+s.gained, 2),
				"spent", pyfmt.RoundInt(s.spent), "replaces", replaces, "replacedBy", later)
		}
		o.Set("mode", "value")
		o.Set("costPerHour", p.perHour)
		o.Set("budget", budget)
		o.Set("steps", steps)
	}
	return o
}

// fightPlan is consumables.fight_plan: the consumables for one metric, the rows per kind with their gain,
// the best set and the metric with it. With set.Value the set is the one by value (chooseValue) and the rows
// of a kind come in the order of their value.
func fightPlan(metric string, start starter, candidates []*Row, seconds float64, top int, set Set) (*plan, error) {
	rows := make([]*Row, len(candidates))
	for i, r := range candidates {
		c := *r
		rows[i] = &c
	}
	eng, err := start(metric)
	if err != nil {
		return nil, err
	}
	var chosen []*Row
	var total *Delta
	var gained float64
	var steps []*step
	if set.Value {
		chosen, total, gained, steps, err = chooseValue(eng.Batch, rows, metric == "hps", seconds, set.Budget)
	} else {
		chosen, total, gained, err = choose(eng.Batch, rows, metric == "hps")
	}
	if err != nil {
		return nil, err
	}
	base := 0.0
	if eng.Baseline != nil {
		base = eng.Baseline.Value
	}
	value := base + gained
	var kindsOut []*pyjson.Obj
	for _, k := range kinds {
		var mine []*Row
		for _, r := range rows {
			if r.Kind == k.key {
				mine = append(mine, r)
			}
		}
		if len(mine) == 0 {
			continue
		}
		sort.SliceStable(mine, func(i, j int) bool {
			a, b := mine[i], mine[j]
			if a.Gain != b.Gain {
				return a.Gain > b.Gain
			}
			if a.Available != b.Available {
				return a.Available
			}
			if a.price() != b.price() {
				return a.price() < b.price()
			}
			return a.ID < b.ID
		})
		if set.Value {
			byValue(mine)
		}
		var keep []*Row
		for i, r := range mine {
			if top == 0 || i < top || r.Chosen != 0 || r.Active {
				keep = append(keep, r)
			}
		}
		if keep == nil {
			keep = []*Row{}
		}
		var assumed any
		if k.assumed != "" {
			assumed = i18n.T(k.assumed)
		}
		kindsOut = append(kindsOut, pyjson.New("key", k.key, "label", i18n.T(k.title), "rule", i18n.T(k.rule),
			"assumed", assumed, "items", keep, "more", len(mine)-len(keep)))
	}
	var secondsOut any = seconds
	if s := eng.Fight.Seconds; s != 0 {
		secondsOut = s
		if s == math.Trunc(s) && math.Abs(s) < 1<<53 {
			secondsOut = int64(s) // the engine's JSON answer has an integral number as an int
		}
	}
	p := &plan{metric: metric, unit: planUnits[metric], title: planTitle(metric), seconds: secondsOut,
		targetDelta: eng.Fight.TargetDelta, base: pyfmt.Round(base, 2), value: pyfmt.Round(value, 2),
		gain: pyfmt.Round(value-base, 2), chosen: chosen, kinds: kindsOut, total: total, engine: eng.Version,
		rows: rows, set: set, steps: steps}
	if base != 0 {
		p.gainPct = pyfmt.Round((value-base)/base*100, 1)
	}
	for _, r := range chosen {
		if r.PerHour != nil {
			p.perHour += *r.PerHour
		}
		if r.Owned == 0 {
			p.cost += r.price()
			if r.price() == 0 {
				p.unpriced++
			}
		}
	}
	seen := map[string]bool{}
	p.active = []string{}
	for _, r := range rows {
		if r.OnYou != "" && !seen[r.OnYou] {
			seen[r.OnYou] = true
			p.active = append(p.active, r.OnYou)
		}
	}
	return p, nil
}

// fightPlans is consumables.fight_plans: one plan per metric of the class (the spec's own first; the time
// until the mana is gone only when asked for by name).
func fightPlans(c *sv.Table, cat *Catalog, x *ctx, start starter, seconds float64, top int,
	metrics []string, set Set) ([]*plan, error) {
	classFile := c.Tab("character").Str("classFile")
	first := enginebundle.DefaultMetric(classFile, enginebundle.SpecOf(c))
	if len(metrics) == 0 {
		metrics = nil
		for _, m := range enginebundle.Metrics[classFile] {
			if m != "ttoom" {
				metrics = append(metrics, m)
			}
		}
		sort.SliceStable(metrics, func(i, j int) bool { return metrics[i] == first && metrics[j] != first })
	}
	rows := fightCandidates(c, cat, x, seconds)
	var out []*plan
	for _, m := range metrics {
		if m == "" {
			m = first
		}
		p, err := fightPlan(m, start, rows, seconds, top, set)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
