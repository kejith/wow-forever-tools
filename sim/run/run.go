// Package run is the engine's JSON contract: one input bundle from the
// Python side in, one result out. The Python side keeps everything that
// needs the SavedVariables or localized tooltips (candidate filtering, slot
// logic, stat canonicalization); the engine evaluates deltas.
package run

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"

	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/classes/druid"
	"github.com/kejith/wow-tools-forever/sim/classes/hunter"
	"github.com/kejith/wow-tools-forever/sim/classes/mage"
	"github.com/kejith/wow-tools-forever/sim/classes/paladin"
	"github.com/kejith/wow-tools-forever/sim/classes/priest"
	"github.com/kejith/wow-tools-forever/sim/classes/rogue"
	"github.com/kejith/wow-tools-forever/sim/classes/shaman"
	"github.com/kejith/wow-tools-forever/sim/classes/warlock"
	"github.com/kejith/wow-tools-forever/sim/classes/warrior"
	"github.com/kejith/wow-tools-forever/sim/engine"
	"github.com/kejith/wow-tools-forever/sim/talents"
)

// Version of the engine contract; printed by --version and in the output.
const Version = "0.11.0"

// Input is the bundle the Python side sends. Stats are the character
// sheet's totals (gear included) from the WoW Tools Forever snapshot; deltas are
// canonical stat changes relative to that (see engine.ApplyDelta).
type Input struct {
	CacheDir    string       `json:"cacheDir"`
	Build       string       `json:"build"` // full version, e.g. "1.60.1.70959"
	Locale      string       `json:"locale"`
	Character   Character    `json:"character"`
	Evaluations []Evaluation `json:"evaluations"`
	Fight       engine.Fight `json:"fight"`
	Metric      string       `json:"metric"`
	Method      string       `json:"method"` // "expectation" (default) or "montecarlo"
	Iterations  int          `json:"iterations"`
	Seed        int64        `json:"seed"`  // Monte-Carlo: 0 means 1; every evaluation shares it (common random numbers)
	MCTop       int          `json:"mcTop"` // Monte-Carlo: refine this many evaluations with the biggest gains (default 16)
	// Situations asks for the rotation guide: the best rotation in several
	// kinds of fights (see situations.go).
	Situations bool `json:"situations"`
	// HealTable asks for every known heal and shield rank with its numbers
	// per cast (see healtable.go).
	HealTable bool `json:"healTable"`
	// DamageTable asks for every known rank of every damage spell with its
	// numbers (see damagetable.go).
	DamageTable bool `json:"damageTable"`
	// Buffs are canonical stat deltas (see engine.ApplyDelta) that every
	// number of the answer is to include: the consumables the character
	// would use. The per-rank spell numbers stay without them, since the
	// caller compares those with the tooltips of the snapshot.
	Buffs map[string]float64 `json:"buffs"`
	// Reoptimize picks the best rotation again for every evaluation instead
	// of keeping the baseline's. Gear swaps are small and keep it; a set of
	// consumables changes the mana budget enough for another heal rank or
	// filler to win.
	Reoptimize bool `json:"reoptimize"`
}

// Character is the snapshot-derived character.
type Character struct {
	ClassFile      string         `json:"classFile"`
	Level          int            `json:"level"`
	SpecOverride   string         `json:"specOverride"`
	Stats          Stats          `json:"stats"`
	Talents        map[string]int `json:"talents"`                  // rank-spell id -> points
	Spellbook      []int          `json:"spellbook"`                // known spell ids; empty = assume all trainer ranks
	SetBonusSpells []int          `json:"setBonusSpells,omitempty"` // active item set bonus spell ids on equipped gear
}

// Stats are the sheet totals the engine starts from.
type Stats struct {
	SpellDamage map[string]float64 `json:"spellDamage"` // school name -> bonus damage
	SpellCrit   map[string]float64 `json:"spellCrit"`   // school name -> percent
	SpellHit    float64            `json:"spellHit"`
	Healing     float64            `json:"healing"`
	Intellect   float64            `json:"intellect"`
	Spirit      float64            `json:"spirit"`
	MP5         float64            `json:"mp5"`
	HP5         float64            `json:"hp5"`
	HealthMax   float64            `json:"healthMax"`
	ManaMax     float64            `json:"manaMax"`
	CritPerInt  float64            `json:"critPerInt"`
	ManaPerInt  float64            `json:"manaPerInt"`
	HPPerSta    float64            `json:"hpPerSta"`
	HastePct    float64            `json:"hastePct"`
	RangedCrit  float64            `json:"rangedCrit"`
	// What the character sheet says about the mana regeneration, per second
	// (addon 1.12, GetManaRegenFromSpirit and GetManaRegen): from spirit
	// outside the five-second rule, and everything while casting. They
	// replace the class formula and the gear's mp5: the sheet has buffs too.
	SpiritRegen  float64        `json:"spiritRegen"`
	CastingRegen *float64       `json:"castingRegen"`
	Wand              engine.Wand    `json:"wand"`
	LifeTap           engine.LifeTap `json:"lifeTap"`
	Strength          float64        `json:"strength,omitempty"`
	Agility           float64        `json:"agility,omitempty"`
	AttackPower       float64        `json:"attackPower,omitempty"`
	RangedAttackPower float64        `json:"rangedAttackPower,omitempty"`
	FeralAP           float64        `json:"feralAp,omitempty"`
	MeleeCrit         float64        `json:"meleeCrit,omitempty"`
	MeleeHit          float64        `json:"meleeHit,omitempty"`
	MeleeHastePct     float64        `json:"meleeHastePct,omitempty"`
	RangedHastePct    float64        `json:"rangedHastePct,omitempty"`
	CritPerAgi        float64        `json:"critPerAgi,omitempty"`
	MainHand          engine.Weapon  `json:"mainHand,omitempty"`
	OffHand           engine.Weapon  `json:"offHand,omitempty"`
	Ranged            engine.Weapon  `json:"ranged,omitempty"`
	PetDPS            float64        `json:"petDps,omitempty"`
}

// Evaluation is one stat delta to rate (an item swap, or a package prefix).
type Evaluation struct {
	Key            string             `json:"key"`
	Delta          map[string]float64 `json:"delta"`
	NoMC           bool               `json:"noMc,omitempty"`           // skip Monte-Carlo refinement (helper evaluations like rm:/pt:)
	HasSetChange   bool               `json:"hasSetChange,omitempty"`   // true when active non-sheet set bonus spells differ from baseline
	SetBonusSpells []int              `json:"setBonusSpells,omitempty"` // replacement active set bonus spell ids when HasSetChange is true
}

// Result is the metric change of one evaluation. MC marks gains that were
// refined by the Monte-Carlo runs (the rest keep the closed-form delta).
type Result struct {
	Value   float64 `json:"value"`
	Gain    float64 `json:"gain"`
	GainPct float64 `json:"gainPct"`
	MC      bool    `json:"mc,omitempty"`
}

// HealOption is one heal-rank candidate of the healer rotation: what one
// cast gives, what it costs, and what the whole rotation sustains with it
// under the mana budget (the number the rank choice uses).
type HealOption struct {
	SpellID       int     `json:"spellId"`
	Name          string  `json:"name"`
	Rank          int     `json:"rank"`
	HealPerCast   float64 `json:"healPerCast"`
	ManaPerCast   float64 `json:"manaPerCast"`
	CastSec       float64 `json:"castSec"`
	HealPerMana   float64 `json:"healPerMana"`
	HPSThroughput float64 `json:"hpsThroughput"` // heal per cast / cast time, mana ignored
	HPSSustained  float64 `json:"hpsSustained"`  // the rotation's HPS under the mana budget
	// BonusLossPct: the rank is outgrown and heals with this much less of
	// the healing bonus (percent).
	BonusLossPct float64 `json:"bonusLossPct,omitempty"`
	Chosen       bool    `json:"chosen,omitempty"`
}

// Downrank says which spell rank the rotation should cast when a lower (or
// cheaper) rank beats the biggest heal under the mana budget.
type Downrank struct {
	SpellID     int     `json:"spellId"`
	Name        string  `json:"name"`
	Rank        int     `json:"rank"`
	InsteadID   int     `json:"insteadId"`
	InsteadName string  `json:"insteadName"`
	InsteadRank int     `json:"insteadRank"`
	GainPct     float64 `json:"gainPct"` // how much more the metric sustains vs the naive pick
}

// CoverageEntry says what the model does with one known spell or talent.
type CoverageEntry struct {
	SpellID    int     `json:"spellId"`
	Name       string  `json:"name"`
	Rank       int     `json:"rank,omitempty"` // the spell's rank, or the points in the talent
	Status     string  `json:"status"`         // used, sheet, idle, unmodeled, none (talents.Status*)
	Reason     string  `json:"reason"`         // a short code the app turns into a sentence
	Unverified bool    `json:"unverified,omitempty"`
	Note       string  `json:"note,omitempty"`  // the rule's source note (English)
	Value      float64 `json:"value,omitempty"` // idle spells: the metric of the best rotation with it
}

// Coverage lists every known spell and every talent with what the model does
// with it, so that nothing is left out silently.
type Coverage struct {
	Spells     []CoverageEntry `json:"spells"`
	Talents    []CoverageEntry `json:"talents"`
	SetBonuses []CoverageEntry `json:"setBonuses,omitempty"`
	// TalentValues is "tables" when the per-rank values came from the trait
	// tables, "basepoints" when only SpellEffect was in the cache (one rank's
	// value for every rank).
	TalentValues string `json:"talentValues"`
	Candidates   int    `json:"candidates"` // how many rotations were compared
}

// Output is the engine's answer. Errors are reported in the payload (exit
// code 0), the same convention as app_bridge.py.
type Output struct {
	Engine          string             `json:"engine"`
	Fight           *engine.Fight      `json:"fight,omitempty"` // as evaluated: defaults and automatic values resolved
	Baseline        *engine.Metrics    `json:"baseline,omitempty"`
	Spec            string             `json:"spec,omitempty"`
	SpellbookSource string             `json:"spellbookSource,omitempty"`
	Downrank        *Downrank          `json:"downrank,omitempty"`
	HealOptions     []HealOption       `json:"healOptions,omitempty"`
	Coverage        *Coverage          `json:"coverage,omitempty"`
	Situations      []Situation        `json:"situations,omitempty"`
	GroupRotations  []Situation        `json:"groupRotations,omitempty"`
	HealTable       *HealTable         `json:"healTable,omitempty"`
	DamageTable     *DamageTable       `json:"damageTable,omitempty"`
	Spells          []SpellNumbers     `json:"spells,omitempty"`
	Results         map[string]Result  `json:"results,omitempty"`
	Buffs           map[string]float64 `json:"buffs,omitempty"` // as applied (Input.Buffs)
	Error           string             `json:"error,omitempty"`
}

// SpellNumbers is what one cast of a known spell rank does in the model,
// without crits: the numbers the game's tooltip shows for the same spell,
// so the caller can lay the two side by side.
type SpellNumbers struct {
	SpellID   int     `json:"spellId"`
	BaseID    int     `json:"baseId"`
	Rank      int     `json:"rank,omitempty"`
	Level     int     `json:"level"` // level the rank is learned at
	DirectMin float64 `json:"directMin,omitempty"`
	DirectMax float64 `json:"directMax,omitempty"`
	Periodic  float64 `json:"periodic,omitempty"` // all ticks together
	Ticks     float64 `json:"ticks,omitempty"`
	Absorb    float64 `json:"absorb,omitempty"`
	Mana      float64 `json:"mana"`
	CastSec   float64 `json:"castSec"`
	// Channeled says that the ticks land while the spell is cast: CastSec
	// is the whole channel, and what ends it early loses the ticks left.
	Channeled bool `json:"channeled,omitempty"`
	Heal      bool `json:"heal,omitempty"`
	// BonusLossPct is the share of the healing bonus an outgrown heal rank
	// has lost, in percent (0 = the rank heals with the full bonus).
	BonusLossPct float64 `json:"bonusLossPct,omitempty"`
}

// bonusLossPct is how much of the healing bonus a heal rank has lost at
// this level, in percent.
func bonusLossPct(level int, sp *engine.Spell) float64 {
	return math.Round((1-engine.RankBonusShare(level, sp.Levels))*1000) / 10
}

// spellNumbers lists every known rank of every spell with an amount.
func spellNumbers(cat *catalog.Catalog, known map[int]bool, st *engine.State, tgt engine.Target) []SpellNumbers {
	var out []SpellNumbers
	for id := range known {
		sp := cat.Spells[id]
		if sp == nil || sp.Passive || (sp.Direct == nil && sp.Periodic == nil && sp.Absorb == nil) {
			continue
		}
		sh := engine.Shape(sp, st, tgt)
		n := SpellNumbers{SpellID: id, BaseID: sp.BaseID, Rank: cat.RankNumber(id), Level: sp.Levels.Spell,
			Mana: sh.ManaCost, CastSec: sh.ExecuteSec, Channeled: sp.Channeled, Heal: sp.IsHeal, Periodic: sh.Ticks * sh.TickBase}
		if n.Periodic != 0 {
			n.Ticks = sh.Ticks
		}
		if sp.IsHeal {
			n.BonusLossPct = bonusLossPct(st.Level, sp)
		}
		if sp.Absorb != nil && sp.Direct == nil {
			n.Absorb = sh.DirectAvg
		} else {
			n.DirectMin = sh.DirectAvg * (1 - sh.DirectVariance/2)
			n.DirectMax = sh.DirectAvg * (1 + sh.DirectVariance/2)
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BaseID != out[j].BaseID {
			return out[i].BaseID < out[j].BaseID
		}
		return out[i].Level < out[j].Level
	})
	return out
}

// Metrics lists what each modeled class can optimize for; the first entry
// is the default for a damage spec (enginebundle.Metrics mirrors this list).
var Metrics = map[string][]string{
	"WARLOCK": {"dps"},
	"PRIEST":  {"hps", "ttoom", "dps"},
	"MAGE":    {"dps"},
	"DRUID":   {"hps", "ttoom", "dps"},
	"SHAMAN":  {"hps", "ttoom", "dps"},
	"PALADIN": {"hps", "ttoom", "dps"},
	"WARRIOR": {"dps"},
	"ROGUE":   {"dps"},
	"HUNTER":  {"dps"},
}

// regen is the mana regeneration while not casting, per second:
// base + spirit * factor, used as it is for a snapshot without the sheet's
// regeneration (Stats.SpiritRegen).
var regen = map[string][2]float64{
	"WARLOCK": {6.25, 0.125},
	"PRIEST":  {6.5, 0.125},
	"MAGE":    {6.25, 0.125},
	"DRUID":   {7.5, 0.10},
	"SHAMAN":  {7.5, 0.10},
	"PALADIN": {7.5, 0.10},
	"HUNTER":  {7.5, 0.10},
}

// Supported reports whether the engine models a class yet.
func Supported(classFile string) bool {
	_, ok := Metrics[classFile]
	return ok
}

// DefaultMetric is the metric a class and spec optimize for by default.
func DefaultMetric(classFile, spec string) string {
	switch classFile {
	case "PRIEST":
		if spec != "shadow" {
			return "hps"
		}
	case "DRUID", "SHAMAN":
		if spec == "restoration" {
			return "hps"
		}
	case "PALADIN":
		if spec == "holy" {
			return "hps"
		}
	}
	return "dps"
}

func metricAllowed(classFile, metric string) bool {
	for _, m := range Metrics[classFile] {
		if m == metric {
			return true
		}
	}
	return false
}

// rotations returns the rotation candidates of a class for a spec and metric.
func rotations(classFile, spec, metric string, cat *catalog.Catalog, known map[int]bool, level int) [][]engine.Action {
	switch classFile {
	case "WARLOCK":
		return warlock.Rotations(cat, known, level)
	case "PRIEST":
		return priest.Rotations(metric, cat, known, level)
	case "MAGE":
		return mage.Rotations(cat, known, level)
	case "DRUID":
		return druid.RotationsForSpec(spec, metric, cat, known, level)
	case "SHAMAN":
		return shaman.RotationsForSpec(spec, metric, cat, known, level)
	case "PALADIN":
		return paladin.RotationsForSpec(spec, metric, cat, known, level)
	case "WARRIOR":
		return warrior.Rotations(cat, known, level)
	case "ROGUE":
		return rogue.Rotations(cat, known, level)
	case "HUNTER":
		return hunter.Rotations(cat, known, level)
	}
	return nil
}

func prepareClass(classFile string, cat *catalog.Catalog) {
	switch classFile {
	case "WARLOCK":
		warlock.Prepare(cat)
	case "PRIEST":
		priest.Prepare(cat)
	case "MAGE":
		mage.Prepare(cat)
	case "DRUID":
		druid.Prepare(cat)
	case "SHAMAN":
		shaman.Prepare(cat)
	case "PALADIN":
		paladin.Prepare(cat)
	case "WARRIOR":
		warrior.Prepare(cat)
	case "ROGUE":
		rogue.Prepare(cat)
	case "HUNTER":
		hunter.Prepare(cat)
	}
}

func classHealSources(classFile string, cat *catalog.Catalog, known map[int]bool, level, targets int) ([]engine.HealSource, *engine.Action) {
	switch classFile {
	case "PRIEST":
		return priest.HealSources(cat, known, level, targets)
	case "DRUID":
		return druid.HealSources(cat, known, level, targets)
	case "SHAMAN":
		return shaman.HealSources(cat, known, level, targets)
	case "PALADIN":
		return paladin.HealSources(cat, known, level, targets)
	}
	return nil, nil
}

func classAreaSpells(classFile string, cat *catalog.Catalog, known map[int]bool, level int) []*engine.Spell {
	switch classFile {
	case "WARLOCK":
		return warlock.AreaSpells(cat, known, level)
	case "PRIEST":
		return priest.AreaSpells(cat, known, level)
	case "MAGE":
		return mage.AreaSpells(cat, known, level)
	case "DRUID":
		return druid.AreaSpells(cat, known, level)
	case "PALADIN":
		return paladin.AreaSpells(cat, known, level)
	case "WARRIOR":
		return warrior.AreaSpells(cat, known, level)
	case "HUNTER":
		return hunter.AreaSpells(cat, known, level)
	}
	return nil
}

// Session holds the prepared character, catalog, rotations and baseline of
// one engine run so callers making several batches in-process (upgrades,
// consumables) avoid rebuilding the catalog, re-solving all rotations, and
// re-running baseline Monte-Carlo on every batch.
type Session struct {
	in          Input
	cat         *catalog.Catalog
	st          *engine.State
	talentMods  engine.Mods // talent + class self-buff mods without item set bonuses
	fight       engine.Fight
	metric      string
	scoreMetric string
	rots        [][]engine.Action
	actions     []engine.Action
	baseline    engine.Metrics
	mcBase      *engine.MCStats
	baseOut     Output
}

// Prepare builds the catalog, decodes talents, picks the best baseline
// rotation and (when Method == "montecarlo") runs baseline Monte-Carlo once.
func Prepare(in Input) (*Session, Output) {
	out := Output{Engine: Version}
	if !Supported(in.Character.ClassFile) {
		out.Error = fmt.Sprintf("class %s is not modeled yet", in.Character.ClassFile)
		return nil, out
	}
	if in.Method != "" && in.Method != "expectation" && in.Method != "montecarlo" {
		out.Error = fmt.Sprintf("unknown method %q", in.Method)
		return nil, out
	}

	cat, err := catalog.Build(in.CacheDir, in.Build, in.Locale, in.Character.ClassFile)
	if err != nil {
		out.Error = err.Error()
		return nil, out
	}

	prepareClass(in.Character.ClassFile, cat)

	ranks := map[int]int{}
	for id, rank := range in.Character.Talents {
		var n int
		if _, err := fmt.Sscanf(id, "%d", &n); err == nil {
			ranks[n] = rank
		}
	}
	talentMods, talentInfos, err := talents.Decode(cat, ranks)
	if err != nil {
		out.Error = err.Error()
		return nil, out
	}
	mods := talentMods.Clone()
	setInfos := talents.DecodeSetBonuses(cat, in.Character.SetBonusSpells, &mods)

	st := buildState(in.Character, mods)

	known := map[int]bool{}
	if len(in.Character.Spellbook) > 0 {
		for _, id := range in.Character.Spellbook {
			known[id] = true
		}
		for id := range ranks { // talent casts are known even if a scan missed them
			if cat.Spells[id] != nil {
				known[id] = true
			}
		}
		out.SpellbookSource = "export"
	} else {
		known = cat.AssumedSpellbook(in.Character.Level, ranks)
		out.SpellbookSource = "assumed"
	}

	metric := in.Metric
	if metric == "" {
		metric = DefaultMetric(in.Character.ClassFile, in.Character.SpecOverride)
	}
	if !metricAllowed(in.Character.ClassFile, metric) {
		out.Error = fmt.Sprintf("metric %s is not modeled for %s", metric, in.Character.ClassFile)
		return nil, out
	}
	out.Spec = in.Character.SpecOverride

	fight := in.Fight
	if fight.Seconds <= 0 {
		fight.Seconds = 60
	}
	standard := fight // as asked, with automatic kills still automatic
	fight = resolveKills(fight, metric)
	out.Fight = &fight
	out.Spells = spellNumbers(cat, known, st, engine.NewTarget(in.Character.Level, fight.TargetDelta))
	if len(in.Buffs) > 0 {
		st = st.ApplyDelta(in.Buffs)
		out.Buffs = in.Buffs
	}

	// Self buffs the model takes as active: Warlock Demon Armor HP5, Mage Armor casting regen.
	support := map[int]string{} // spell id -> coverage reason
	switch in.Character.ClassFile {
	case "WARLOCK":
		hp5, armor := warlock.ArmorRegen(cat, known, in.Character.Level)
		st.HP5 += hp5
		if armor != nil && hp5 > 0 {
			support[armor.ID] = "buff"
		}
		if tap := cat.BestRank(warlock.LifeTap, known, in.Character.Level); tap != nil && st.LifeTap.ManaPerCast > 0 {
			support[tap.ID] = "regen"
		}
	case "MAGE":
		regenPct, armor := mage.ArmorRegen(cat, known, in.Character.Level)
		if armor != nil && regenPct > 0 {
			st.Mods.CastingRegenPct += regenPct
			talentMods.CastingRegenPct += regenPct
			support[armor.ID] = "buff"
		}
	case "ROGUE":
		hastePct, snd := rogue.SliceAndDiceHaste(cat, known, in.Character.Level)
		if snd != nil && hastePct > 0 {
			st.MeleeHastePct += hastePct
			support[snd.ID] = "buff"
		}
	}

	if in.HealTable {
		lockouts := map[int]float64{}
		sources, _ := classHealSources(in.Character.ClassFile, cat, known, in.Character.Level, 1)
		for _, src := range sources {
			if src.Action.CycleSec > 0 {
				lockouts[src.Action.Spell.ID] = src.Action.CycleSec
			}
		}
		if t := healTable(cat, known, st, fight, lockouts); len(t.Rows) > 0 {
			out.HealTable = t
		}
	}
	if in.DamageTable {
		if t := damageTable(cat, known, st, fight); len(t.Rows) > 0 {
			out.DamageTable = t
		}
	}
	if in.Situations && metric == "dps" {
		var tap *engine.Spell
		if in.Character.ClassFile == "WARLOCK" {
			tap = cat.BestRank(warlock.LifeTap, known, in.Character.Level)
		}
		out.Situations = damageSituations(in.Character.ClassFile, cat, known, st, standard, tap)
		out.GroupRotations = groupDamageSituations(in.Character.ClassFile, cat, known, st, standard, tap, out.Situations)
	} else if in.Situations {
		out.Situations = healSituations(in.Character.ClassFile, cat, known, st, standard)
	}

	rots := rotations(in.Character.ClassFile, in.Character.SpecOverride, metric, cat, known, in.Character.Level)
	if len(rots) == 0 {
		out.Error = "no castable rotation: the spellbook has none of the modeled spells"
		return nil, out
	}
	scoreMetric := metric
	if metric != "dps" {
		scoreMetric = "hps"
	}
	type scored struct {
		actions []engine.Action
		filler  *engine.Spell
		variant string // what the rotation casts besides the filler
		score   float64
	}
	all := make([]scored, len(rots))
	for i, r := range rots {
		m := engine.Evaluate(st, fight, r, scoreMetric)
		var filler *engine.Spell
		variant := ""
		for _, a := range r {
			switch {
			case a.Kind == "heal" || a.Kind == "filler":
				filler = a.Spell
			case a.Spell != nil && a.Kind != "proc_cast":
				variant += fmt.Sprintf("%s%d,", a.Kind, a.Spell.ID)
			}
		}
		all[i] = scored{r, filler, variant, m.Value}
	}
	best := 0
	for i, s := range all {
		if s.score > all[best].score {
			best = i
		}
	}
	actions := all[best].actions
	if len(rots) > 1 && scoreMetric == "hps" {
		chosen := all[best].filler
		same := all[best].variant
		tgt := engine.NewTarget(in.Character.Level, fight.TargetDelta)
		for i, s := range all {
			if s.filler == nil || s.variant != same {
				continue
			}
			sh := engine.Shape(s.filler, st, tgt)
			o := HealOption{
				SpellID: s.filler.ID, Name: s.filler.Name, Rank: cat.RankNumber(s.filler.ID),
				HealPerCast: sh.SampleDirect(nil), ManaPerCast: sh.ManaCost, CastSec: sh.ExecuteSec,
				HPSSustained: s.score, Chosen: i == best,
				BonusLossPct: bonusLossPct(st.Level, s.filler),
			}
			if o.ManaPerCast > 0 {
				o.HealPerMana = o.HealPerCast / o.ManaPerCast
			}
			if o.CastSec > 0 {
				o.HPSThroughput = o.HealPerCast / o.CastSec
			}
			out.HealOptions = append(out.HealOptions, o)
		}
		sort.Slice(out.HealOptions, func(i, j int) bool {
			return out.HealOptions[i].HPSSustained > out.HealOptions[j].HPSSustained
		})

		naive := -1
		if chosen != nil {
			bestSame, bestOther := -1, -1
			for i, s := range all {
				if s.filler == nil || s.filler.ID == chosen.ID || s.variant != same {
					continue
				}
				if cat.BaseOf[s.filler.ID] == cat.BaseOf[chosen.ID] {
					if bestSame < 0 || cat.RankNumber(s.filler.ID) > cat.RankNumber(all[bestSame].filler.ID) {
						bestSame = i
					}
				} else if bestOther < 0 || s.filler.Levels.Base > all[bestOther].filler.Levels.Base {
					bestOther = i
				}
			}
			if bestSame >= 0 && cat.RankNumber(all[bestSame].filler.ID) > cat.RankNumber(chosen.ID) {
				naive = bestSame
			} else if bestOther >= 0 && all[bestOther].filler.Levels.Base > chosen.Levels.Base {
				naive = bestOther
			}
		}
		if naive >= 0 && all[naive].score > 0 && all[best].score > all[naive].score {
			n := all[naive].filler
			out.Downrank = &Downrank{
				SpellID: chosen.ID, Name: chosen.Name, Rank: cat.RankNumber(chosen.ID),
				InsteadID: n.ID, InsteadName: n.Name, InsteadRank: cat.RankNumber(n.ID),
				GainPct: (all[best].score - all[naive].score) / all[naive].score * 100,
			}
		}
	}

	// Coverage: what became of every known spell and every talent.
	tried := map[int]float64{} // spell id -> the best score of a candidate that casts it
	for _, s := range all {
		for _, a := range s.actions {
			if a.Spell == nil || a.Kind == "mana" || a.Kind == "proc_cast" {
				continue
			}
			if old, ok := tried[a.Spell.ID]; !ok || s.score > old {
				tried[a.Spell.ID] = s.score
			}
		}
	}

	baseline := engine.Evaluate(st, fight, actions, metric)
	// A spell that trades health for mana (Dark Sacrifice) is cast on demand:
	// used when the baseline needed it, else idle with the reason.
	unused := map[int]string{} // spell id -> why the rotation does not cast it
	for _, a := range actions {
		if a.Kind != "mana" || a.Spell == nil {
			continue
		}
		cast := false
		for _, row := range baseline.Breakdown {
			cast = cast || (row.Kind == "mana" && row.SpellID == a.Spell.ID)
		}
		switch {
		case cast:
			support[a.Spell.ID] = "regen"
		case baseline.ManaSpentPS <= baseline.ManaBudgetPS:
			unused[a.Spell.ID] = "notneeded"
		default:
			unused[a.Spell.ID] = "notworth"
		}
	}
	for _, row := range baseline.Breakdown {
		if row.Kind == "proc_cast" && row.SpellID != 0 {
			support[row.SpellID] = "rotation"
		}
	}
	for _, sp := range classAreaSpells(in.Character.ClassFile, cat, known, in.Character.Level) {
		if _, ok := tried[sp.ID]; !ok {
			unused[sp.ID] = "area"
		}
	}
	out.Coverage = coverage(cat, known, in.Character.Level, metric, actions, tried, support, unused, ranks, talentInfos,
		setInfos, baseline.KillBuffPct)
	out.Coverage.Candidates = len(all)

	for i := range baseline.Breakdown { // rank numbers for the labels ("Heal (Rank 2)")
		row := &baseline.Breakdown[i]
		if row.Kind == "lifetap" { // the spell's own name and rank where the book has it
			for id, reason := range support {
				if reason == "regen" {
					row.SpellID, row.Label = id, cat.Spells[id].Name
				}
			}
		}
		if row.SpellID != 0 {
			row.Rank = cat.RankNumber(row.SpellID)
		}
	}
	sess := &Session{
		in: in, cat: cat, st: st, talentMods: talentMods, fight: fight, metric: metric, scoreMetric: scoreMetric,
		rots: rots, actions: actions, baseline: baseline, baseOut: out,
	}
	if in.Method == "montecarlo" {
		seed := in.Seed
		if seed == 0 {
			seed = 1
		}
		mc := engine.RunMC(st, fight, actions, metric, in.Iterations, seed)
		sess.mcBase = &mc
		sess.baseline.MC = &mc
		sess.baseline.Value = mc.Mean
	}
	b := sess.baseline
	sess.baseOut.Baseline = &b
	return sess, sess.baseOut
}

// evalSignature returns a canonical string key for an evaluation (stat delta +
// any active set bonus spell changes) so identical evaluations in one batch are
// evaluated only once.
func evalSignature(d map[string]float64, hasSetChange bool, setSpells []int) string {
	var b []byte
	if len(d) > 0 {
		keys := make([]string, 0, len(d))
		for k, v := range d {
			if v != 0 {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				b = append(b, ';')
			}
			b = append(b, k...)
			b = append(b, '=')
			b = fmt.Appendf(b, "%.12g", d[k])
		}
	}
	if hasSetChange {
		b = append(b, '|')
		if len(setSpells) > 0 {
			ids := append([]int(nil), setSpells...)
			sort.Ints(ids)
			for i, id := range ids {
				if i > 0 {
					b = append(b, ',')
				}
				b = fmt.Appendf(b, "%d", id)
			}
		}
	}
	return string(b)
}

func (s *Session) stateFor(delta map[string]float64, hasSetChange bool, setSpells []int) *engine.State {
	st := s.st
	if hasSetChange {
		cp := *st
		m := s.talentMods.Clone()
		talents.DecodeSetBonuses(s.cat, setSpells, &m)
		cp.Mods = m
		st = &cp
	}
	if len(delta) > 0 {
		st = st.ApplyDelta(delta)
	}
	return st
}

// Eval evaluates one batch of deltas against the prepared session. Identical
// deltas are deduplicated; Monte-Carlo refinement skips NoMC evaluations.
func (s *Session) Eval(evals []Evaluation, reoptimize bool) Output {
	out := s.baseOut
	b := s.baseline
	out.Baseline = &b
	out.Results = make(map[string]Result, len(evals))
	if len(evals) == 0 {
		return out
	}

	// Deduplicate identical stat deltas + set bonus changes so items with
	// identical relevant stats are evaluated once.
	type uniqueEval struct {
		delta        map[string]float64
		hasSetChange bool
		setSpells    []int
		res          Result
		best         []engine.Action
	}
	sigIdx := make(map[string]int, len(evals))
	uniques := make([]uniqueEval, 0, len(evals))
	evToUniq := make([]int, len(evals))
	for i, ev := range evals {
		sig := evalSignature(ev.Delta, ev.HasSetChange, ev.SetBonusSpells)
		if idx, ok := sigIdx[sig]; ok {
			evToUniq[i] = idx
			continue
		}
		idx := len(uniques)
		sigIdx[sig] = idx
		evToUniq[i] = idx
		uniques = append(uniques, uniqueEval{
			delta: ev.Delta, hasSetChange: ev.HasSetChange, setSpells: ev.SetBonusSpells,
		})
	}

	baseDetVal := s.baseline.Value
	if s.mcBase != nil {
		// Deterministic baseline for closed-form deltas before MC refinement.
		baseDetVal = engine.Evaluate(s.st, s.fight, s.actions, s.metric).Value
	}

	evalOne := func(i int) {
		u := &uniques[i]
		if len(u.delta) == 0 && !u.hasSetChange {
			u.res = Result{Value: baseDetVal}
			u.best = s.actions
			return
		}
		st := s.stateFor(u.delta, u.hasSetChange, u.setSpells)
		with := s.actions
		if reoptimize {
			top := math.Inf(-1)
			for _, r := range s.rots {
				if v := engine.Evaluate(st, s.fight, r, s.scoreMetric).Value; v > top {
					top, with = v, r
				}
			}
		}
		m := engine.Evaluate(st, s.fight, with, s.metric)
		r := Result{Value: m.Value, Gain: m.Value - baseDetVal}
		if baseDetVal != 0 {
			r.GainPct = r.Gain / baseDetVal * 100
		}
		u.res = r
		u.best = with
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > 1 && len(uniques) >= 8 && (reoptimize || len(uniques) >= 64) {
		if workers > len(uniques) {
			workers = len(uniques)
		}
		var wg sync.WaitGroup
		wg.Add(workers)
		chunk := (len(uniques) + workers - 1) / workers
		for w := 0; w < workers; w++ {
			lo := w * chunk
			hi := lo + chunk
			if hi > len(uniques) {
				hi = len(uniques)
			}
			if lo >= hi {
				wg.Done()
				continue
			}
			go func(lo, hi int) {
				defer wg.Done()
				for i := lo; i < hi; i++ {
					evalOne(i)
				}
			}(lo, hi)
		}
		wg.Wait()
	} else {
		for i := range uniques {
			evalOne(i)
		}
	}
	for i, ev := range evals {
		out.Results[ev.Key] = uniques[evToUniq[i]].res
	}

	// Monte-Carlo: refine only eligible (non-NoMC) evaluations with the
	// biggest closed-form gains, deduplicated by unique stat delta.
	if s.mcBase != nil {
		seed := s.in.Seed
		if seed == 0 {
			seed = 1
		}
		top := s.in.MCTop
		if top <= 0 {
			top = 16
		}
		// Collect eligible keys (excluding NoMC helpers such as rm:* and pt:*).
		type cand struct {
			key  string
			uniq int
			abs  float64
		}
		cands := make([]cand, 0, len(evals))
		for i, ev := range evals {
			if ev.NoMC {
				continue
			}
			u := evToUniq[i]
			cands = append(cands, cand{key: ev.Key, uniq: u, abs: math.Abs(uniques[u].res.Gain)})
		}
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].abs != cands[j].abs {
				return cands[i].abs > cands[j].abs
			}
			return cands[i].key < cands[j].key
		})
		mcCache := make(map[int]Result)
		for i := 0; i < len(cands) && i < top; i++ {
			c := cands[i]
			if r, ok := mcCache[c.uniq]; ok {
				out.Results[c.key] = r
				continue
			}
			u := &uniques[c.uniq]
			if len(u.delta) == 0 && !u.hasSetChange {
				r := Result{Value: s.mcBase.Mean, MC: true}
				mcCache[c.uniq] = r
				out.Results[c.key] = r
				continue
			}
			m := engine.RunMC(s.stateFor(u.delta, u.hasSetChange, u.setSpells), s.fight, u.best, s.metric, s.in.Iterations, seed)
			r := Result{Value: m.Mean, Gain: m.Mean - s.mcBase.Mean, MC: true}
			if s.mcBase.Mean != 0 {
				r.GainPct = r.Gain / s.mcBase.Mean * 100
			}
			mcCache[c.uniq] = r
			out.Results[c.key] = r
		}
	}
	return out
}

// Run evaluates one input bundle.
func Run(in Input) Output {
	sess, out := Prepare(in)
	if out.Error != "" {
		return out
	}
	return sess.Eval(in.Evaluations, in.Reoptimize)
}

func buildState(ch Character, mods engine.Mods) *engine.State {
	apPerStr, apPerAgi, rapPerAgi := 2.0, 0.0, 0.0
	switch ch.ClassFile {
	case "ROGUE":
		apPerStr, apPerAgi = 1.0, 1.0
	case "HUNTER":
		apPerStr, apPerAgi, rapPerAgi = 1.0, 1.0, 2.0
	case "DRUID":
		if ch.SpecOverride == "feral" {
			apPerStr, apPerAgi = 2.0, 1.0
		}
	}
	st := &engine.State{
		ClassFile:         ch.ClassFile,
		Spec:              ch.SpecOverride,
		Level:             ch.Level,
		Healing:           ch.Stats.Healing,
		Hit:               ch.Stats.SpellHit,
		Strength:          ch.Stats.Strength,
		Agility:           ch.Stats.Agility,
		Intellect:         ch.Stats.Intellect,
		Spirit:            ch.Stats.Spirit,
		AttackPower:       ch.Stats.AttackPower,
		RangedAttackPower: ch.Stats.RangedAttackPower,
		FeralAP:           ch.Stats.FeralAP,
		MeleeCrit:         ch.Stats.MeleeCrit,
		MeleeHit:          ch.Stats.MeleeHit,
		MeleeHastePct:     ch.Stats.MeleeHastePct,
		RangedHastePct:    ch.Stats.RangedHastePct,
		CritPerAgi:        ch.Stats.CritPerAgi,
		APPerStr:          apPerStr,
		APPerAgi:          apPerAgi,
		RAPPerAgi:         rapPerAgi,
		MP5:               ch.Stats.MP5,
		HP5:               ch.Stats.HP5,
		HealthMax:         ch.Stats.HealthMax,
		ManaMax:           ch.Stats.ManaMax,
		CritPerInt:        ch.Stats.CritPerInt,
		ManaPerInt:        ch.Stats.ManaPerInt,
		HPPerSta:          ch.Stats.HPPerSta,
		HastePct:          ch.Stats.HastePct,
		RangedCrit:        ch.Stats.RangedCrit,
		Wand:              ch.Stats.Wand,
		MainHand:          ch.Stats.MainHand,
		OffHand:           ch.Stats.OffHand,
		Ranged:            ch.Stats.Ranged,
		PetDPS:            ch.Stats.PetDPS,
		LifeTap:           ch.Stats.LifeTap,
		Mods:              mods,
	}
	if r, ok := regen[ch.ClassFile]; ok {
		st.RegenBasePS, st.RegenSpiritFactor = r[0], r[1]
	}
	// The sheet's own numbers win. The spirit regeneration is the sheet's at
	// the sheet's spirit (the formula keeps the slope for item deltas), and
	// the mana per 5 s is what the sheet regenerates while casting beyond the
	// share of the spirit regeneration the talents let through: gear and
	// buffs, so that the model's regeneration while casting is the sheet's.
	if ch.Stats.SpiritRegen > 0 {
		st.RegenBasePS = ch.Stats.SpiritRegen - st.Spirit*st.RegenSpiritFactor
		if c := ch.Stats.CastingRegen; c != nil {
			share := math.Min(100, mods.CastingRegenPct) / 100
			st.MP5 = math.Max(0, *c-ch.Stats.SpiritRegen*share) * 5
		}
	}
	for name, v := range ch.Stats.SpellDamage {
		if s := engine.SchoolByName(name); s >= 0 {
			st.SP[s] = v
		}
	}
	for name, v := range ch.Stats.SpellCrit {
		if s := engine.SchoolByName(name); s >= 0 {
			st.Crit[s] = v
		}
	}
	return st
}

// relevant reports whether a spell could move the metric at all: damage for
// dps, healing and shields for the healer metrics, mana for every metric.
func relevant(sp *engine.Spell, metric string) (bool, string) {
	switch {
	case sp.IsHeal:
		return metric != "dps", "heal"
	case sp.Direct != nil || sp.Periodic != nil:
		return metric == "dps", "damage"
	case sp.Absorb != nil:
		return metric != "dps", "absorb"
	case sp.GivesMana:
		return true, "mana"
	}
	return false, ""
}

// coverage classifies every known spell (one entry per rank chain) and every
// talent: cast in the chosen rotation, tried in a weaker candidate, not
// modeled, or without influence on the metric.
func coverage(cat *catalog.Catalog, known map[int]bool, level int, metric string, actions []engine.Action,
	tried map[int]float64, support, unused map[int]string, ranks map[int]int, infos, setInfos []talents.Info,
	killBuffPct float64) *Coverage {
	cov := &Coverage{TalentValues: "basepoints"}
	if cat.HasTraits {
		cov.TalentValues = "tables"
	}
	bestScore := 0.0
	for _, score := range tried {
		bestScore = math.Max(bestScore, score)
	}
	inRotation := map[int]bool{}
	var rotation []*engine.Spell
	for _, a := range actions {
		if a.Spell != nil && a.Kind != "mana" && a.Kind != "proc_cast" { // mana & proc_cast are conditional
			inRotation[a.Spell.ID] = true
			rotation = append(rotation, a.Spell)
		}
	}
	castable := func(sp *engine.Spell) bool {
		return sp != nil && !sp.Passive && (sp.Direct != nil || sp.Periodic != nil || sp.Absorb != nil)
	}

	// Spells, one entry per chain: the rank the rotation casts, else the
	// best rank a candidate tried, else the highest known rank.
	chainStatus := map[int]CoverageEntry{} // chain base -> its entry
	bases := make([]int, 0, len(cat.Chains))
	for base := range cat.Chains {
		bases = append(bases, base)
	}
	sort.Ints(bases)
	for _, base := range bases {
		knownRanks := cat.KnownRanks(base, known, level)
		if len(knownRanks) == 0 {
			continue
		}
		top := knownRanks[len(knownRanks)-1]
		if top.Passive || (ranks[top.ID] > 0 && !castable(top)) {
			continue // passives and passive talents are not spells one casts
		}
		entry := func(sp *engine.Spell, status, reason string) CoverageEntry {
			return CoverageEntry{SpellID: sp.ID, Name: sp.Name, Rank: cat.RankNumber(sp.ID), Status: status, Reason: reason}
		}
		var e CoverageEntry
		found := false
		for _, sp := range knownRanks { // cast in the rotation, or supporting it
			if inRotation[sp.ID] {
				e, found = entry(sp, talents.StatusUsed, "rotation"), true
			} else if reason, ok := support[sp.ID]; ok && !found {
				e, found = entry(sp, talents.StatusUsed, reason), true
			}
		}
		if !found { // modeled, and the rotation did not need it
			for _, sp := range knownRanks {
				if reason, ok := unused[sp.ID]; ok {
					e, found = entry(sp, talents.StatusIdle, reason), true
				}
			}
		}
		if !found { // tried in a candidate that lost
			for _, sp := range knownRanks {
				if score, ok := tried[sp.ID]; ok && (!found || score > e.Value) {
					e, found = entry(sp, talents.StatusIdle, "weaker"), true
					e.Value = score
				}
			}
			// A candidate that scores what the best one does only has the
			// spell standing by (Drain Life in a fight the health lasts for).
			if found && e.Value >= bestScore*(1-1e-9) {
				e.Reason = "nogain"
			}
		}
		if !found {
			if ok, what := relevant(top, metric); ok {
				e = entry(top, talents.StatusUnmodeled, what)
			} else {
				e = entry(top, talents.StatusNone, "other")
			}
		}
		chainStatus[base] = e
		cov.Spells = append(cov.Spells, e)
	}

	// Talents. A talent that is a spell one casts (Siphon Life, Wrack)
	// follows that spell; one that changes spells is judged by whether the
	// rotation casts any of them.
	matches := func(info talents.Info, sp *engine.Spell) bool {
		if info.Bases[sp.BaseID] {
			return true
		}
		for _, m := range info.Mods {
			if m.Op == engine.OpHit && sp.IsHeal {
				continue // heals never miss
			}
			if m.Matches(sp) {
				return true
			}
		}
		return false
	}
	classifyInfo := func(info talents.Info) CoverageEntry {
		name := cat.Name(info.SpellID)
		e := CoverageEntry{SpellID: info.SpellID, Name: name, Rank: info.Rank,
			Status: info.Status, Reason: info.Reason, Unverified: info.Unverified, Note: info.Note}
		if sp := cat.Spells[info.SpellID]; castable(sp) {
			if ce, ok := chainStatus[cat.BaseOf[sp.ID]]; ok {
				e.Status, e.Reason, e.Value = ce.Status, ce.Reason, ce.Value
			}
		} else if len(info.Mods) > 0 || len(info.Bases) > 0 {
			status, reason := talents.StatusNone, "nospell"
			for _, sp := range rotation {
				if matches(info, sp) {
					status, reason = talents.StatusUsed, "mods"
					break
				}
			}
			if status != talents.StatusUsed {
				for id := range tried {
					if matches(info, cat.Spells[id]) {
						status, reason = talents.StatusIdle, "idle"
						break
					}
				}
			}
			if status == talents.StatusNone {
				for id := range known {
					sp := cat.Spells[id]
					if sp == nil || sp.Passive || !matches(info, sp) {
						continue
					}
					if ok, _ := relevant(sp, metric); ok {
						status, reason = talents.StatusUnmodeled, "notcast"
						break
					}
					reason = "othermetric"
				}
			}
			if talents.Better(status, e.Status) {
				e.Status, e.Reason = status, reason
			}
		}
		if e.Reason == "wand" && metric != "dps" {
			e.Status, e.Reason = talents.StatusNone, "othermetric"
		}
		if e.Reason == "kills" { // a buff from kills counts for the kills the fight assumes
			if e.Value = killBuffPct; killBuffPct <= 0 {
				e.Status, e.Reason = talents.StatusNone, "nokills"
			}
		}
		if e.Status != talents.StatusUsed && e.Status != talents.StatusIdle {
			e.Unverified = false // an assumption only matters where it is applied
		}
		return e
	}
	for _, info := range infos {
		cov.Talents = append(cov.Talents, classifyInfo(info))
	}
	for _, info := range setInfos {
		cov.SetBonuses = append(cov.SetBonuses, classifyInfo(info))
	}
	return cov
}
