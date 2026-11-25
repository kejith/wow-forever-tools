package enginebundle

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/engine"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/internal/tables"
	"github.com/kejith/wow-tools-forever/sim/itemsets"
	"github.com/kejith/wow-tools-forever/sim/run"
)

// Tables are the DB2 tables the engine reads from the shared cache (sim/catalog and sim/itemsets).
// The three trait tables hold the exact value of every talent rank; without them the engine falls
// back to the single value in SpellEffect.
var Tables = []string{"SpellClassOptions", "SpellMisc", "SpellEffect", "SpellCastTimes", "SpellDuration",
	"SpellPower", "SpellLevels", "SpellCooldowns", "SkillLineAbility", "SpellName",
	"TraitDefinition", "TraitDefinitionEffectPoints", "CurvePoint",
	"ItemSet", "ItemSetSpell"}

// ReferenceLocale is sim/catalog.ReferenceLocale: the rank chains are grouped by the English spell names.
const ReferenceLocale = "enUS"

// WandsSkillID is the skill line of wands.
const WandsSkillID = 228

// itemKeys are the ITEM_MOD stats (in stat_key form) as the engine's delta keys.
var itemKeys = map[string]string{
	"spell_power": "sp", "spell_damage_done": "spDamage",
	"spell_healing_done": "healing", "holy_damage_done": "spHoly", "fire_damage_done": "spFire",
	"nature_damage_done": "spNature", "frost_damage_done": "spFrost", "shadow_damage_done": "spShadow",
	"arcane_damage_done": "spArcane",
	"strength": "str", "agility": "agi", "stamina": "sta", "intellect": "int", "spirit": "spi",
	"attack_power": "ap", "ranged_attack_power": "rap", "feral_attack_power": "feralAp",
	"mana_regeneration": "mp5", "health_regen": "hp5",
	"defense_skill_rating": "defense", "dodge_rating": "dodge", "parry_rating": "parry",
	"block_rating": "block", "block_value": "blockValue",
	"haste_rating": "haste", "melee_haste_rating": "meleeHaste",
}

// NormItem is simengine.norm_item: an item's stats as engine delta keys. Unlike the weight scale's
// NormStats this keeps healing, per-school damage and the split between "damage done" and "damage and
// healing done". Hit and crit come as percent from the tooltip (pctLine: kind -> pattern, upgrades.PctLine),
// else as rating / ratings; without ratings they are left out.
func NormItem(rec *sv.Table, ranged bool, ratings map[string]float64, pctLine map[string]*regexp.Regexp) map[string]float64 {
	n := map[string]float64{}
	stats := rec.Tab("stats")
	for _, key := range stats.Keys() {
		val, ok := sv.Num(stats.Get(key))
		if !ok {
			continue
		}
		k := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(sv.KeyString(key), "ITEM_MOD_"), "_SHORT"))
		switch {
		case itemKeys[k] != "":
			n[itemKeys[k]] += val
		case k == "damage_per_second" && ranged:
			n["wandDps"] = val
			n["rangedDps"] = val
		case k == "damage_per_second" && !ranged:
			n["weaponDps"] = val
		case (k == "hit_rating" || k == "crit_rating") && ratings != nil && pctLine != nil:
			kind := strings.TrimSuffix(k, "_rating")
			var pct []float64
			for _, line := range rec.Tab("tooltip").List() {
				text, _ := line.(string)
				if m := pctLine[kind].FindStringSubmatch(text); m != nil {
					f, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
					pct = append(pct, f)
				}
			}
			if p := pyfmt.Sum(pct...); p != 0 {
				n[kind] = p
			} else {
				n[kind] = val / ratings[kind]
			}
		}
	}
	for k, v := range n {
		if v == 0 {
			delete(n, k)
		}
	}
	return n
}

// Delta is simengine.delta: the sum of norm_item dicts, the removed ones subtracted.
func Delta(adds, removes []map[string]float64) map[string]float64 {
	d := map[string]float64{}
	for _, n := range adds {
		for k, v := range n {
			d[k] += v
		}
	}
	for _, n := range removes {
		for k, v := range n {
			d[k] -= v
		}
	}
	for k, v := range d {
		if v == 0 {
			delete(d, k)
		}
	}
	return d
}

// SheetNumbers is simengine.sheet_numbers: the character sheet's own haste and regeneration (addon 1.12)
// as the engine's stats. HasCasting tells whether the sheet's regeneration while casting is in it (the
// Python dict then has "castingRegen").
type SheetNumbers struct {
	HastePct     float64
	SpiritRegen  float64
	CastingRegen *float64
}

// HasCasting is `"castingRegen" in sheet_numbers(...)`.
func (s SheetNumbers) HasCasting() bool { return s.CastingRegen != nil }

// Sheet reads the sheet numbers from a snapshot's character.stats.
func Sheet(st *sv.Table) SheetNumbers {
	out := SheetNumbers{}
	out.HastePct, _ = st.Num("spellHaste")
	regen := st.Tab("manaRegen")
	if spirit, _ := regen.Num("spirit"); spirit != 0 {
		out.SpiritRegen = spirit
		if regen.Has("casting") {
			casting, _ := regen.Num("casting")
			out.CastingRegen = &casting
		}
	}
	return out
}

// Engine is one started engine: the bundle without evaluations, run per batch. The engine runs in this
// process (Runner); what the last run said is kept as simengine.Engine keeps it.
type Engine struct {
	Input           run.Input
	Baseline        *engine.Metrics
	Version         string
	SpellbookSource string
	Downrank        *run.Downrank
	HealOptions     []run.HealOption
	Coverage        *run.Coverage
	Spells          []run.SpellNumbers // per known spell rank what one cast does, as a tooltip shows it
	Fight           engine.Fight       // as evaluated: the engine resolves automatic values
	sess            *run.Session       // cached prepared session when using the default run.Run
}

// Runner runs one bundle; tests replace it with a stub.
var Runner = run.Run

func isDefaultRunner() bool {
	return reflect.ValueOf(Runner).Pointer() == reflect.ValueOf(run.Run).Pointer()
}

func (e *Engine) exec(in run.Input) (out run.Output, err error) {
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("%s", i18n.T("The combat engine failed: {err}", "err", cut(fmt.Sprint(p), 200)))
			}
		}()
		if isDefaultRunner() && !in.Situations && !in.HealTable && !in.DamageTable {
			if e.sess == nil {
				var baseOut run.Output
				e.sess, baseOut = run.Prepare(in)
				if baseOut.Error != "" {
					out = baseOut
					return
				}
			}
			out = e.sess.Eval(in.Evaluations, in.Reoptimize)
			return
		}
		out = Runner(in)
	}()
	if err != nil {
		return out, err
	}
	if out.Error != "" {
		return out, fmt.Errorf("%s", i18n.T("The combat engine reports: {err}", "err", out.Error))
	}
	e.Baseline = out.Baseline
	e.Version = out.Engine
	e.SpellbookSource = out.SpellbookSource
	e.Downrank = out.Downrank
	e.HealOptions = out.HealOptions
	e.Coverage = out.Coverage
	if len(out.Spells) > 0 {
		e.Spells = out.Spells
	}
	if out.Fight != nil {
		e.Fight = *out.Fight
	}
	return out, nil
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// Eval is one stat delta to rate under a key.
type Eval struct {
	Key            string
	Delta          map[string]float64
	NoMC           bool
	HasSetChange   bool
	SetBonusSpells []int
}

// Batch evaluates deltas and returns the metric gain per key. With reoptimize the engine picks the best
// rotation again for each delta instead of keeping the baseline's (for big deltas: consumables).
func (e *Engine) Batch(evals []Eval, reoptimize bool) (map[string]float64, error) {
	in := e.Input
	in.Evaluations = make([]run.Evaluation, len(evals))
	for i, ev := range evals {
		in.Evaluations[i] = run.Evaluation{
			Key: ev.Key, Delta: ev.Delta, NoMC: ev.NoMC,
			HasSetChange: ev.HasSetChange, SetBonusSpells: ev.SetBonusSpells,
		}
	}
	in.Reoptimize = reoptimize
	out, err := e.exec(in)
	if err != nil {
		return nil, err
	}
	gains := map[string]float64{}
	for k, r := range out.Results {
		gains[k] = r.Gain
	}
	return gains, nil
}

// Situations is the rotation guide (sim/run/situations.go): the best rotation in several kinds of fights
// for a damage metric, the cheapest spells per level of demand for a healer.
func (e *Engine) Situations() ([]run.Situation, error) {
	in := e.Input
	in.Evaluations, in.Situations = []run.Evaluation{}, true
	out, err := e.exec(in)
	return out.Situations, err
}

// HealTable is every known heal and shield rank with what one cast gives and costs (sim/run/healtable.go),
// and the rotation guide of the same run.
func (e *Engine) HealTable() (*run.HealTable, []run.Situation, error) {
	in := e.Input
	in.Evaluations, in.HealTable, in.Situations = []run.Evaluation{}, true, true
	out, err := e.exec(in)
	return out.HealTable, out.Situations, err
}

// DamageTable is every known rank of a damage spell with its numbers per cast (sim/run/damagetable.go),
// and the rotation guide of the same run. For an engine started with the metric "dps".
func (e *Engine) DamageTable() (*run.DamageTable, []run.Situation, error) {
	in := e.Input
	in.Evaluations, in.DamageTable, in.Situations = []run.Evaluation{}, true, true
	out, err := e.exec(in)
	return out.DamageTable, out.Situations, err
}

// Options are the optional arguments of simengine.start.
type Options struct {
	Method     string
	Iterations int
	Buffs      map[string]float64 // a stat delta in the engine's keys that every number is to include
	CacheDir   string             // default tables.CacheDir()
}

func num(t *sv.Table, k string) float64 { f, _ := t.Num(k); return f }

func orNum(t *sv.Table, k string, def float64) float64 {
	if f, ok := t.Num(k); ok && f != 0 {
		return f
	}
	return def
}

func numMap(t *sv.Table) map[string]float64 {
	m := map[string]float64{}
	for _, k := range t.Keys() {
		if f, ok := sv.Num(t.Get(k)); ok {
			m[sv.KeyString(k)] = f
		}
	}
	return m
}

// Start is simengine.start: builds the bundle from a WoW Tools Forever snapshot and returns a ready Engine. lifeTap is
// parsed from the spell text by the caller; fight has negative kills for automatic (the answer has the
// fight as evaluated, Engine.Fight).
func Start(c *sv.Table, gearMp5, gearHp5 float64, fight engine.Fight, metric string, lifeTap engine.LifeTap,
	opt Options) (*Engine, error) {
	ch := c.Tab("character")
	st := ch.Tab("stats")
	spec := SpecOf(c)
	if metric == "" {
		metric = DefaultMetric(ch.Str("classFile"), spec)
	}
	client := c.Tab("client")
	if !sv.Truthy(client.Get("version")) || !sv.Truthy(client.Get("build")) {
		return nil, fmt.Errorf("%s", i18n.T("The snapshot does not name the client build; /wtf sync and /reload first."))
	}
	build := pyfmt.Str(client.Get("version")) + "." + pyfmt.Str(client.Get("build"))
	locale := client.Str("locale")
	if locale == "" {
		locale = "deDE"
	}
	cache := opt.CacheDir
	if cache == "" {
		cache = tables.CacheDir()
	}
	for _, t := range Tables {
		tables.Ensure(cache, t, build, locale)
	}
	if locale != ReferenceLocale {
		tables.Ensure(cache, "SpellName", build, ReferenceLocale)
	}

	intellect := num(st, "intellect")
	critPerInt := 0.0
	if cfi, ok := st.Num("critFromIntellect"); intellect != 0 && ok {
		critPerInt = cfi / intellect
	}
	skillRanks := map[int]int{}
	wandSkill := 0.0
	for _, s := range c.Tab("skills").Tab("skills").Tables() {
		if id, ok := s.Num("skillID"); ok && s.Has("skillID") {
			skillRanks[int(id)] = int(num(s, "rank"))
			if id == WandsSkillID {
				wandSkill = num(s, "rank") + num(s, "modifier")
			}
		}
	}
	var wand *sv.Table
	var equippedIDs []int64
	for _, e := range c.Tab("equipment").Tables() {
		if slot, _ := e.Num("slot"); slot == 18 {
			wand = e
		}
		if id, ok := e.Num("id"); ok && id > 0 {
			equippedIDs = append(equippedIDs, int64(id))
		}
	}
	setCat := itemsets.Load(cache, build, locale)
	activeBonuses := setCat.ActiveBonuses(equippedIDs, skillRanks)
	setBonusSpells := itemsets.AllSpellIDs(activeBonuses)
	sheet := Sheet(st)
	for _, b := range activeBonuses {
		if v := b.Stats["hp5"]; v != 0 {
			gearHp5 += v
		}
		if !sheet.HasCasting() {
			if v := b.Stats["mp5"]; v != 0 {
				gearMp5 += v
			}
		}
	}
	wandDps := NormItem(wand, true, nil, nil)["wandDps"]
	wandSpeed := parseWandSpeed(wand)
	talents := map[string]int{}
	for _, tree := range c.Tab("talents").Tab("class").Tables() {
		for _, t := range tree.Tab("talents").Tables() {
			if sv.Truthy(t.Get("spellID")) {
				talents[pyfmt.Str(t.Get("spellID"))] = int(num(t, "rank"))
			}
		}
	}
	var spellbook []int
	for _, k := range c.Tab("spellbook").Tab("spells").Keys() {
		if id, err := strconv.Atoi(sv.KeyString(k)); err == nil {
			spellbook = append(spellbook, id)
		}
	}
	sort.Ints(spellbook)

	level := int(num(ch, "level"))
	if level == 0 {
		level = 60
	}
	classFile := ch.Str("classFile")
	mh, oh, rangedWep, petDPS := physicalWeapons(c, ch, st, classFile)
	agility := num(st, "agility")
	critPerAgi := 0.0
	if cfa, ok := st.Num("critFromAgility"); agility > 0 && ok {
		critPerAgi = cfa / agility
	}
	in := run.Input{
		CacheDir: cache, Build: build, Locale: locale,
		Character: run.Character{
			ClassFile: classFile, Level: level, SpecOverride: spec,
			Stats: run.Stats{
				SpellDamage:       numMap(st.Tab("spellDamage")),
				SpellCrit:         numMap(st.Tab("spellCrit")),
				SpellHit:          num(st, "spellHit"),
				Healing:           num(st, "healing"),
				Strength:          num(st, "strength"),
				Agility:           agility,
				Intellect:         intellect, Spirit: num(st, "spirit"),
				AttackPower:       num(st, "attackPower"),
				RangedAttackPower: num(st, "rangedAttackPower"),
				MeleeCrit:         num(st, "meleeCrit"),
				MeleeHit:          num(st, "meleeHit"),
				MeleeHastePct:     num(st, "meleeHaste"),
				RangedHastePct:    num(st, "rangedHaste"),
				CritPerAgi:        critPerAgi,
				MainHand:          mh,
				OffHand:           oh,
				Ranged:            rangedWep,
				PetDPS:            petDPS,
				MP5:               gearMp5, HP5: gearHp5,
				HealthMax:         num(st, "healthMax"), ManaMax: num(st, "powerMax"),
				CritPerInt:        critPerInt,
				ManaPerInt:        orNum(st, "manaPerIntellect", 15),
				HPPerSta:          orNum(st, "hpPerStamina", 10),
				RangedCrit:        num(st, "rangedCrit"),
				Wand:              engine.Wand{DPS: wandDps, Speed: wandSpeed, Skill: wandSkill},
				LifeTap:           lifeTap,
				HastePct:          sheet.HastePct, SpiritRegen: sheet.SpiritRegen, CastingRegen: sheet.CastingRegen,
			},
			Talents: talents, Spellbook: spellbook, SetBonusSpells: setBonusSpells,
		},
		Fight: fight, Metric: metric, Method: opt.Method, Iterations: opt.Iterations,
	}
	if len(opt.Buffs) > 0 {
		in.Buffs = opt.Buffs
	}
	return &Engine{Input: in, Fight: fight}, nil
}

var wandSpeedRe = regexp.MustCompile(`(?:Speed|Tempo)\s+([0-9]+[.,][0-9]+)`)

func parseWandSpeed(wand *sv.Table) float64 {
	if wand == nil {
		return 0
	}
	for _, line := range wand.Tab("tooltip").List() {
		s, ok := line.(string)
		if !ok {
			continue
		}
		if m := wandSpeedRe.FindStringSubmatch(s); m != nil {
			if f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64); err == nil && f > 0 {
				return f
			}
		}
	}
	return 0
}

func weaponFromSlot(c *sv.Table, slotID int64) engine.Weapon {
	for _, e := range c.Tab("equipment").Tables() {
		if slot, _ := e.Num("slot"); int64(slot) == slotID {
			dps, _ := e.Tab("stats").Num("ITEM_MOD_DAMAGE_PER_SECOND_SHORT")
			spd := parseWandSpeed(e)
			if dps > 0 {
				if spd <= 0 {
					spd = 2.4
				}
				return engine.Weapon{DPS: dps, Speed: spd, Min: dps * spd * 0.85, Max: dps * spd * 1.15}
			}
		}
	}
	return engine.Weapon{}
}

func physicalWeapons(c, ch, st *sv.Table, classFile string) (mh, oh, ranged engine.Weapon, petDPS float64) {
	// Skip for pure cloth casters so their bundles and deltas remain untouched.
	if classFile == "WARLOCK" || classFile == "PRIEST" || classFile == "MAGE" {
		return mh, oh, ranged, 0
	}
	mh = weaponFromSlot(c, 16)
	if spd := num(st, "meleeSpeed"); spd > 0 {
		mh.Speed = spd
	}
	if dmg := st.Tab("meleeDamage"); dmg != nil && num(dmg, "max") > 0 {
		mh.Min, mh.Max = num(dmg, "min"), num(dmg, "max")
		if mh.Speed > 0 {
			mh.DPS = (mh.Min + mh.Max) / 2 / mh.Speed
		}
	}
	oh = weaponFromSlot(c, 17)
	if spd := num(st, "offhandSpeed"); spd > 0 {
		oh.Speed = spd
	}
	if dmg := st.Tab("offhandDamage"); dmg != nil && num(dmg, "max") > 0 {
		oh.Min, oh.Max = num(dmg, "min"), num(dmg, "max")
		if oh.Speed > 0 {
			oh.DPS = (oh.Min + oh.Max) / 2 / oh.Speed
		}
	}
	ranged = weaponFromSlot(c, 18)
	if spd := num(st, "rangedSpeed"); spd > 0 {
		ranged.Speed = spd
	}
	if dmg := st.Tab("rangedDamage"); dmg != nil && num(dmg, "max") > 0 {
		ranged.Min, ranged.Max = num(dmg, "min"), num(dmg, "max")
		if ranged.Speed > 0 {
			ranged.DPS = (ranged.Min + ranged.Max) / 2 / ranged.Speed
		}
	}
	if pet := ch.Tab("pet"); pet != nil {
		spd := num(pet, "speed")
		dmg := pet.Tab("damage")
		if spd > 0 && dmg != nil && num(dmg, "max") > 0 {
			petDPS = (num(dmg, "min") + num(dmg, "max")) / 2 / spd
		}
	}
	return mh, oh, ranged, petDPS
}
