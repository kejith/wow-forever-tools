// Package run is the engine's JSON contract: one input bundle from the
// Python side in, one result out. The Python side keeps everything that
// needs the SavedVariables or localized tooltips (candidate filtering, slot
// logic, stat canonicalization); the engine evaluates deltas.
package run

import (
	"fmt"

	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/classes/warlock"
	"github.com/kejith/wow-forever-tools/sim/engine"
	"github.com/kejith/wow-forever-tools/sim/talents"
)

// Version of the engine contract; printed by --version and in the output.
const Version = "0.1.0"

// Input is the bundle the Python side sends. Stats are the character
// sheet's totals (gear included) from the WoWMCP snapshot; deltas are
// canonical stat changes relative to that (see engine.ApplyDelta).
type Input struct {
	CacheDir    string       `json:"cacheDir"`
	Build       string       `json:"build"` // full version, e.g. "1.60.1.70959"
	Locale      string       `json:"locale"`
	Character   Character    `json:"character"`
	Evaluations []Evaluation `json:"evaluations"`
	Fight       engine.Fight `json:"fight"`
	Metric      string       `json:"metric"`
	Method      string       `json:"method"` // "expectation" (default); "montecarlo" is the Stage-2 seam
	Iterations  int          `json:"iterations"`
}

// Character is the snapshot-derived character.
type Character struct {
	ClassFile    string         `json:"classFile"`
	Level        int            `json:"level"`
	SpecOverride string         `json:"specOverride"`
	Stats        Stats          `json:"stats"`
	Talents      map[string]int `json:"talents"`   // rank-spell id -> points
	Spellbook    []int          `json:"spellbook"` // known spell ids; empty = assume all trainer ranks
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
	Wand        engine.Wand        `json:"wand"`
	LifeTap     engine.LifeTap     `json:"lifeTap"`
}

// Evaluation is one stat delta to rate (an item swap, or a package prefix).
type Evaluation struct {
	Key   string             `json:"key"`
	Delta map[string]float64 `json:"delta"`
}

// Result is the metric change of one evaluation.
type Result struct {
	Value   float64 `json:"value"`
	Gain    float64 `json:"gain"`
	GainPct float64 `json:"gainPct"`
}

// Output is the engine's answer. Errors are reported in the payload (exit
// code 0), the same convention as app_bridge.py.
type Output struct {
	Engine          string            `json:"engine"`
	Baseline        *engine.Metrics   `json:"baseline,omitempty"`
	Spec            string            `json:"spec,omitempty"`
	SpellbookSource string            `json:"spellbookSource,omitempty"`
	Results         map[string]Result `json:"results,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// Supported reports whether the engine models a class yet.
func Supported(classFile string) bool {
	return classFile == "WARLOCK"
}

// DefaultMetric is the metric a class optimizes for by default.
func DefaultMetric(classFile string) string {
	switch classFile {
	case "WARLOCK":
		return warlock.DefaultMetric
	}
	return "dps"
}

// Run evaluates one input bundle.
func Run(in Input) Output {
	out := Output{Engine: Version}
	if !Supported(in.Character.ClassFile) {
		out.Error = fmt.Sprintf("class %s is not modeled yet", in.Character.ClassFile)
		return out
	}
	if in.Method == "montecarlo" {
		out.Error = "method montecarlo is not implemented yet (Stage 2)"
		return out
	}

	cat, err := catalog.Build(in.CacheDir, in.Build, in.Locale, in.Character.ClassFile)
	if err != nil {
		out.Error = err.Error()
		return out
	}

	ranks := map[int]int{}
	for id, rank := range in.Character.Talents {
		var n int
		if _, err := fmt.Sscanf(id, "%d", &n); err == nil {
			ranks[n] = rank
		}
	}
	mods, err := talents.Decode(cat, ranks)
	if err != nil {
		out.Error = err.Error()
		return out
	}

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
		metric = DefaultMetric(in.Character.ClassFile)
	}
	actions := warlock.Rotation(cat, known, in.Character.Level)

	baseline := engine.Evaluate(st, in.Fight, actions, metric)
	out.Baseline = &baseline

	out.Results = make(map[string]Result, len(in.Evaluations))
	for _, ev := range in.Evaluations {
		m := engine.Evaluate(st.ApplyDelta(ev.Delta), in.Fight, actions, metric)
		r := Result{Value: m.Value, Gain: m.Value - baseline.Value}
		if baseline.Value != 0 {
			r.GainPct = r.Gain / baseline.Value * 100
		}
		out.Results[ev.Key] = r
	}
	return out
}

func buildState(ch Character, mods engine.Mods) *engine.State {
	st := &engine.State{
		Level:      ch.Level,
		Healing:    ch.Stats.Healing,
		Hit:        ch.Stats.SpellHit,
		Intellect:  ch.Stats.Intellect,
		Spirit:     ch.Stats.Spirit,
		MP5:        ch.Stats.MP5,
		HP5:        ch.Stats.HP5,
		HealthMax:  ch.Stats.HealthMax,
		ManaMax:    ch.Stats.ManaMax,
		CritPerInt: ch.Stats.CritPerInt,
		ManaPerInt: ch.Stats.ManaPerInt,
		HPPerSta:   ch.Stats.HPPerSta,
		HastePct:   ch.Stats.HastePct,
		RangedCrit: ch.Stats.RangedCrit,
		Wand:       ch.Stats.Wand,
		LifeTap:    ch.Stats.LifeTap,
		Mods:       mods,
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
