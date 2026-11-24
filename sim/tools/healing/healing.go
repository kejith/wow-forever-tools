// Package healing is the Go side of scripts/healing.py: the heal table, every heal and shield rank the
// character knows with what one cast gives and costs, where the rotation guide casts it, the regeneration
// and what a point of each stat adds to the healing.
package healing

import (
	"fmt"

	"github.com/kejith/wow-tools-forever/sim/engine"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
	"github.com/kejith/wow-tools-forever/sim/tools/rotations"
)

// Metric is the engine metric of the heal table.
const Metric = "hps"

// LongFight is the second fight the stat weights are given for.
const LongFight = 300.0

// Weight is one stat whose worth is measured: the engine's delta key, the label, and how much is added
// for the measurement (enough to move the result, small enough to stay a slope).
type Weight struct {
	Key    string
	Label  string
	Amount float64
}

// Weights are healing.WEIGHTS.
var Weights = []Weight{{"healing", i18n.Mark("Healing bonus"), 10}, {"int", i18n.Mark("Intellect"), 10},
	{"spi", i18n.Mark("Spirit"), 10}, {"mp5", i18n.Mark("Mana per 5 sec."), 5},
	{"crit", i18n.Mark("Spell crit, per 1%"), 1}}

// Supported tells whether the combat engine has a healing metric for the class.
func Supported(classFile string) bool { return enginebundle.HasMetric(classFile, Metric) }

// WeightsOf is healing.weights_of and damage.weights_of: what a point adds in two fights, and how many
// points of the base stat (the first weight) do the same; suffix names that column ("Bonus", "Power").
func WeightsOf(weights []Weight, suffix string, short, long map[string]float64, seconds float64) *pyjson.Obj {
	rows := []any{}
	for _, w := range weights {
		row := pyjson.New("key", w.Key, "label", i18n.T(w.Label))
		for _, g := range []struct {
			name  string
			gains map[string]float64
		}{{"short", short}, {"long", long}} {
			per := g.gains[w.Key] / w.Amount
			base := g.gains[weights[0].Key] / weights[0].Amount
			row.Set(g.name, pyfmt.Round(per, 3))
			if base > 0 {
				row.Set(g.name+suffix, pyfmt.Round(per/base, 2))
			} else {
				row.Set(g.name+suffix, nil)
			}
		}
		rows = append(rows, row)
	}
	return pyjson.New("seconds", seconds, "longSeconds", LongFight, "rows", rows)
}

// Evals are the measurements of a list of weights.
func Evals(weights []Weight) []enginebundle.Eval {
	out := make([]enginebundle.Eval, len(weights))
	for i, w := range weights {
		out[i] = enginebundle.Eval{Key: w.Key, Delta: map[string]float64{w.Key: w.Amount}}
	}
	return out
}

// Use is one situation that casts a rank.
type Use struct{ Key, Title string }

// UsesList is a rank's uses as JSON.
func UsesList(uses []Use) []any {
	out := make([]any, len(uses))
	for i, u := range uses {
		out[i] = pyjson.New("key", u.Key, "title", u.Title)
	}
	return out
}

// OrNil is `round(x, n) if x else None` for an optional number.
func OrNil(x float64, n int, whole bool) any {
	if x == 0 {
		return nil
	}
	if whole {
		return pyfmt.RoundInt(x)
	}
	return pyfmt.Round(x, n)
}

// SpamOf is "casting nothing else": how long the mana lasts and what the casts do until then (0, 0 when it
// lasts or the spell costs nothing).
func SpamOf(cast, mana, execute, casting, manaMax float64) (seconds, total float64) {
	if execute > 0 && mana > 0 {
		if net := mana/execute - casting; net > 0 {
			seconds = manaMax / net
			total = seconds * cast / execute
		}
	}
	return
}

type row struct {
	obj  *pyjson.Obj
	kind string
	hpm  float64
	hps  float64
	best []any
}

// Compute is healing.compute: the heal table of a character as plain data.
func Compute(wow, char string, fightSeconds float64, targetDelta int) (*pyjson.Obj, error) {
	setup, err := rotations.Character(wow, char, fightSeconds, targetDelta, -1, nil)
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
		return nil, fmt.Errorf("%s", i18n.T("The combat engine has no heals for this class ({cls}).", "cls", cls))
	}
	label := rotations.NewLabeler(c)
	evals := Evals(Weights)

	eng, err := setup.Start(Metric, 0, nil)
	if err != nil {
		return nil, err
	}
	table, situations, err := eng.HealTable()
	if err != nil {
		return nil, err
	}
	if table == nil || len(table.Rows) == 0 {
		return nil, fmt.Errorf("%s", i18n.T("This character knows no heal the combat engine models."))
	}
	baseline := eng.Baseline
	if baseline == nil {
		baseline = &engine.Metrics{}
	}
	short, err := eng.Batch(evals, false)
	if err != nil {
		return nil, err
	}
	longEng, err := setup.Start(Metric, LongFight, nil)
	if err != nil {
		return nil, err
	}
	long, err := longEng.Batch(evals, false)
	if err != nil {
		return nil, err
	}
	longValue := 0.0
	if longEng.Baseline != nil {
		longValue = longEng.Baseline.Value
	}

	// Where the guide casts each rank.
	unit := rotations.Units[Metric]
	guide := make([]rotations.Situation, len(situations))
	for i, s := range situations {
		guide[i] = rotations.SituationOut(s, label, unit)
	}
	uses := map[int][]Use{}
	for _, s := range guide {
		for _, st := range s.Steps {
			if st.Kind != "mana" && st.SpellID != 0 {
				uses[st.SpellID] = append(uses[st.SpellID], Use{s.Key, s.Title})
			}
		}
	}
	sustained := []any{}
	for _, r := range baseline.Breakdown {
		if r.SpellID == 0 || r.Value <= 0 {
			continue
		}
		sustained = append(sustained, pyjson.New("spellId", r.SpellID, "label", label(r.SpellID, r.Label, r.Rank),
			"value", pyfmt.Round(r.Value, 1), "sharePct", pyfmt.Round(r.SharePct, 1)))
		uses[r.SpellID] = append(uses[r.SpellID], Use{"sustained", i18n.T("Sustained")})
	}

	casting, idle := table.RegenCastingPS, table.RegenIdlePS
	manaMax := table.ManaMax
	rows := make([]*row, 0, len(table.Rows))
	for _, r := range table.Rows {
		cast, mana, execute := r.PerCast, r.Mana, r.ExecuteSec
		kind := r.Kind
		if kind == "" {
			kind = "direct"
		}
		// A HoT or a shield sits on a target for its time: that is its rate there, whatever the casting takes.
		var onTarget any
		if r.OnTargetSec != 0 {
			onTarget = pyfmt.Round(cast/r.OnTargetSec, 1)
		}
		// Casting nothing else: how long the mana lasts and what it heals until then.
		var spamSec, spamTotal float64
		if kind == "direct" || kind == "both" {
			spamSec, spamTotal = SpamOf(cast, mana, execute, casting, manaMax)
		}
		bonusPct := any(0)
		if cast != 0 {
			bonusPct = pyfmt.Round(r.Bonus/cast*100, 1)
		}
		var perBar any
		if mana != 0 {
			perBar = pyfmt.Round(manaMax/mana, 1)
		}
		out := &row{kind: kind, hpm: pyfmt.Round(r.HPM, 2), hps: pyfmt.Round(r.HPS, 1), best: []any{}}
		out.obj = pyjson.New("spellId", r.SpellID, "baseId", r.BaseID, "name", r.Name,
			"label", label(r.SpellID, r.Name, r.Rank), "rank", r.Rank, "level", r.Level, "kind", kind,
			"min", pyfmt.RoundInt(r.Min), "max", pyfmt.RoundInt(r.Max),
			"perTick", pyfmt.Round(r.PerTick, 1), "ticks", r.Ticks, "durationSec", r.DurationSec,
			"absorb", pyfmt.RoundInt(r.Absorb), "critPct", pyfmt.Round(r.CritPct, 1),
			"perCast", pyfmt.Round(cast, 1), "mana", pyfmt.RoundInt(mana),
			"castSec", pyfmt.Round(r.CastSec, 2), "executeSec", pyfmt.Round(execute, 2),
			"hps", out.hps, "hpsOnTarget", onTarget, "hpm", out.hpm, "bonus", pyfmt.Round(r.Bonus, 1),
			"bonusPct", bonusPct, "coefPct", pyfmt.Round(r.CoefPct, 1),
			"bonusLossPct", pyfmt.RoundInt(r.BonusLossPct), "castsPerBar", perBar,
			"spamSeconds", OrNil(spamSec, 1, false), "spamHealing", OrNil(spamTotal, 0, true),
			"uses", UsesList(uses[r.SpellID]))
		rows = append(rows, out)
	}
	// The marks: the most healing per mana of all, the most per second among the heals that can be cast back
	// to back (a HoT or a shield fits on a target once).
	bestHPM := rows[0]
	for _, r := range rows[1:] {
		if r.hpm > bestHPM.hpm {
			bestHPM = r
		}
	}
	if bestHPM.hpm > 0 {
		bestHPM.best = append(bestHPM.best, "hpm")
	}
	var bestHPS *row
	for _, r := range rows {
		if (r.kind == "direct" || r.kind == "both") && (bestHPS == nil || r.hps > bestHPS.hps) {
			bestHPS = r
		}
	}
	if bestHPS != nil {
		bestHPS.best = append(bestHPS.best, "hps")
	}
	rowList := make([]any, len(rows))
	for i, r := range rows {
		rowList[i] = r.obj.Set("best", r.best)
	}

	pct, gear := table.CastingRegenPct, table.GearMP5
	fromSheet := enginebundle.Sheet(ch.Tab("stats")).HasCasting()
	spiritPer5 := pyfmt.Round(idle*5-gear, 1)
	stats := pyjson.New("healing", pyfmt.RoundInt(table.Healing), "critPct", pyfmt.Round(table.CritPct, 2),
		"manaMax", pyfmt.RoundInt(manaMax), "spirit", pyfmt.RoundInt(table.Spirit), "gearMp5", pyfmt.Round(gear, 1),
		"castingPer5", pyfmt.Round(casting*5, 1), "idlePer5", pyfmt.Round(idle*5, 1),
		"spiritPer5", spiritPer5, "castingRegenPct", pyfmt.Round(pct, 1),
		// The regeneration is the character sheet's own (addon 1.12): its mp5 has the buffs as well.
		"regenFromSheet", fromSheet)
	var notes []any
	if fromSheet {
		notes = append(notes, i18n.T("Mana per 5 sec. from your gear and buffs counts all the time ({gear:.0f} now). The "+
			"regeneration from spirit ({spirit:.0f} mana per 5 sec.) stops while you cast and for five "+
			"seconds after a cast that costs mana. Both numbers are those of your character sheet.",
			"gear", gear, "spirit", spiritPer5))
	} else {
		notes = append(notes, i18n.T("Mana per 5 sec. on your gear counts all the time ({gear:.0f} now). The regeneration from "+
			"spirit ({spirit:.0f} mana per 5 sec.) stops while you cast and for five seconds after a cast "+
			"that costs mana.", "gear", gear, "spirit", spiritPer5))
	}
	if pct > 0 {
		notes = append(notes, i18n.T("Your talents let {pct:.0f}% of it through while casting.", "pct", pct))
	}
	notes = append(notes, i18n.T("The numbers are those of your last snapshot, with the buffs you had then."))

	recommended := make([]any, len(guide))
	for i, s := range guide {
		steps := make([]any, len(s.Steps))
		for j, st := range s.Steps {
			steps[j] = pyjson.New("label", st.Label, "rule", st.Rule, "kind", st.Kind)
		}
		recommended[i] = pyjson.New("key", s.Key, "title", s.Title, "about", s.About, "value", s.Value,
			"oomSeconds", optional(s.OOMSeconds), "verdict", s.Verdict, "steps", steps)
	}
	return pyjson.New("name", rotations.Name(ch, char), "level", rotations.Level(ch), "class", rotations.Class(ch),
		"classFile", classFile, "spec", enginebundle.SpecOf(c), "unit", unit,
		"rows", rowList, "stats", stats, "notes", notes, "recommended", recommended,
		"sustained", pyjson.New("seconds", fightSeconds, "value", pyfmt.Round(baseline.Value, 1),
			"longSeconds", LongFight, "longValue", pyfmt.Round(longValue, 1), "steps", sustained),
		"weights", WeightsOf(Weights, "Bonus", short, long, fightSeconds),
		"engine", pyjson.New("version", eng.Version, "spellbookSource", eng.SpellbookSource),
		"fight", pyjson.New("seconds", fightSeconds, "targetDelta", setup.Fight.TargetDelta)), nil
}

func optional(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
