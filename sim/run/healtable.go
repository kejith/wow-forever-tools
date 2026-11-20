package run

import (
	"math"
	"sort"

	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

// The heal table: every known rank of every heal and shield with what one
// cast gives and costs, the numbers healers compare ranks by. It is what
// the character can cast, not what a rotation casts: the rotation guide
// (situations.go) says which of these ranks to use when.

// HealRow is one known rank of a heal or a shield.
type HealRow struct {
	SpellID int    `json:"spellId"`
	BaseID  int    `json:"baseId"`
	Name    string `json:"name"`
	Rank    int    `json:"rank,omitempty"`
	Level   int    `json:"level"` // level the rank is learned at
	// Kind: "direct", "hot", "both" (a heal with a periodic part) or "shield".
	Kind string `json:"kind"`
	// A direct hit without a crit, as the tooltip shows it.
	Min float64 `json:"min,omitempty"`
	Max float64 `json:"max,omitempty"`
	// The periodic part: one tick, how many, over how long.
	PerTick     float64 `json:"perTick,omitempty"`
	Ticks       float64 `json:"ticks,omitempty"`
	DurationSec float64 `json:"durationSec,omitempty"`
	Absorb      float64 `json:"absorb,omitempty"`
	CritPct     float64 `json:"critPct,omitempty"` // chance of the direct part to crit
	// PerCast is what one cast heals on average: crits weighed in, all
	// ticks counted.
	PerCast     float64 `json:"perCast"`
	Mana        float64 `json:"mana"`
	CastSec     float64 `json:"castSec"`    // cast time; 0 for an instant
	ExecuteSec  float64 `json:"executeSec"` // time one cast occupies (cast time or global cooldown)
	CooldownSec float64 `json:"cooldownSec,omitempty"`
	// OnTargetSec is how long one cast occupies a target that can carry it
	// only once: a HoT's duration, a shield's lockout (Weakened Soul). 0 for
	// a heal that is cast back to back.
	OnTargetSec float64 `json:"onTargetSec,omitempty"`
	// HPS is PerCast per second of casting, HPM per point of mana.
	HPS float64 `json:"hps"`
	HPM float64 `json:"hpm"`
	// Bonus is the part of PerCast that comes from the healing bonus, and
	// CoefPct that part per point of healing bonus, in percent: the
	// spell's coefficient with talents, crits and the loss of an outgrown
	// rank in it.
	Bonus        float64 `json:"bonus"`
	CoefPct      float64 `json:"coefPct"`
	BonusLossPct float64 `json:"bonusLossPct,omitempty"`
}

// HealTable is the rows and the character's numbers they were computed with.
type HealTable struct {
	Rows    []HealRow `json:"rows"`
	Healing float64   `json:"healing"` // healing bonus
	CritPct float64   `json:"critPct"` // crit chance of holy spells
	ManaMax float64   `json:"manaMax"`
	Spirit  float64   `json:"spirit"`
	GearMP5 float64   `json:"gearMp5"` // mana per 5 s from gear: works all the time
	// Mana per second while casting (gear plus the share of the spirit
	// regeneration that talents let through) and outside the five-second rule.
	RegenCastingPS float64 `json:"regenCastingPerSec"`
	RegenIdlePS    float64 `json:"regenIdlePerSec"`
	// CastingRegenPct is that share of the spirit regeneration, in percent.
	CastingRegenPct float64 `json:"castingRegenPct"`
}

// perCast is the expected healing of one cast of a shape.
func perCast(sh engine.CastShape) float64 {
	return sh.SampleDirect(nil) + sh.Ticks*sh.TickValue
}

// healTable lists every known heal and shield rank. Spells without a mana
// cost that heal the caster only (potions the catalog may carry, racials)
// stay in: they are rows like any other. lockouts is per spell how long a
// target cannot take it again when that is more than its own cooldown.
func healTable(cat *catalog.Catalog, known map[int]bool, st *engine.State, fight engine.Fight, lockouts map[int]float64) *HealTable {
	casting, idle := st.Regen(fight)
	t := &HealTable{Healing: st.Healing, CritPct: st.Crit[engine.Holy], ManaMax: st.ManaMax, Spirit: st.Spirit,
		GearMP5: st.MP5, RegenCastingPS: casting, RegenIdlePS: idle, CastingRegenPct: st.Mods.CastingRegenPct}
	plain := *st // the same character without a healing bonus
	plain.Healing = 0
	tgt := engine.Target{}
	firstLevel := map[int]int{} // chain -> level of its first known rank, for the order
	for id := range known {
		sp := cat.Spells[id]
		if sp == nil || sp.Passive || !sp.Heals() || (sp.Direct == nil && sp.Periodic == nil && sp.Absorb == nil) {
			continue
		}
		sh := engine.Shape(sp, st, tgt)
		r := HealRow{SpellID: id, BaseID: sp.BaseID, Name: sp.Name, Rank: cat.RankNumber(id), Level: sp.Levels.Spell,
			Mana: sh.ManaCost, CastSec: sh.CastSec, ExecuteSec: sh.ExecuteSec, CooldownSec: sh.CooldownSec,
			PerCast: perCast(sh)}
		if r.PerCast <= 0 {
			continue
		}
		switch {
		case sp.Absorb != nil && sp.Direct == nil:
			r.Kind, r.Absorb = "shield", sh.DirectAvg
		case sh.DirectAvg > 0:
			r.Kind = "direct"
			r.Min = sh.DirectAvg * (1 - sh.DirectVariance/2)
			r.Max = sh.DirectAvg * (1 + sh.DirectVariance/2)
			r.CritPct = sh.CritPct
		}
		if sh.Ticks > 0 && sh.TickValue > 0 {
			r.PerTick, r.Ticks, r.DurationSec = sh.TickBase, sh.Ticks, sh.DurationSec
			if r.Kind == "direct" {
				r.Kind = "both"
			} else if r.Kind == "" {
				r.Kind = "hot"
			}
		}
		switch r.Kind {
		case "hot":
			r.OnTargetSec = sh.DurationSec
		case "shield":
			r.OnTargetSec = math.Max(sh.CooldownSec, lockouts[id])
		}
		if sp.IsHeal {
			r.BonusLossPct = bonusLossPct(st.Level, sp)
		}
		if r.ExecuteSec > 0 {
			r.HPS = r.PerCast / r.ExecuteSec
		}
		if r.Mana > 0 {
			r.HPM = r.PerCast / r.Mana
		}
		r.Bonus = r.PerCast - perCast(engine.Shape(sp, &plain, tgt))
		if st.Healing > 0 {
			r.CoefPct = r.Bonus / st.Healing * 100
		}
		if lv, ok := firstLevel[sp.BaseID]; !ok || r.Level < lv {
			firstLevel[sp.BaseID] = r.Level
		}
		t.Rows = append(t.Rows, r)
	}
	// Chains in the order a character learns them, ranks from low to high.
	sort.Slice(t.Rows, func(i, j int) bool {
		a, b := t.Rows[i], t.Rows[j]
		if a.BaseID != b.BaseID {
			if firstLevel[a.BaseID] != firstLevel[b.BaseID] {
				return firstLevel[a.BaseID] < firstLevel[b.BaseID]
			}
			return a.BaseID < b.BaseID
		}
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		return a.SpellID < b.SpellID
	})
	return t
}
