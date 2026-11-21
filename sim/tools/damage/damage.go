// Package damage is the Go side of scripts/damage.py: the damage analysis, the counterpart of the heal
// table. The rotations (maximum damage first, then sustained damage and the other fights), every damage
// spell and rank the character knows with its numbers, the wand, and what a point of each stat adds.
package damage

import (
	"fmt"
	"math"

	"github.com/kejith/wow-forever-tools/sim/engine"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/run"
	"github.com/kejith/wow-forever-tools/sim/tools/enginebundle"
	"github.com/kejith/wow-forever-tools/sim/tools/healing"
	"github.com/kejith/wow-forever-tools/sim/tools/rotations"
)

// Metric is the engine metric of the damage analysis.
const Metric = "dps"

// Weights are damage.WEIGHTS.
var Weights = []healing.Weight{w("sp", i18n.Mark("Spell power"), 10), w("int", i18n.Mark("Intellect"), 10),
	w("spi", i18n.Mark("Spirit"), 10), w("sta", i18n.Mark("Stamina"), 10), w("mp5", i18n.Mark("Mana per 5 sec."), 5),
	w("hit", i18n.Mark("Spell hit, per 1%"), 1), w("crit", i18n.Mark("Spell crit, per 1%"), 1)}

func w(key, label string, amount float64) healing.Weight {
	return healing.Weight{Key: key, Label: label, Amount: amount}
}

// Schools are the names of the magic schools.
var Schools = map[string]string{"shadow": i18n.Mark("Shadow"), "fire": i18n.Mark("Fire"), "holy": i18n.Mark("Holy"),
	"frost": i18n.Mark("Frost"), "nature": i18n.Mark("Nature"), "arcane": i18n.Mark("Arcane"),
	"physical": i18n.Mark("Physical")}

// order is the order of the rotations: the two ends first (everything at once, what lasts), then the other
// fights.
var order = []string{"max", "endurance", "standard", "quick", "boss", "pack", "crowd"}

// Supported tells whether the combat engine has a damage metric for the class.
func Supported(classFile string) bool { return enginebundle.HasMetric(classFile, Metric) }

type row struct {
	obj       *pyjson.Obj
	dps, dpm  float64
	spammable bool
	area      bool
	best      []any
}

func schoolLabel(school string) string {
	if name, ok := Schools[school]; ok {
		return i18n.T(name)
	}
	return ""
}

// rowOut is damage.row_out: one rank of the engine's damage table with the columns derived from it.
func rowOut(r run.DamageRow, label rotations.Labeler, uses map[int][]healing.Use, casting, manaMax float64) *row {
	cast, mana, execute := r.PerCast, r.Mana, r.ExecuteSec
	kind := r.Kind
	if kind == "" {
		kind = "direct"
	}
	duration, cooldown := r.DurationSec, r.CooldownSec
	perCastSec := r.DPS
	// What the spell does per second when it is used as much as it can be on one enemy: a spell over time
	// is kept up, a cooldown is used whenever it is ready, everything else is cast back to back.
	dps, limit := perCastSec, any(nil)
	if cooldown > execute {
		dps, limit = cast/math.Max(cooldown, duration), "cooldown"
	} else if (kind == "dot" || kind == "both") && duration > execute {
		dps, limit = cast/duration, "kept"
	}
	// Casting nothing else: how long the mana lasts and what it does until then.
	spammable := (kind == "direct" || kind == "channel") && limit == nil
	var spamSec, spamTotal float64
	if spammable {
		spamSec, spamTotal = healing.SpamOf(cast, mana, execute, casting, manaMax)
	}
	bonusPct := any(0)
	if cast != 0 {
		bonusPct = pyfmt.Round(r.Bonus/cast*100, 1)
	}
	var perBar any
	if mana != 0 {
		perBar = pyfmt.Round(manaMax/mana, 1)
	}
	out := &row{dps: pyfmt.Round(dps, 1), dpm: pyfmt.Round(r.DPM, 2), spammable: spammable, area: r.Area, best: []any{}}
	out.obj = pyjson.New("spellId", r.SpellID, "baseId", r.BaseID, "name", r.Name,
		"label", label(r.SpellID, r.Name, r.Rank), "rank", r.Rank, "level", r.Level, "top", r.Top, "kind", kind,
		"school", r.School, "schoolLabel", schoolLabel(r.School),
		"min", pyfmt.RoundInt(r.Min), "max", pyfmt.RoundInt(r.Max),
		"perTick", pyfmt.Round(r.PerTick, 1), "ticks", r.Ticks, "durationSec", duration,
		"critPct", pyfmt.Round(r.CritPct, 1), "tickCrits", r.TickCrits,
		"hitPct", pyfmt.Round(r.HitPct, 1), "perCast", pyfmt.Round(cast, 1), "mana", pyfmt.RoundInt(mana),
		"castSec", pyfmt.Round(r.CastSec, 2), "executeSec", pyfmt.Round(execute, 2), "cooldownSec", cooldown,
		"perCastSec", pyfmt.Round(perCastSec, 1), "dps", out.dps, "limit", limit,
		"dpm", out.dpm, "bonus", pyfmt.Round(r.Bonus, 1),
		"bonusPct", bonusPct, "coefPct", pyfmt.Round(r.CoefPct, 1), "leech", pyfmt.RoundInt(r.Leech),
		"selfDamage", pyfmt.RoundInt(r.SelfDamage), "area", r.Area,
		"castsPerBar", perBar, "spammable", spammable,
		"spamSeconds", healing.OrNil(spamSec, 1, false), "spamDamage", healing.OrNil(spamTotal, 0, true),
		"uses", healing.UsesList(uses[r.SpellID]))
	return out
}

// Compute is damage.compute: the damage analysis of a character as plain data.
func Compute(wow, char string, fightSeconds float64, targetDelta int, killsPerMinute float64) (*pyjson.Obj, error) {
	setup, err := rotations.Character(wow, char, fightSeconds, targetDelta, killsPerMinute, nil)
	if err != nil {
		return nil, err
	}
	c := setup.C
	ch := c.Tab("character")
	classFile := ch.Str("classFile")
	if !Supported(classFile) {
		cls := classFile
		if cls == "" {
			cls = "?"
		}
		return nil, fmt.Errorf("%s", i18n.T("The combat engine has no damage spells for this class ({cls}).", "cls", cls))
	}
	label := rotations.NewLabeler(c)
	evals := healing.Evals(Weights)

	eng, err := setup.Start(Metric, 0, nil)
	if err != nil {
		return nil, err
	}
	table, situations, err := eng.DamageTable()
	if err != nil {
		return nil, err
	}
	if table == nil || len(table.Rows) == 0 {
		return nil, fmt.Errorf("%s", i18n.T("This character knows no damage spell the combat engine models."))
	}
	baseline := eng.Baseline
	if baseline == nil {
		baseline = &engine.Metrics{}
	}
	short, err := eng.Batch(evals, false)
	if err != nil {
		return nil, err
	}
	longEng, err := setup.Start(Metric, healing.LongFight, nil)
	if err != nil {
		return nil, err
	}
	long, err := longEng.Batch(evals, false)
	if err != nil {
		return nil, err
	}

	// The rotations, and where they cast each rank.
	unit := rotations.Units[Metric]
	guide := map[string]rotations.Situation{}
	var keys []string
	for _, s := range situations {
		if _, seen := guide[s.Key]; !seen {
			keys = append(keys, s.Key)
		}
		guide[s.Key] = rotations.SituationOut(s, label, unit)
	}
	var played []rotations.Situation
	inOrder := map[string]bool{}
	for _, k := range order {
		inOrder[k] = true
		if s, ok := guide[k]; ok {
			played = append(played, s)
		}
	}
	for _, k := range keys {
		if !inOrder[k] {
			played = append(played, guide[k])
		}
	}
	uses := map[int][]healing.Use{}
	for _, s := range played {
		for _, st := range s.Steps {
			if st.SpellID != 0 && st.Kind != "mana" && st.Kind != "lifetap" {
				uses[st.SpellID] = append(uses[st.SpellID], healing.Use{Key: s.Key, Title: s.Title})
			}
		}
	}

	casting, idle := table.RegenCastingPS, table.RegenIdlePS
	manaMax := table.ManaMax
	rows := make([]*row, len(table.Rows))
	for i, r := range table.Rows {
		rows[i] = rowOut(r, label, uses, casting, manaMax)
	}
	// The marks: the most damage per mana of all, and the most per second among the spells cast back to back
	// on one enemy.
	var bestDPM, bestDPS *row
	for _, r := range rows {
		if r.dpm > 0 && (bestDPM == nil || r.dpm > bestDPM.dpm) {
			bestDPM = r
		}
		if r.spammable && !r.area && (bestDPS == nil || r.dps > bestDPS.dps) {
			bestDPS = r
		}
	}
	if bestDPM != nil {
		bestDPM.best = append(bestDPM.best, "dpm")
	}
	if bestDPS != nil {
		bestDPS.best = append(bestDPS.best, "dps")
	}
	rowList := make([]any, len(rows))
	for i, r := range rows {
		rowList[i] = r.obj.Set("best", r.best)
	}

	delta := table.TargetDelta
	schools := make([]any, len(table.Schools))
	for i, s := range table.Schools {
		name := s.School
		if l, ok := Schools[s.School]; ok {
			name = i18n.T(l)
		}
		schools[i] = pyjson.New("school", s.School, "label", name, "power", pyfmt.RoundInt(s.Power),
			"critPct", pyfmt.Round(s.CritPct, 2))
	}
	stats := pyjson.New("schools", schools, "missPct", pyfmt.Round(table.MissPct, 1), "targetDelta", delta,
		"manaMax", pyfmt.RoundInt(manaMax), "healthMax", pyfmt.RoundInt(table.HealthMax),
		"castingPer5", pyfmt.Round(casting*5, 1), "idlePer5", pyfmt.Round(idle*5, 1),
		"wandDps", pyfmt.Round(table.WandDPS, 1),
		"tapMana", pyfmt.RoundInt(table.TapMana), "tapHealth", pyfmt.RoundInt(table.TapHealth))
	notes := []any{
		i18n.T("Every number is against an enemy {delta} levels above you, with your talents in it and the "+
			"buffs you had at your last snapshot.", "delta", delta),
		i18n.T("A row of the spell table is one cast alone: what your other spells add to it (a curse on the " +
			"enemy, talents that count your spells over time) shows in the rotations."),
		i18n.T("The rotations cast the highest rank of each spell; a lower rank is in the table for its " +
			"damage per mana."),
	}

	outRotations := make([]any, len(played))
	for i, s := range played {
		steps := make([]any, len(s.Steps))
		for j, st := range s.Steps {
			steps[j] = pyjson.New("label", st.Label, "rule", st.Rule, "kind", st.Kind, "value", st.Value,
				"sharePct", st.SharePct, "perMin", st.PerMin)
		}
		skipped := make([]any, len(s.Skipped))
		for j, k := range s.Skipped {
			skipped[j] = pyjson.New("label", k.Label, "loss", k.Loss)
		}
		var oom any
		if s.OOMSeconds != nil {
			oom = *s.OOMSeconds
		}
		outRotations[i] = pyjson.New("key", s.Key, "title", s.Title, "about", s.About, "value", s.Value,
			"oomSeconds", oom, "freeMana", s.FreeMana, "verdict", s.Verdict, "seconds", s.Fight.Seconds,
			"steps", steps, "skipped", skipped)
	}
	return pyjson.New("name", rotations.Name(ch, char), "level", rotations.Level(ch), "class", rotations.Class(ch),
		"classFile", classFile, "spec", enginebundle.SpecOf(c), "unit", unit,
		"rows", rowList, "stats", stats, "notes", notes, "rotations", outRotations,
		// The gear rating's number for the fight asked for: a faster estimate of the same fight.
		"rated", pyjson.New("seconds", fightSeconds, "value", pyfmt.Round(baseline.Value, 1)),
		"weights", healing.WeightsOf(Weights, "Power", short, long, fightSeconds),
		"engine", pyjson.New("version", eng.Version, "spellbookSource", eng.SpellbookSource),
		"fight", pyjson.New("seconds", fightSeconds, "targetDelta", setup.Fight.TargetDelta)), nil
}
