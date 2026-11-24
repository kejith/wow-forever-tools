// Package rotations is the Go side of scripts/rotations.py: the rotation guide, what to cast in which
// situation, from the combat engine. A damage dealer gets the best rotation for several kinds of fights,
// each played cast by cast; a healer gets a ladder of demand with the spells that supply it for the least
// mana. The sentences of a guide are shared with the heal table and the damage analysis.
package rotations

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/kejith/wow-tools-forever/sim/engine"
	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/run"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

// Units are the units of the metrics a guide has.
var Units = map[string]string{"dps": "DPS", "hps": "HPS"}

type guide struct{ title, about string }

var guides = map[string]guide{
	"dps": {i18n.Mark("Damage"),
		i18n.Mark("Every fight is played cast by cast with each rotation you could cast; this is the best one.")},
	"hps": {i18n.Mark("Healing"),
		i18n.Mark("How to heal a given amount per second for the least mana. The levels are shares of the most " +
			"you can heal.")},
}

// guideNotes are the heals the healer's plan leaves out, said once per guide.
var guideNotes = map[string][]string{
	"hps": {i18n.Mark("Heals that reach several targets at once (Prayer of Healing, Holy Nova) are not in the model " +
		"yet; the group level uses heals on one target at a time."),
		i18n.Mark("The casts are spread evenly: after each one the five-second rule holds the spirit " +
			"regeneration back, the rest of the pause regenerates in full.")},
}

var (
	sameDemand = i18n.Mark("One target needs {value:.0f} healing per second: {pct:.0f}% of the most you can heal.")
	several    = i18n.Mark("{targets} enemies at once that live for {seconds:.0f} seconds; the number is the damage on all of " +
		"them together.")
)

var situations = map[string]guide{
	"quick": {i18n.Mark("Quick kill"), i18n.Mark("An enemy that dies in {seconds:.0f} seconds.")},
	"standard": {i18n.Mark("Normal fight"),
		i18n.Mark("{seconds:.0f} seconds against an enemy {delta} levels above you: the fight your gear is " +
			"rated for.")},
	"max": {i18n.Mark("Maximum damage"),
		i18n.Mark("The normal fight as if your mana never ran out: the most damage you can do in {seconds:.0f} " +
			"seconds, whatever it costs.")},
	"boss":      {i18n.Mark("Boss"), i18n.Mark("{minutes:.0f} minutes against an enemy {delta} levels above you.")},
	"endurance": {i18n.Mark("Sustained damage"), i18n.Mark("{minutes:.0f} minutes of fighting without a break.")},
	"pack":      {i18n.Mark("Several enemies"), several},
	"crowd":     {i18n.Mark("Many enemies"), several},
	"light":     {i18n.Mark("Light damage"), sameDemand},
	"steady":    {i18n.Mark("Tank healing"), sameDemand},
	"heavy":     {i18n.Mark("Heavy damage"), sameDemand},
	"burst": {i18n.Mark("Everything you have"),
		i18n.Mark("The most you can heal on one target: {value:.0f} healing per second, whatever it costs.")},
	"group": {i18n.Mark("Group damage"),
		i18n.Mark("{targets} targets need {value:.0f} healing per second together: {pct:.0f}% of the most you " +
			"can heal there.")},
}

// rules say what a line of the priority list means, per kind of step.
var rules = map[string]string{
	"debuff":   i18n.Mark("cast it first and keep it on the enemy: your spells deal {pct:g}% more under it"),
	"dot":      i18n.Mark("keep it on the enemy"),
	"cooldown":  i18n.Mark("whenever it is ready"),
	"proc_cast": i18n.Mark("whenever Nightfall makes it instant"),
	"filler":    i18n.Mark("fills the rest of the time"),
	"recover":  i18n.Mark("when the health is too low to trade for mana and the mana is running out (first after {time})"),
	"lifetap":  i18n.Mark("as soon as the health bar is full and the mana has room for it, and when the mana is gone"),
	"mana":     i18n.Mark("when mana is missing and the health allows it"),
	"wand":     i18n.Mark("when the mana is gone (first after {time})"),
	"burn": i18n.Mark("once the spells above are up, while your health is above {floor:.0f}%: it costs you {health:.0f} " +
		"health per second"),
}

// rulesSeveral are the same lines in a fight against several enemies.
var rulesSeveral = map[string]string{
	"debuff":   i18n.Mark("cast it on every enemy first: your spells deal {pct:g}% more under it"),
	"dot":      i18n.Mark("keep it on every enemy"),
	"cooldown":  i18n.Mark("whenever it is ready, on one enemy"),
	"proc_cast": i18n.Mark("whenever Nightfall makes it instant, on one enemy"),
	"filler":    i18n.Mark("fills the rest of the time, on one enemy"),
	"burn": i18n.Mark("once the spells above are up, while your health is above {floor:.0f}%: it hits every enemy and " +
		"costs you {health:.0f} health per second"),
}

var (
	areaFiller = i18n.Mark("fills the rest of the time and hits every enemy at once")
	// A spell with a cooldown as long as it lasts cannot be on every enemy.
	keptOnOne  = i18n.Mark("keep it on one enemy: its cooldown allows no more")
	keptOnSome = i18n.Mark("keep it on {n:.0f} of the {targets} enemies: its cooldown allows no more")
	// Life Tap next to a spell that stands by to return health: no reason to wait for a full health bar.
	tapWithStandby = i18n.Mark("as soon as the health allows it and the mana has room for it, and when the mana is gone")
	// A rotation without a filler spell: the wand takes its place.
	wandFills = i18n.Mark("fills the rest of the time and costs no mana")
)

var skippedWhy = map[string]string{
	"debuff":   i18n.Mark("costs more casting time than it returns in this fight"),
	"filler":   i18n.Mark("the weaker filler in this fight"),
	"dot":      i18n.Mark("costs more casting time than it returns in this fight"),
	"cooldown": i18n.Mark("costs more casting time than it returns in this fight"),
	"burn":     i18n.Mark("your health does not pay for enough of it, or the mana is worth more on the other spells"),
}

// Clock is a time in a fight: "45 s" or "2:24 min".
func Clock(seconds float64) string {
	if seconds < 60 {
		return i18n.T("{n:.0f} s", "n", seconds)
	}
	return i18n.T("{m}:{s:02d} min", "m", int64(math.Floor(seconds/60)), "s", int64(math.Mod(seconds, 60)))
}

// Labeler names a spell with the game's own rank line from the spellbook export (localized, "Rang 3"), else
// the position in the rank chain.
type Labeler func(spellID int, name string, rank int) string

// NewLabeler is rotations.labeler.
func NewLabeler(c *sv.Table) Labeler {
	lines := map[int]string{}
	spells := c.Tab("spellbook").Tab("spells")
	for _, k := range spells.Keys() {
		id, err := strconv.Atoi(sv.KeyString(k))
		if err != nil {
			continue
		}
		v, _ := spells.Get(k).(*sv.Table)
		lines[id] = v.Str("rank")
	}
	return func(spellID int, name string, rank int) string {
		line := lines[spellID]
		if line == "" && rank > 1 {
			line = i18n.T("Rank {n}", "n", rank)
		}
		if line != "" {
			if name == "" {
				name = "None" // Python's f-string of a missing name
			}
			return name + " (" + line + ")"
		}
		if name == "" {
			return "?"
		}
		return name
	}
}

// Step is one line of a situation's priority list as the guide says it.
type Step struct {
	Kind         string
	SpellID      int
	Label        string
	Rule         string
	Casts        float64
	PerMin       float64
	FirstSec     float64
	SharePct     float64
	Value        float64
	PerCast      float64
	ManaPerSec   float64
	Targets      float64
	Area         bool
	HealthPerSec float64
}

// Skip is a known spell the best rotation leaves out.
type Skip struct {
	Kind    string
	SpellID int
	Label   string
	Loss    float64
	Why     string
}

// Event is one entry of the played fight's start.
type Event struct {
	T       float64
	Kind    string
	Label   string
	N       int
	ManaPct int64
	HPPct   int64
}

// Situation is one situation of a guide as rotations.situation_out returns it.
type Situation struct {
	Key, Title, About    string
	Value                float64
	Unit, Verdict        string
	OOMSeconds           *float64
	Steps                []Step
	Skipped              []Skip
	Timeline             []Event
	More, Candidates     int
	Fight                engine.Fight
	Demand               *run.Demand
	FreeMana             bool
	EndManaPct, EndHPPct int64
}

func idOrNil(id int) any {
	if id == 0 {
		return nil
	}
	return id
}

func floatOrNil(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// FightObj is an engine fight as the engine's JSON has it.
func FightObj(f engine.Fight) *pyjson.Obj {
	o := pyjson.New("seconds", f.Seconds, "targetDelta", f.TargetDelta, "killsPerMinute", f.KillsPerMinute)
	if f.Targets != 0 {
		o.Set("targets", f.Targets)
	}
	return o
}

func demandObj(d *run.Demand) any {
	if d == nil {
		return nil
	}
	return pyjson.New("targets", d.Targets, "pct", d.Pct, "value", d.Value, "maxValue", d.MaxValue,
		"manaPerSec", d.ManaPS, "regenPerSec", d.RegenPS, "castPct", d.CastPct, "freePct", d.FreePct,
		"manaMax", d.ManaMax)
}

// Obj is the step as JSON.
func (st Step) Obj() *pyjson.Obj {
	return pyjson.New("kind", st.Kind, "spellId", idOrNil(st.SpellID), "label", st.Label, "rule", st.Rule,
		"casts", st.Casts, "perMin", st.PerMin, "firstSec", st.FirstSec, "sharePct", st.SharePct,
		"value", st.Value, "perCast", st.PerCast, "manaPerSec", st.ManaPerSec, "targets", st.Targets,
		"area", st.Area, "healthPerSec", st.HealthPerSec)
}

// Obj is the situation as JSON.
func (s Situation) Obj() *pyjson.Obj {
	steps := make([]any, len(s.Steps))
	for i, st := range s.Steps {
		steps[i] = st.Obj()
	}
	skipped := make([]any, len(s.Skipped))
	for i, k := range s.Skipped {
		skipped[i] = pyjson.New("kind", k.Kind, "spellId", k.SpellID, "label", k.Label, "loss", k.Loss, "why", k.Why)
	}
	timeline := make([]any, len(s.Timeline))
	for i, e := range s.Timeline {
		timeline[i] = pyjson.New("t", e.T, "kind", e.Kind, "label", e.Label, "n", e.N, "manaPct", e.ManaPct,
			"hpPct", e.HPPct)
	}
	return pyjson.New("key", s.Key, "title", s.Title, "about", s.About, "value", s.Value, "unit", s.Unit,
		"verdict", s.Verdict, "oomSeconds", floatOrNil(s.OOMSeconds), "steps", steps, "skipped", skipped,
		"timeline", timeline, "more", s.More, "candidates", s.Candidates, "fight", FightObj(s.Fight),
		"demand", demandObj(s.Demand), "freeMana", s.FreeMana, "endManaPct", s.EndManaPct,
		"endHpPct", s.EndHPPct)
}

func healRule(i int, steps []run.Step, demand *run.Demand) string {
	step := steps[i]
	targets := demand.Targets
	if targets == 0 {
		targets = 1
	}
	switch step.Kind {
	case "mana":
		if step.Casts != 0 {
			return i18n.T("{n:.0f}x before the mana is gone: {mana:.0f} mana for {health:.0f} health, which you "+
				"heal back", "n", step.Casts, "mana", step.ManaGain, "health", step.HealthCost)
		}
		return i18n.T("every {minutes:.0f} minutes: {mana:.0f} mana for {health:.0f} health, which you heal back; "+
			"with it the mana lasts", "minutes", 1/math.Max(step.PerMin, 1e-9), "mana", step.ManaGain,
			"health", step.HealthCost)
	case "hot", "cooldown":
		kept := step.Targets
		if targets > 1 {
			return i18n.T("keep it on {n:.0f} of the {targets} targets", "n", kept, "targets", targets)
		}
		if kept >= 0.95 {
			return i18n.T("keep it on the target")
		}
		return i18n.T("on the target {pct:.0f}% of the time", "pct", kept*100)
	}
	// Of several heals the one with the least mana per point is the default; the others fill in when it is
	// too slow for the damage.
	heals, cheapest := 0, i
	best := math.Inf(1)
	for j, s := range steps {
		if s.Kind != "heal" || s.Value <= 0 {
			continue
		}
		heals++
		if price := s.ManaPS / s.Value; price < best {
			best, cheapest = price, j
		}
	}
	if heals > 1 && i != cheapest {
		return i18n.T("in between, when the default heal is too slow")
	}
	if demand.CastPct < 99.5 {
		return i18n.T("the default heal, with pauses in between")
	}
	return i18n.T("the default heal")
}

func damageRule(step run.Step, enemies int) string {
	kind := step.Kind
	values := []any{"time", Clock(step.FirstSec), "pct", step.TakenPct, "floor", step.FloorPct,
		"health", step.HealthPerSec}
	if enemies > 1 {
		kept := step.Targets
		if (kind == "dot" || kind == "debuff") && 0 < kept && kept < float64(enemies) {
			if kept < 1.5 {
				return i18n.T(keptOnOne)
			}
			return i18n.T(keptOnSome, "n", kept, "targets", enemies)
		}
		if kind == "filler" && step.Area {
			return i18n.T(areaFiller)
		}
		if text, ok := rulesSeveral[kind]; ok {
			return i18n.T(text, values...)
		}
	}
	if text, ok := rules[kind]; ok {
		return i18n.T(text, values...)
	}
	return ""
}

func damageVerdict(s run.Situation, steps []Step) string {
	if s.FreeMana {
		// Mana was no limit: the only question is how long the real bar pays for it.
		if s.OOMSec == nil {
			return i18n.T("Your mana pays for the whole fight like this: it is the rotation of the normal fight.")
		}
		return i18n.T("Your mana pays for {time} of it. A fight that lasts longer needs the rotation of the normal "+
			"fight.", "time", Clock(*s.OOMSec))
	}
	byKind := map[string]*Step{}
	for i := range steps {
		byKind[steps[i].Kind] = &steps[i]
	}
	trade := byKind["lifetap"]
	if trade == nil {
		trade = byKind["mana"]
	}
	recover := byKind["recover"]
	var wand *Step
	if w := byKind["wand"]; w != nil && w.SharePct >= 1 {
		wand = w
	}
	end := i18n.T("At the end {mana:.0f}% mana and {health:.0f}% health are left.", "mana", s.EndManaPct,
		"health", s.EndHPPct)
	if byKind["filler"] == nil && byKind["wand"] != nil {
		// The wand by choice, not for lack of mana.
		return i18n.T("No filler spell: in this fight the mana is worth more on the spells above, and the "+
			"pauses on the wand regenerate it.") + " " + end
	}
	var parts []string
	if recover != nil && trade != nil {
		parts = append(parts, i18n.T("From {time} on the health is too low for {trade}: {recover} brings it back.",
			"time", Clock(recover.FirstSec), "trade", trade.Label, "recover", recover.Label))
	}
	if wand != nil {
		parts = append(parts, i18n.T("From {time} on the mana is gone and the wand fills the gaps.",
			"time", Clock(wand.FirstSec)))
	}
	if len(parts) == 0 {
		if trade != nil {
			parts = append(parts, i18n.T("The mana lasts with {trade}.", "trade", trade.Label))
		} else {
			parts = append(parts, i18n.T("The mana lasts for the whole fight."))
		}
	}
	parts = append(parts, end)
	out := parts[0]
	for _, p := range parts[1:] {
		out += " " + p
	}
	return out
}

type nameKey struct {
	kind string
	id   int
}

func keyOf(kind string, id int) nameKey {
	if kind == "wand" || kind == "lifetap" {
		return nameKey{kind, 0}
	}
	return nameKey{"", id}
}

// SituationOut is rotations.situation_out: one situation of the engine with its texts.
func SituationOut(s run.Situation, label Labeler, unit string) Situation {
	fight, demand := s.Fight, s.Demand
	g, ok := situations[s.Key]
	if !ok {
		g = guide{s.Key, ""}
	}
	seconds := fight.Seconds
	about := ""
	if g.about != "" {
		var value, pct float64
		targets := fight.Targets
		if demand != nil {
			value, pct, targets = demand.Value, demand.Pct, demand.Targets
		}
		if targets == 0 {
			targets = 1
		}
		about = i18n.T(g.about, "seconds", seconds, "minutes", seconds/60, "delta", fight.TargetDelta,
			"value", value, "pct", pct, "targets", targets)
	}
	enemies := 1
	if demand == nil && fight.Targets != 0 {
		enemies = fight.Targets
	}
	names := map[nameKey]string{{"wand", 0}: i18n.T("Wand")}
	hasFiller, hasStandby := false, false
	for _, st := range s.Steps {
		hasFiller = hasFiller || st.Kind == "filler"
		hasStandby = hasStandby || st.Kind == "recover"
	}
	steps := make([]Step, 0, len(s.Steps))
	for i, st := range s.Steps {
		text := i18n.T("Wand")
		if st.Kind != "wand" {
			text = label(st.SpellID, st.Name, st.Rank)
		}
		// The played fight names Life Tap and the wand by their kind, every other action by its spell.
		names[keyOf(st.Kind, st.SpellID)] = text
		var rule string
		switch {
		case demand != nil:
			rule = healRule(i, s.Steps, demand)
		case st.Kind == "wand" && !hasFiller:
			rule = i18n.T(wandFills)
		case st.Kind == "lifetap" && hasStandby:
			rule = i18n.T(tapWithStandby)
		default:
			rule = damageRule(st, enemies)
		}
		steps = append(steps, Step{Kind: st.Kind, SpellID: st.SpellID, Label: text, Rule: rule, Casts: st.Casts,
			PerMin: pyfmt.Round(st.PerMin, 2), FirstSec: st.FirstSec, SharePct: pyfmt.Round(st.SharePct, 1),
			Value: pyfmt.Round(st.Value, 1), PerCast: pyfmt.Round(st.PerCast, 1),
			ManaPerSec: pyfmt.Round(st.ManaPS, 2), Targets: pyfmt.Round(st.Targets, 2), Area: st.Area,
			HealthPerSec: pyfmt.Round(st.HealthPerSec, 1)})
	}
	skipped := make([]Skip, 0, len(s.Skipped))
	for _, k := range s.Skipped {
		why := ""
		if text, ok := skippedWhy[k.Kind]; ok {
			why = i18n.T(text)
		}
		skipped = append(skipped, Skip{Kind: k.Kind, SpellID: k.SpellID, Label: label(k.SpellID, k.Name, k.Rank),
			Loss: pyfmt.Round(k.Loss, 1), Why: why})
	}
	timeline := make([]Event, 0, len(s.Timeline))
	for _, e := range s.Timeline {
		name := names[keyOf(e.Kind, e.SpellID)]
		if name == "" {
			name = e.Kind
		}
		n := e.N
		if n == 0 {
			n = 1
		}
		timeline = append(timeline, Event{T: pyfmt.Round(e.T, 1), Kind: e.Kind, Label: name, N: n,
			ManaPct: pyfmt.RoundInt(e.ManaPct), HPPct: pyfmt.RoundInt(e.HPPct)})
	}

	var verdict string
	if demand != nil {
		balance := []any{"spent", demand.ManaPS, "regen", demand.RegenPS}
		if s.OOMSec == nil {
			verdict = i18n.T("The mana lasts: {spent:.1f} mana per second against {regen:.1f} regenerated.", balance...)
		} else {
			verdict = i18n.T("The mana lasts {time}: {spent:.1f} mana per second against {regen:.1f} regenerated.",
				append([]any{"time", Clock(*s.OOMSec)}, balance...)...)
		}
		if demand.CastPct < 99.5 {
			verdict += " " + i18n.T("You cast {cast:.0f}% of the time.", "cast", demand.CastPct)
		}
		if demand.FreePct >= 0.5 {
			verdict += " " + i18n.T("{free:.0f}% of the time is outside the five-second rule and regenerates in full.",
				"free", demand.FreePct)
		}
	} else {
		verdict = damageVerdict(s, steps)
	}
	return Situation{Key: s.Key, Title: i18n.T(g.title), About: about, Value: pyfmt.Round(s.Value, 1), Unit: unit,
		Verdict: verdict, OOMSeconds: s.OOMSec, Steps: steps, Skipped: skipped, Timeline: timeline, More: s.More,
		Candidates: s.Candidates, Fight: fight, Demand: demand, FreeMana: s.FreeMana,
		EndManaPct: pyfmt.RoundInt(s.EndManaPct), EndHPPct: pyfmt.RoundInt(s.EndHPPct)}
}

// Setup is what rotations.character returns: the snapshot, the fight as asked for and a function that
// starts the engine for a metric.
type Setup struct {
	C     *sv.Table
	Fight engine.Fight
	// Start starts the engine for a metric; seconds 0 keeps the fight's length, buffs may be nil.
	Start func(metric string, seconds float64, buffs map[string]float64) (*enginebundle.Engine, error)
}

// Character is rotations.character. prepare may change the snapshot before the engine sees it (the combat
// log's spell power instead of the sheet's).
func Character(wow, char string, fightSeconds float64, targetDelta int, killsPerMinute float64,
	prepare func(*sv.Table)) (*Setup, error) {
	files := game.FindChars(wow, char)
	if len(files) == 0 {
		return nil, errors.New(i18n.T("No WoWToolsForever.lua found for this character."))
	}
	c, err := upgrades.LoadChar(files[0])
	if err != nil {
		return nil, err
	}
	if prepare != nil {
		prepare(c)
	}
	ch := c.Tab("character")
	classFile := ch.Str("classFile")
	if !enginebundle.Supported(classFile) {
		cls := classFile
		if cls == "" {
			cls = "?"
		}
		return nil, fmt.Errorf("%s", i18n.T("The combat engine does not model this class yet ({cls}).", "cls", cls))
	}
	st := ch.Tab("stats")
	_, totals := upgrades.EquippedStats(c, nil)
	lifeTap := engine.LifeTap{}
	if m := upgrades.LifeTapText.FindStringSubmatch(st.Tab("lifeTap").Str("text")); m != nil && upgrades.Number(m[1]) > 0 {
		lifeTap = engine.LifeTap{HealthPerCast: upgrades.Number(m[1]), ManaPerCast: upgrades.Number(m[2])}
	}
	level, _ := ch.Num("level")
	if level == 0 {
		level = 60
	}
	profile := "dungeon"
	if level < 60 {
		profile = "leveling"
	}
	fight := engine.Fight{Seconds: fightSeconds, TargetDelta: targetDelta, KillsPerMinute: -1}
	if targetDelta < 0 || targetDelta > 3 {
		fight.TargetDelta = upgrades.Profiles[profile].Target
	}
	if killsPerMinute >= 0 {
		fight.KillsPerMinute = killsPerMinute
	}
	start := func(metric string, seconds float64, buffs map[string]float64) (*enginebundle.Engine, error) {
		f := fight
		if seconds != 0 {
			f.Seconds = seconds
		}
		return enginebundle.Start(c, totals["mp5"], totals["hp5"], f, metric, lifeTap, enginebundle.Options{Buffs: buffs})
	}
	return &Setup{C: c, Fight: fight, Start: start}, nil
}

// Name is `ch.get("name") or char`.
func Name(ch *sv.Table, char string) string {
	if n := ch.Str("name"); n != "" {
		return n
	}
	return char
}

// Level is `ch.get("level") or 60`.
func Level(ch *sv.Table) any {
	if v := ch.Get("level"); sv.Truthy(v) {
		return v
	}
	return 60
}

// Class is `ch.get("class") or class_file`.
func Class(ch *sv.Table) string {
	if n := ch.Str("class"); n != "" {
		return n
	}
	return ch.Str("classFile")
}

// Compute is rotations.compute: the rotation guide of a character, one guide per metric the class has
// (damage, healing), each with its situations. fightBuffs, when set, is consumables.fight_buffs (the bridge
// passes it for --consumables; this package cannot import consumables): every fight of a guide is then
// played with the best set for its metric as buffs (the set and a potion's share are those of the normal
// fight), and the guide names the set.
func Compute(wow, char string, fightSeconds float64, targetDelta int, killsPerMinute float64,
	fightBuffs upgrades.FightBuffsFunc) (*pyjson.Obj, error) {
	setup, err := Character(wow, char, fightSeconds, targetDelta, killsPerMinute, nil)
	if err != nil {
		return nil, err
	}
	c := setup.C
	ch := c.Tab("character")
	classFile := ch.Str("classFile")

	// The spec's own metric first; the time-to-OOM metric has no guide of its own.
	first := enginebundle.DefaultMetric(classFile, enginebundle.SpecOf(c))
	var metrics []string
	for _, m := range enginebundle.Metrics[classFile] {
		if _, ok := guides[m]; ok {
			metrics = append(metrics, m)
		}
	}
	sort.SliceStable(metrics, func(i, j int) bool { return metrics[i] == first && metrics[j] != first })
	label := NewLabeler(c)
	var out []any
	version, source := "", ""
	for _, metric := range metrics {
		var consumed *upgrades.Consumed
		var buffs map[string]float64
		if fightBuffs != nil {
			if consumed, err = fightBuffs(metric, fightSeconds, setup.Fight.TargetDelta); err != nil {
				return nil, err
			}
			buffs = consumed.Delta
		}
		eng, err := setup.Start(metric, 0, buffs)
		if err != nil {
			return nil, err
		}
		sits, err := eng.Situations()
		if err != nil {
			return nil, err
		}
		version, source = eng.Version, eng.SpellbookSource
		if len(sits) == 0 {
			continue
		}
		g := guides[metric]
		notes := []any{}
		for _, n := range guideNotes[metric] {
			notes = append(notes, i18n.T(n))
		}
		list := make([]any, len(sits))
		for i, s := range sits {
			list[i] = SituationOut(s, label, Units[metric]).Obj()
		}
		guide := pyjson.New("metric", metric, "unit", Units[metric], "title", i18n.T(g.title),
			"about", i18n.T(g.about), "notes", notes, "situations", list)
		if consumed != nil {
			names := make([]any, len(consumed.Names))
			for i, n := range consumed.Names {
				names[i] = n
			}
			guide.Set("consumables", pyjson.New("names", names, "gain", consumed.Gain, "unit", consumed.Unit,
				"text", consumed.Text))
		}
		out = append(out, guide)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s", i18n.T("The combat engine has no rotation guide for this character."))
	}
	return pyjson.New("name", Name(ch, char), "level", Level(ch), "class", Class(ch), "classFile", classFile,
		"spec", enginebundle.SpecOf(c), "guides", out,
		"engine", pyjson.New("version", version, "spellbookSource", source),
		"fight", pyjson.New("seconds", fightSeconds, "targetDelta", setup.Fight.TargetDelta)), nil
}
