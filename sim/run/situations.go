package run

import (
	"sort"

	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/classes/priest"
	"github.com/kejith/wow-forever-tools/sim/classes/warlock"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

// The rotation guide: the same character in several kinds of fights. Which
// spells pay depends on the fight (a DoT that outlives the enemy does not,
// a long fight is decided by mana and health, not by the pools), so every
// situation gets its own best rotation, played once cast by cast at the
// expectation of every cast.

// Step is one line of a situation's priority list, with what it did in the
// played fight.
type Step struct {
	Kind     string  `json:"kind"` // debuff, dot, hot, cooldown, filler, heal, recover, lifetap, mana, wand
	SpellID  int     `json:"spellId,omitempty"`
	Name     string  `json:"name,omitempty"`
	Rank     int     `json:"rank,omitempty"`
	Casts    float64 `json:"casts"` // in the fight; a healer's plan: until the mana is gone (conversions only)
	PerMin   float64 `json:"perMin"`
	FirstSec float64 `json:"firstSec"` // when it was first cast (played fights)
	SharePct float64 `json:"sharePct"` // share of the fight time spent casting it
	Value    float64 `json:"value"`    // damage or healing per second it did
	// A healer's plan: healing of one cast, the mana per second it costs, and
	// on how many targets a HoT or a shield is kept.
	PerCast float64 `json:"perCast,omitempty"`
	ManaPS  float64 `json:"manaPerSec,omitempty"`
	Targets float64 `json:"targets,omitempty"`
	// A conversion (Dark Sacrifice): the mana one cast returns and the
	// health it costs.
	ManaGain   float64 `json:"manaGain,omitempty"`
	HealthCost float64 `json:"healthCost,omitempty"`
	// A debuff: how much more damage the enemy takes under it, in percent.
	TakenPct float64 `json:"takenPct,omitempty"`
}

// Skipped is a spell the character knows that the best rotation of a
// situation leaves out, with what casting it would cost.
type Skipped struct {
	Kind    string  `json:"kind"`
	SpellID int     `json:"spellId"`
	Name    string  `json:"name"`
	Rank    int     `json:"rank,omitempty"`
	Loss    float64 `json:"loss"` // the metric lost with the best rotation that casts it
}

// TimelineEntry is one action of the played fight; N counts how often it
// was repeated in a row.
type TimelineEntry struct {
	T       float64 `json:"t"`
	Kind    string  `json:"kind"`
	SpellID int     `json:"spellId,omitempty"`
	N       int     `json:"n"`
	ManaPct float64 `json:"manaPct"` // before the action
	HPPct   float64 `json:"hpPct"`
}

// Situation is one kind of fight with the rotation that does best in it.
type Situation struct {
	Key        string          `json:"key"`
	Fight      engine.Fight    `json:"fight"`
	Metric     string          `json:"metric"`
	Value      float64         `json:"value"`
	OOMSec     *float64        `json:"oomSeconds"` // when the mana first ran out with nothing to convert; nil = never
	Steps      []Step          `json:"steps"`
	Skipped    []Skipped       `json:"skipped,omitempty"`
	Timeline   []TimelineEntry `json:"timeline,omitempty"`
	More       int             `json:"more,omitempty"` // actions after the listed timeline
	Candidates int             `json:"candidates"`
	// EndManaPct and EndHPPct are what is left when a played fight ends.
	EndManaPct float64 `json:"endManaPct"`
	EndHPPct   float64 `json:"endHpPct"`
	// A healer's situation is a demand: so much healing per second on so
	// many targets (DemandPct of the most the character can heal there,
	// MaxValue). Value is what the plan supplies, with its mana balance.
	Demand *Demand `json:"demand,omitempty"`
}

// Demand is the healing a healer's situation asks for and the mana balance
// of the plan that supplies it.
type Demand struct {
	Targets  int     `json:"targets"`
	Pct      float64 `json:"pct"`      // share of MaxValue asked for
	Value    float64 `json:"value"`    // healing per second asked for
	MaxValue float64 `json:"maxValue"` // the most healing per second on these targets, whatever the mana
	ManaPS   float64 `json:"manaPerSec"`
	RegenPS  float64 `json:"regenPerSec"`
	CastPct  float64 `json:"castPct"` // share of the time spent casting
	FreePct  float64 `json:"freePct"` // share of the time outside the five-second rule
	ManaMax  float64 `json:"manaMax"`
}

const timelineEntries = 40

// damageFights are the fights a damage dealer's guide covers. The standard
// one is the fight the gear is rated for.
func damageFights(standard engine.Fight) []struct {
	key   string
	fight engine.Fight
} {
	f := func(seconds float64, delta int) engine.Fight {
		return engine.Fight{Seconds: seconds, TargetDelta: delta, KillsPerMinute: -1}
	}
	return []struct {
		key   string
		fight engine.Fight
	}{
		{"quick", f(15, standard.TargetDelta)},
		{"standard", engine.Fight{Seconds: standard.Seconds, TargetDelta: standard.TargetDelta, KillsPerMinute: -1}},
		{"boss", f(180, 3)},
		{"endurance", f(600, standard.TargetDelta)},
	}
}

// resolveKills turns automatic kills into a number: a damage dealer against
// normal enemies lands one killing blow per fight, which is when buffs like
// Spirit Tap start; a boss fight has none, and a healer has none.
func resolveKills(fight engine.Fight, metric string) engine.Fight {
	if fight.KillsPerMinute < 0 {
		fight.KillsPerMinute = 0
		if metric == "dps" && fight.TargetDelta < 3 {
			fight.KillsPerMinute = 60 / fight.Seconds
		}
	}
	return fight
}

func choices(classFile string, cat *catalog.Catalog, known map[int]bool, level int) engine.Choices {
	switch classFile {
	case "WARLOCK":
		return warlock.Choices(cat, known, level)
	case "PRIEST":
		return priest.Choices(cat, known, level)
	}
	return engine.Choices{}
}

// damageSituations plays every candidate rotation in every fight and keeps
// the best one of each.
func damageSituations(classFile string, cat *catalog.Catalog, known map[int]bool, st *engine.State, standard engine.Fight, tapSpell *engine.Spell) []Situation {
	rots := choices(classFile, cat, known, st.Level).Rotations()
	if len(rots) == 0 {
		return nil
	}
	var out []Situation
	for _, df := range damageFights(standard) {
		fight := resolveKills(df.fight, "dps")
		scores := make([]float64, len(rots))
		best := 0
		for i, r := range rots {
			scores[i] = engine.Play(st, fight, r, "dps").Value
			if scores[i] > scores[best] {
				best = i
			}
		}
		pb := engine.Play(st, fight, rots[best], "dps")
		s := Situation{Key: df.key, Fight: fight, Metric: "dps", Value: pb.Value, OOMSec: pb.OOMSec, Candidates: len(rots)}
		s.Steps = steps(cat, rots[best], pb, fight, tapSpell)
		s.Timeline, s.More = timeline(pb, st)
		if n := len(pb.Events); n > 0 {
			// The state before the last action is the closest the record has.
			s.EndManaPct, s.EndHPPct = pct(pb.Events[n-1].Mana, st.ManaMax), pct(pb.Events[n-1].Health, st.HealthMax)
		}

		// What the rotation leaves out, and what casting it anyway would cost.
		in := map[int]bool{}
		for _, a := range rots[best] {
			if a.Spell != nil {
				in[a.Spell.ID] = true
			}
		}
		with := map[int]float64{}
		kinds := map[int]string{}
		for i, r := range rots {
			for _, a := range r {
				if a.Spell == nil || in[a.Spell.ID] || a.Kind == "mana" || a.Kind == "recover" {
					continue
				}
				if old, ok := with[a.Spell.ID]; !ok || scores[i] > old {
					with[a.Spell.ID] = scores[i]
				}
				kinds[a.Spell.ID] = a.Kind
			}
		}
		for id, score := range with {
			sp := cat.Spells[id]
			s.Skipped = append(s.Skipped, Skipped{Kind: kinds[id], SpellID: id, Name: sp.Name, Rank: cat.RankNumber(id),
				Loss: scores[best] - score})
		}
		sort.Slice(s.Skipped, func(i, j int) bool {
			if s.Skipped[i].Loss != s.Skipped[j].Loss {
				return s.Skipped[i].Loss < s.Skipped[j].Loss
			}
			return s.Skipped[i].SpellID < s.Skipped[j].SpellID
		})
		out = append(out, s)
	}
	return out
}

func pct(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return part / whole * 100
}

// steps is the rotation as a priority list: the DoTs, the cooldowns, the
// filler, then what keeps it going: the conversion, the spell that returns
// health when the conversion is blocked, the wand. A line that was never
// cast in this fight is left out.
func steps(cat *catalog.Catalog, actions []engine.Action, pb engine.Playback, fight engine.Fight, tapSpell *engine.Spell) []Step {
	type key struct {
		kind string
		id   int
	}
	casts := map[key]int{}
	first := map[key]float64{}
	spent := map[key]float64{}
	for i, e := range pb.Events {
		k := key{e.Kind, e.SpellID}
		if casts[k] == 0 {
			first[k] = e.T
		}
		casts[k]++
		end := fight.Seconds
		if i+1 < len(pb.Events) {
			end = pb.Events[i+1].T
		}
		if end > fight.Seconds {
			end = fight.Seconds
		}
		if end > e.T {
			spent[k] += end - e.T
		}
	}
	line := func(kind string, sp *engine.Spell, eventID int) (Step, bool) {
		k := key{kind, eventID}
		if casts[k] == 0 {
			return Step{}, false
		}
		s := Step{Kind: kind, Casts: float64(casts[k]), PerMin: float64(casts[k]) / fight.Seconds * 60, FirstSec: first[k],
			SharePct: spent[k] / fight.Seconds * 100}
		if sp != nil {
			s.SpellID, s.Name, s.Rank = sp.ID, sp.Name, cat.RankNumber(sp.ID)
			if kind != "lifetap" {
				s.Value = pb.BySpell[sp.ID] / fight.Seconds
			}
		} else if kind == "wand" {
			s.Value = pb.BySpell[0] / fight.Seconds
		}
		return s, true
	}
	var out []Step
	add := func(kind string, sp *engine.Spell, eventID int) {
		if s, ok := line(kind, sp, eventID); ok {
			out = append(out, s)
		}
	}
	byKind := func(kinds ...string) {
		for _, kind := range kinds {
			for _, a := range actions {
				if a.Kind == kind && a.Spell != nil {
					if s, ok := line(kind, a.Spell, a.Spell.ID); ok {
						s.TakenPct = a.TakenPct
						out = append(out, s)
					}
				}
			}
		}
	}
	byKind("debuff", "dot", "hot", "cooldown", "filler", "heal")
	add("lifetap", tapSpell, 0)
	byKind("mana", "recover")
	add("wand", nil, 0)
	return out
}

// timeline is the start of the played fight, repeats folded into one entry.
func timeline(pb engine.Playback, st *engine.State) ([]TimelineEntry, int) {
	var out []TimelineEntry
	more := 0
	for _, e := range pb.Events {
		if n := len(out); n > 0 && out[n-1].Kind == e.Kind && out[n-1].SpellID == e.SpellID && more == 0 {
			out[n-1].N++
			continue
		}
		if len(out) == timelineEntries {
			more++
			continue
		}
		out = append(out, TimelineEntry{T: e.T, Kind: e.Kind, SpellID: e.SpellID, N: 1,
			ManaPct: pct(e.Mana, st.ManaMax), HPPct: pct(e.Health, st.HealthMax)})
	}
	return out, more
}

// healDemands are the situations of a healer's guide: how much of the most
// the character can heal is asked for, and on how many targets.
var healDemands = []struct {
	key     string
	targets int
	pct     float64
}{
	{"light", 1, 25},
	{"steady", 1, 50},
	{"heavy", 1, 75},
	{"burst", 1, 100},
	{"group", groupTargets, 50},
}

const groupTargets = 5

// healSituations plans every level of demand: the spells that supply it for
// the longest time, how long that is, and what it costs.
func healSituations(classFile string, cat *catalog.Catalog, known map[int]bool, st *engine.State, standard engine.Fight) []Situation {
	if classFile != "PRIEST" {
		return nil
	}
	fight := resolveKills(standard, "hps")
	var out []Situation
	for _, d := range healDemands {
		sources, convert := priest.HealSources(cat, known, st.Level, d.targets)
		max := engine.MaxHealing(st, sources)
		if max <= 0 {
			return nil
		}
		plan := engine.PlanDemand(st, fight, sources, max*d.pct/100, convert)
		s := Situation{Key: d.key, Fight: fight, Metric: "hps", Value: plan.HPS, OOMSec: plan.OOMSec, Candidates: plan.Candidates,
			Demand: &Demand{Targets: d.targets, Pct: d.pct, Value: plan.Demand, MaxValue: max, ManaPS: plan.ManaPS,
				RegenPS: plan.RegenPS, CastPct: plan.CastPct, FreePct: plan.FreePct, ManaMax: st.ManaMax}}
		for _, l := range plan.Lines {
			sp := l.Action.Spell
			s.Steps = append(s.Steps, Step{Kind: l.Action.Kind, SpellID: sp.ID, Name: sp.Name, Rank: cat.RankNumber(sp.ID),
				PerMin: l.PerMin, SharePct: l.SharePct, Value: l.HPS, PerCast: l.PerCast, ManaPS: l.ManaPS, Targets: l.Targets})
		}
		// The spells kept up come first, then the heals, the one with the
		// least mana per point of healing in front.
		price := func(st Step) float64 { return st.ManaPS / st.Value }
		sort.SliceStable(s.Steps, func(i, j int) bool {
			a, b := s.Steps[i], s.Steps[j]
			if (a.Kind == "heal") != (b.Kind == "heal") {
				return b.Kind == "heal"
			}
			return price(a) < price(b)
		})
		if sp := plan.ConversionSpell; sp != nil {
			s.Steps = append(s.Steps, Step{Kind: "mana", SpellID: sp.ID, Name: sp.Name, Rank: cat.RankNumber(sp.ID),
				Casts: plan.Conversions, PerMin: plan.ConversionsPerMin, ManaGain: plan.ConversionMana, HealthCost: plan.ConversionHealth})
		}
		out = append(out, s)
	}
	return out
}
