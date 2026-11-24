// Package upgrades is the Go side of scripts/upgrades.py. So far it has the weight scale (the Pawn-like
// rating in points of shadow spell power that the Character page and the upgrades without the combat engine
// use) and the item helpers around it: stat keys, an item's stats in the scale's units, the value of a set,
// the short stat text and the tooltip rebuilt from the stats.
package upgrades

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// Profile is one of upgrades.PROFILES: the enemies' level above the character, how much one percent more mana
// pool and more health count, and the fixed weights.
type Profile struct {
	Label                    string
	Target                   int
	ManaShare, SurvivalShare float64
	Weights                  []Weight // in the order of the Python dict
}

// Weight is one stat weight; the order of a list of them is the order Python sums them in.
type Weight struct {
	Key   string
	Value float64
}

// Profiles are upgrades.PROFILES.
var Profiles = map[string]Profile{
	"leveling": {Label: i18n.Mark("Leveling"), Target: 2, ManaShare: 0.10, SurvivalShare: 0.05,
		Weights: []Weight{{"stamina", 0.5}, {"intellect", 0.35}, {"spirit", 0.25}, {"mp5", 0.7}, {"hp5", 0.15},
			{"armor", 0.01}, {"wand_dps", 0.6}}},
	"dungeon": {Label: i18n.Mark("Dungeon/raid"), Target: 3, ManaShare: 0.06, SurvivalShare: 0.02,
		Weights: []Weight{{"stamina", 0.25}, {"intellect", 0.25}, {"spirit", 0.05}, {"mp5", 0.3}, {"hp5", 0.0},
			{"armor", 0.0}, {"wand_dps", 0.1}}},
}

// The constants of upgrades.py.
const (
	BaseSpellCrit     = 1.7
	ManaPerInt        = 15
	HPPerStamina      = 10
	ImprovedLifeTap   = 0.1
	ImprovedLifeTapID = 18182
	Pandemic          = 427712
	MinMiss           = 0.0
	BasePerLevel      = 13
	CritBonus         = 0.5
	NewID             = 200000
)

// SpellMiss is the chance of spells to miss enemies 0 to 3 levels above (upgrades.SPELL_MISS).
var SpellMiss = map[int]float64{0: 4.0, 1: 5.0, 2: 6.0, 3: 17.0}

// DefaultRatings is the rating per 1 % hit and crit (upgrades.DEFAULT_RATINGS).
var DefaultRatings = map[string]float64{"hit": 10.0, "crit": 14.0}

var spKeys = []string{"spell_power", "spell_damage_done", "shadow_damage_done"}

var linearKeys = []struct{ raw, name string }{{"stamina", "stamina"}, {"intellect", "intellect"},
	{"spirit", "spirit"}, {"mana_regeneration", "mp5"}, {"health_regen", "hp5"}, {"armor", "armor"}}

// pyS is Python's \s in a str pattern: every Unicode white space, the no-break space of a German tooltip too.
const pyS = `[\t\n\v\f\r \x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`

// PctLine finds the percent of hit and crit in a tooltip line, in English and German (upgrades.PCT_LINE).
var PctLine = []struct {
	Kind string
	Re   *regexp.Regexp
}{
	{"hit", regexp.MustCompile(`(?:Improves your chance to hit by|Verbessert Eure Trefferchance um) ([0-9.,]+)` + pyS + `*%`)},
	{"crit", regexp.MustCompile(`(?:Improves your chance to get a critical strike by|Erhöht Eure kritische Trefferchance um) ([0-9.,]+)` + pyS + `*%`)},
}

var LifeTapText = regexp.MustCompile(`([0-9.,]+)` + pyS + `*(?:Health|Gesundheit)[^0-9]+?([0-9.,]+)` + pyS + `*Mana`)
var thousands = regexp.MustCompile(`[.,]`)

// SlotName holds the names of the inventory slots (upgrades.SLOT_NAME).
var SlotName = map[int64]string{1: i18n.Mark("Head"), 2: i18n.Mark("Neck"), 3: i18n.Mark("Shoulder"),
	5: i18n.Mark("Chest"), 6: i18n.Mark("Waist"), 7: i18n.Mark("Legs"), 8: i18n.Mark("Feet"),
	9: i18n.Mark("Wrist"), 10: i18n.Mark("Hands"), 11: i18n.Mark("Finger"), 12: i18n.Mark("Finger"),
	13: i18n.Mark("Trinket"), 14: i18n.Mark("Trinket"), 15: i18n.Mark("Back"), 16: i18n.Mark("Main Hand"),
	17: i18n.Mark("Off Hand"), 18: i18n.Mark("Wand")}

// LoadChar is upgrades.load_char: the WoWToolsForever_Char table of a snapshot, empty if the file has none.
func LoadChar(path string) (*sv.Table, error) { return sv.LoadTable(path, "WoWToolsForever_Char") }

// SlotLabel is upgrades.slot_name: the slot's name in the selected language.
func SlotLabel(slot int64) string { return i18n.T(SlotName[slot]) }

// Number is upgrades.number: a whole number from a tooltip, with the thousands separator of either language.
func Number(text string) float64 {
	f, _ := strconv.ParseFloat(thousands.ReplaceAllString(text, ""), 64)
	return f
}

// StatKey is upgrades.stat_key: ITEM_MOD_STAMINA_SHORT -> stamina, RESISTANCE0_NAME -> armor.
func StatKey(k string) string {
	if k == "RESISTANCE0_NAME" {
		return "armor"
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(k, "ITEM_MOD_"), "_SHORT"))
}

// Stats are stats in the units of the scale: sp, stamina, intellect, spirit, mp5, hp5, armor, wand_dps, hit
// and crit in percent.
type Stats map[string]float64

// rawStats is the item's numeric stats under their stat_key names.
func rawStats(rec *sv.Table) map[string]float64 {
	raw := map[string]float64{}
	stats := rec.Tab("stats")
	for i, k := range stats.Keys() {
		key, ok := k.(string)
		if !ok {
			key = pyfmt.Str(k)
		}
		v := stats.Values()[i]
		if b, isBool := v.(bool); isBool { // isinstance(True, int) in Python
			if b {
				raw[StatKey(key)] = 1
			} else {
				raw[StatKey(key)] = 0
			}
			continue
		}
		if f, ok := sv.Num(v); ok {
			raw[StatKey(key)] = f
		}
	}
	return raw
}

// tooltipPct is the sum of the percentages a pattern finds in the tooltip lines.
func tooltipPct(rec *sv.Table, re *regexp.Regexp) float64 {
	var pcts []float64
	for _, line := range rec.Tab("tooltip").List() {
		s, ok := line.(string)
		if !ok {
			continue
		}
		if m := re.FindStringSubmatch(s); m != nil {
			f, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
			pcts = append(pcts, f)
		}
	}
	return pyfmt.Sum(pcts...)
}

// NormStats is upgrades.norm_stats: an item's stats in the units of the scale. Everything the scale ignores
// (healing, other schools, melee stats) is dropped, and so is every stat that is 0.
func NormStats(rec *sv.Table, ranged bool, ratings map[string]float64) Stats {
	if ratings == nil {
		ratings = DefaultRatings
	}
	raw := rawStats(rec)
	n := Stats{}
	sps := make([]float64, len(spKeys))
	for i, k := range spKeys {
		sps[i] = raw[k]
	}
	n["sp"] = pyfmt.Sum(sps...)
	for _, lk := range linearKeys {
		n[lk.name] = raw[lk.raw]
	}
	if ranged {
		n["wand_dps"] = raw["damage_per_second"]
	}
	for _, pl := range PctLine {
		if rating := raw[pl.Kind+"_rating"]; rating != 0 {
			pct := tooltipPct(rec, pl.Re)
			if pct == 0 {
				pct = rating / ratings[pl.Kind]
			}
			n[pl.Kind] = pct
		}
	}
	for k, v := range n {
		if v == 0 {
			delete(n, k)
		}
	}
	return n
}

// Add is upgrades.add_stats: total += sign × n.
func (total Stats) Add(n Stats, sign float64) {
	for k, v := range n {
		total[k] += sign * v
	}
}

// Copy is dict(stats).
func (total Stats) Copy() Stats {
	out := Stats{}
	for k, v := range total {
		out[k] = v
	}
	return out
}

// EquippedStats is upgrades.equipped_stats: the scale's stats per equipped slot and their sum, in the order
// of the equipment list (the order of the additions decides the last bits of a float sum).
func EquippedStats(char *sv.Table, ratings map[string]float64) (map[int64]Stats, Stats) {
	eq := map[int64]Stats{}
	var order []int64
	for _, e := range char.Tab("equipment").Tables() {
		slot, ok := e.Get("slot").(int64)
		if !ok || slot == 0 {
			continue
		}
		if _, seen := eq[slot]; !seen {
			order = append(order, slot)
		}
		eq[slot] = NormStats(e, slot == 18, ratings)
	}
	total := Stats{}
	for _, slot := range order {
		total.Add(eq[slot], 1)
	}
	return eq, total
}

// Scale is the result of upgrades.build_scale.
type Scale struct {
	Profile  string
	Label    string
	Pct      float64
	Weights  []Weight // sp, crit, then the profile's weights, with intellect and stamina replaced
	HitSteps [][2]float64
	Rows     []*pyjson.Obj
	Text     string
}

func (s *Scale) weight(k string) float64 {
	for _, w := range s.Weights {
		if w.Key == k {
			return w.Value
		}
	}
	return 0
}

func (s *Scale) setWeight(k string, v float64) {
	for i := range s.Weights {
		if s.Weights[i].Key == k {
			s.Weights[i].Value = v
			return
		}
	}
	s.Weights = append(s.Weights, Weight{k, v})
}

// num is a number field, 0 if it is missing or not a number (Python's `x or 0`).
func num(t *sv.Table, k string) float64 {
	f, _ := t.Num(k)
	return f
}

// orNum is `t.get(k) or def`.
func orNum(t *sv.Table, k string, def float64) float64 {
	if f, ok := t.Num(k); ok && f != 0 {
		return f
	}
	return def
}

// critPerIntellect is upgrades.crit_per_intellect: spell crit in percent per point of intellect and where
// the number comes from; ok is false where Python returns None.
func critPerIntellect(st *sv.Table, gearCrit float64) (float64, string, bool) {
	intellect := num(st, "intellect")
	if intellect <= 0 {
		return 0, "", false
	}
	if v := st.Get("critFromIntellect"); v != nil {
		f, _ := sv.Num(v)
		return f / intellect, i18n.T("from the game"), true
	}
	crit := st.Tab("spellCrit").Get("shadow")
	if crit == nil {
		return 0, "", false
	}
	c, _ := sv.Num(crit)
	return math.Max(0.0, (c-BaseSpellCrit-gearCrit)/intellect),
		i18n.T("estimated: spell crit − {base:g}%", "base", BaseSpellCrit), true
}

// lifeTapRatio is upgrades.life_tap_ratio: mana per point of health from Life Tap, with Improved Life Tap.
func lifeTapRatio(st *sv.Table, level float64, iltRank int64) (float64, string) {
	lt := st.Get("lifeTap")
	ltTab, _ := lt.(*sv.Table)
	text := ltTab.Str("text")
	var ratio float64
	var source string
	if m := LifeTapText.FindStringSubmatch(text); m != nil && Number(m[1]) > 0 {
		ratio = Number(m[2]) / Number(m[1])
		source = i18n.T("from the spell text")
	} else if lt == nil && st.Has("hpPerStamina") { // addon 1.3 exports and knows no Life Tap
		return 0.0, i18n.T("Life Tap not known")
	} else if level < 6 {
		return 0.0, i18n.T("Life Tap only from level 6")
	} else {
		ratio, source = 1.0, i18n.T("assumed 1:1")
	}
	if iltRank != 0 {
		ratio *= 1 + ImprovedLifeTap*float64(iltRank)
		source += i18n.T(", Improved Life Tap {rank}/2", "rank", iltRank)
	}
	return ratio, source
}

// talentRanks is spell ID -> rank of the class talents (later entries win, as in the Python dict).
func talentRanks(char *sv.Table) map[int64]int64 {
	out := map[int64]int64{}
	for _, tree := range char.Tab("talents").Tab("class").Tables() {
		for _, t := range tree.Tab("talents").Tables() {
			id, _ := t.Get("spellID").(int64)
			rank, _ := t.Get("rank").(int64)
			out[id] = rank
		}
	}
	return out
}

// BuildScale is upgrades.build_scale: the weights for this character, in points of shadow spell power.
func BuildScale(char *sv.Table, profile string, gearHit, gearCrit float64) *Scale {
	ch := char.Tab("character")
	st := ch.Tab("stats")
	level := orNum(ch, "level", 60)
	if _, ok := Profiles[profile]; !ok {
		profile = "dungeon"
		if level < 60 {
			profile = "leveling"
		}
	}
	p := Profiles[profile]
	talents := talentRanks(char)
	spellPower := orNum(st.Tab("spellDamage"), "shadow", 0)
	pct := (BasePerLevel*level + spellPower) / 100

	baseHit := orNum(st, "spellHit", 0)
	baseHitV := st.Get("spellHit") // formatted as Python formats the exported number
	if !sv.Truthy(baseHitV) {
		baseHitV = int64(0)
	}
	cap := SpellMiss[p.Target] - MinMiss
	need := math.Max(0.0, cap-baseHit)
	s := &Scale{Profile: profile, Label: i18n.T(p.Label), Pct: pct, HitSteps: [][2]float64{{need, 1.0}}}

	pandemic := talents[Pandemic]
	critShare := 0.25 + 0.6*float64(min(pandemic, 3))/3
	s.Weights = []Weight{{"sp", 1.0}, {"crit", pyfmt.Round(pct*critShare*CritBonus, 2)}}
	s.Weights = append(s.Weights, p.Weights...)

	var shares []float64
	for _, step := range s.HitSteps {
		if gearHit < step[0] {
			shares = append(shares, step[1])
		}
	}
	hitNow := pct * pyfmt.Sum(shares...)
	var hitNote string
	if need <= 0 {
		hitNote = i18n.T("{base:g}% without gear, the cap against +{target} levels is {cap:g}%: already reached",
			"base", baseHitV, "target", p.Target, "cap", cap)
	} else {
		hitNote = i18n.T("cap against +{target} levels {cap:g}%, you have {base:g}%, {need:g}% missing",
			"target", p.Target, "cap", cap, "base", baseHitV, "need", need)
		hitNote += i18n.T(", gear gives {gear:g}%", "gear", gearHit)
	}
	critNote := i18n.T("1% damage = {points:.1f} spell power, {share:.0%} of the damage can crit",
		"points", pct, "share", critShare)
	if pandemic != 0 {
		critNote += i18n.T(" (with Pandemic your DoTs crit)")
	}

	staminaNote := i18n.T("health, and mana through Life Tap (fixed value)")
	intellectNote := i18n.T("15 mana and some crit (fixed value)")
	mana, health := num(st, "powerMax"), num(st, "healthMax")
	cpi, cpiSource, cpiOK := critPerIntellect(st, gearCrit)
	if mana > 0 && health > 0 && cpiOK {
		tap, tapSource := lifeTapRatio(st, level, talents[ImprovedLifeTapID])
		pool := mana + health*tap
		perMana := p.ManaShare * pct * 100 / pool
		perHealth := p.SurvivalShare * pct * 100 / health
		mpi := orNum(st, "manaPerIntellect", ManaPerInt)
		hps := orNum(st, "hpPerStamina", HPPerStamina)
		mpiV, hpsV := orValue(st, "manaPerIntellect", int64(ManaPerInt)), orValue(st, "hpPerStamina", int64(HPPerStamina))
		intMana, intCrit := mpi*perMana, cpi*s.weight("crit")
		staTap, staLive := hps*tap*perMana, hps*perHealth
		s.setWeight("intellect", pyfmt.Round(intMana+intCrit, 3))
		s.setWeight("stamina", pyfmt.Round(staTap+staLive, 3))
		intellectNote = i18n.T("{mana:g} mana = {manaValue:.2f}, {crit:.4f}% crit ({source}) = {critValue:.2f}. "+
			"1 mana = {perMana:.3f}: pool {pool:.0f} (mana + health through Life Tap), "+
			"1% more pool counts like {share:g}% damage", "mana", mpiV, "manaValue", intMana, "crit", cpi,
			"source", cpiSource, "critValue", intCrit, "perMana", perMana, "pool", pool, "share", p.ManaShare)
		staminaNote = i18n.T("{health:g} health: {tapMana:.1f} mana through Life Tap ({source}) = {tapValue:.2f}, "+
			"survival = {liveValue:.2f} (1% more health counts like {share:g}% damage)", "health", hpsV,
			"tapMana", hps*tap, "source", tapSource, "tapValue", staTap, "liveValue", staLive,
			"share", p.SurvivalShare)
	}
	w := s.weight
	row := func(label string, weight float64, note string) *pyjson.Obj {
		return pyjson.New("label", label, "weight", weight, "note", note)
	}
	s.Rows = []*pyjson.Obj{
		row(i18n.T("Shadow / spell power"), 1.0, i18n.T("reference")),
		row(i18n.T("Stamina"), w("stamina"), staminaNote),
		row(i18n.T("Intellect"), w("intellect"), intellectNote),
		row(i18n.T("Spirit"), w("spirit"), i18n.T("regeneration out of combat")),
		row(i18n.T("Mana per 5 sec."), w("mp5"), ""),
		row(i18n.T("Hit, next 1%"), pyfmt.Round(hitNow, 2), hitNote),
		row(i18n.T("Crit, per 1%"), w("crit"), critNote),
		row(i18n.T("Wand DPS"), w("wand_dps"), i18n.T("wand slot only")),
	}
	s.Text = i18n.T("Shadow/spell power 1, Stamina {stamina:.2f}, Int {intellect:.2f}, Spirit {spirit:g}, Mana/5s {mp5:g}, "+
		"Hit 1% = {hit:.1f}, Crit 1% = {crit:.1f}, Wand DPS {wand:g}", "stamina", w("stamina"),
		"intellect", w("intellect"), "spirit", w("spirit"), "mp5", w("mp5"), "hit", hitNow, "crit", w("crit"),
		"wand", w("wand_dps"))
	return s
}

// orValue is `t.get(k) or def` with the value's own type (an exported int formats as an int).
func orValue(t *sv.Table, k string, def any) any {
	if v := t.Get(k); sv.Truthy(v) {
		return v
	}
	return def
}

// SetValue is upgrades.set_value: the value of a whole set of stats. Linear except for hit, which stops
// counting at the cap.
func SetValue(total Stats, s *Scale) float64 {
	terms := make([]float64, len(s.Weights))
	for i, w := range s.Weights {
		terms[i] = w.Value * total[w.Key]
	}
	hit := math.Max(0.0, total["hit"])
	steps := make([]float64, len(s.HitSteps))
	for i, step := range s.HitSteps {
		steps[i] = step[1] * math.Min(hit, step[0])
	}
	// Python's sum() is compensated (pyfmt.Sum): a plain loop differs in the last bits, and a score
	// rounded to one decimal then flips at its half.
	return pyfmt.Sum(terms...) + s.Pct*pyfmt.Sum(steps...)
}

var shortName = []struct{ key, name string }{{"spell_power", i18n.Mark("SP")},
	{"spell_damage_done", i18n.Mark("SD")}, {"shadow_damage_done", i18n.Mark("Shadow")},
	{"stamina", i18n.Mark("Sta")}, {"intellect", i18n.Mark("Int")}, {"spirit", i18n.Mark("Spi")},
	{"hit_rating", i18n.Mark("Hit rating")}, {"crit_rating", i18n.Mark("Crit rating")},
	{"mana_regeneration", i18n.Mark("Mana/5s")}, {"damage_per_second", i18n.Mark("DPS")}}

// statValues is the item's stats under their stat_key names with their own values (int or float).
func statValues(stats *sv.Table) map[string]any {
	raw := map[string]any{}
	for i, k := range stats.Keys() {
		key, ok := k.(string)
		if !ok {
			key = pyfmt.Str(k)
		}
		raw[StatKey(key)] = stats.Values()[i]
	}
	return raw
}

// roundG is f"{round(v, 1):g}" for an int or a float.
func roundG(v any) string {
	if i, ok := v.(int64); ok {
		s, _ := pyfmt.Spec(i, "g")
		return s
	}
	f, _ := sv.Num(v)
	s, _ := pyfmt.Spec(pyfmt.Round(f, 1), "g")
	return s
}

// Short is upgrades.short: the stats the window shows next to an item ("SP 19, Int 20").
func Short(stats *sv.Table) string {
	raw := statValues(stats)
	var parts []string
	for _, sn := range shortName {
		v, ok := raw[sn.key]
		if !ok {
			continue
		}
		if _, isNum := sv.Num(v); isNum {
			parts = append(parts, i18n.T(sn.name)+" "+roundG(v))
		} else if b, isBool := v.(bool); isBool {
			n := int64(0)
			if b {
				n = 1
			}
			parts = append(parts, i18n.T(sn.name)+" "+roundG(n))
		} else {
			parts = append(parts, i18n.T(sn.name))
		}
	}
	return strings.Join(parts, ", ")
}

var statLabel = []struct{ key, label string }{{"stamina", i18n.Mark("Stamina")},
	{"intellect", i18n.Mark("Intellect")}, {"spirit", i18n.Mark("Spirit")}, {"strength", i18n.Mark("Strength")},
	{"agility", i18n.Mark("Agility")}}

var spellLabel = []struct{ key, text string }{
	{"spell_power", i18n.Mark("Equip: Increases damage and healing done by magical spells and effects by up to {n}.")},
	{"spell_damage_done", i18n.Mark("Equip: Increases damage done by magical spells and effects by up to {n}.")},
	{"shadow_damage_done", i18n.Mark("Equip: Increases damage done by Shadow spells and effects by up to {n}.")},
	{"spell_healing_done", i18n.Mark("Equip: Increases healing done by spells and effects by up to {n}.")},
}

// gspec is f"{v:g}" of an exported number.
func gspec(v any) string {
	s, _ := pyfmt.Spec(v, "g")
	return s
}

// Tooltip is upgrades.tooltip: the game's own lines when the addon exported them, otherwise lines rebuilt
// from the stats. slotLabel is nil where Python passes None.
func Tooltip(rec *sv.Table, slotLabel *string) *pyjson.Obj {
	out := pyjson.New("name", rec.Get("name"), "quality", rec.Get("quality"), "ilvl", rec.Get("ilvl"))
	if sv.Truthy(rec.Get("tooltip")) {
		out.Set("lines", rec.Get("tooltip"))
		out.Set("colors", rec.Get("tipColors"))
		return out
	}
	var lines, colors []any
	var equip []any
	label := ""
	if slotLabel != nil {
		label = *slotLabel
	}
	if label != "" || sv.Truthy(rec.Get("subType")) {
		sub, _ := rec.Get("subType").(string)
		lines = append(lines, strings.Trim(label+" | "+sub, " |"))
		colors = append(colors, "ffffff")
	}
	stats := statValues(rec.Tab("stats"))
	if sv.Truthy(stats["armor"]) {
		lines = append(lines, i18n.T("{n:g} Armor", "n", stats["armor"]))
		colors = append(colors, "ffffff")
	}
	if sv.Truthy(stats["damage_per_second"]) {
		lines = append(lines, i18n.T("({n:.1f} damage per second)", "n", stats["damage_per_second"]))
		colors = append(colors, "ffffff")
	}
	for _, sl := range statLabel {
		if sv.Truthy(stats[sl.key]) {
			lines = append(lines, "+"+gspec(stats[sl.key])+" "+i18n.T(sl.label))
			colors = append(colors, "ffffff")
		}
	}
	if dur := rec.Tab("durability"); dur != nil && dur.IsList() && dur.Len() == 2 {
		d := dur.List()
		lines = append(lines, i18n.T("Durability {now} / {max}", "now", d[0], "max", d[1]))
		colors = append(colors, "ffffff")
	}
	if sv.Truthy(rec.Get("reqLevel")) {
		lines = append(lines, i18n.T("Requires Level {n}", "n", rec.Get("reqLevel")))
		colors = append(colors, "ffffff")
	}
	for _, sl := range spellLabel {
		if sv.Truthy(stats[sl.key]) {
			equip = append(equip, i18n.T(sl.text, "n", gspec(stats[sl.key])))
		}
	}
	lines = append(lines, equip...)
	for range equip {
		colors = append(colors, "1eff00")
	}
	if lines == nil {
		lines = []any{}
	}
	if colors == nil {
		colors = []any{}
	}
	out.Set("lines", lines)
	out.Set("colors", colors)
	out.Set("rebuilt", true)
	return out
}
