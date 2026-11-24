package run

import (
	"sort"

	"github.com/kejith/wow-tools-forever/sim/catalog"
	"github.com/kejith/wow-tools-forever/sim/engine"
)

// The damage table: every known rank of every spell that deals damage with
// what one cast does and costs against the enemy of the fight. Like the
// heal table it is what the character can cast, outside any rotation: no
// DoT of the character and no debuff is on the enemy. The rotation guide
// (situations.go) says which of these spells to cast when.

// DamageRow is one known rank of a damage spell.
type DamageRow struct {
	SpellID int    `json:"spellId"`
	BaseID  int    `json:"baseId"`
	Name    string `json:"name"`
	Rank    int    `json:"rank,omitempty"`
	Level   int    `json:"level"`  // level the rank is learned at
	School  string `json:"school"` // "shadow", "fire", ...
	// Kind: "direct", "dot", "both" (a hit with a periodic part) or
	// "channel" (the ticks land while the spell is cast).
	Kind string `json:"kind"`
	// A direct hit without a crit, as the tooltip shows it.
	Min float64 `json:"min,omitempty"`
	Max float64 `json:"max,omitempty"`
	// The periodic part: one tick without a crit, how many, over how long.
	PerTick     float64 `json:"perTick,omitempty"`
	Ticks       float64 `json:"ticks,omitempty"`
	DurationSec float64 `json:"durationSec,omitempty"`
	CritPct     float64 `json:"critPct"`             // crit chance of the spell
	TickCrits   bool    `json:"tickCrits,omitempty"` // the ticks crit as well
	HitPct      float64 `json:"hitPct"`              // chance to land on the enemy of the fight
	// PerCast is what one cast does on average: misses and crits weighed in,
	// all ticks counted, on one enemy.
	PerCast     float64 `json:"perCast"`
	Mana        float64 `json:"mana"`
	CastSec     float64 `json:"castSec"`    // cast time or channel length; 0 for an instant
	ExecuteSec  float64 `json:"executeSec"` // time one cast occupies (cast time or global cooldown)
	CooldownSec float64 `json:"cooldownSec,omitempty"`
	// DPS is PerCast per second of casting, DPM per point of mana.
	DPS float64 `json:"dps"`
	DPM float64 `json:"dpm"`
	// Bonus is the part of PerCast that comes from spell power, CoefPct that
	// part per point of spell power, in percent: the spell's coefficient
	// with talents, crits and misses in it.
	Bonus   float64 `json:"bonus"`
	CoefPct float64 `json:"coefPct"`
	// Leech is the health one cast returns, SelfDamage the health a whole
	// channel costs its caster (Hellfire).
	Leech      float64 `json:"leech,omitempty"`
	SelfDamage float64 `json:"selfDamage,omitempty"`
	Area       bool    `json:"area,omitempty"` // hits every enemy
	Top        bool    `json:"top,omitempty"`  // the highest known rank of its spell
}

// SchoolPower is the character's spell power and crit chance in one school.
type SchoolPower struct {
	School  string  `json:"school"`
	Power   float64 `json:"power"`
	CritPct float64 `json:"critPct"`
}

// DamageTable is the rows and the character's numbers they were computed with.
type DamageTable struct {
	Rows []DamageRow `json:"rows"`
	// Schools the rows use, the one with the most spell power first.
	Schools     []SchoolPower `json:"schools"`
	TargetDelta int           `json:"targetDelta"` // enemy levels above the character
	MissPct     float64       `json:"missPct"`     // chance of a spell without a hit talent of its own to miss
	ManaMax     float64       `json:"manaMax"`
	HealthMax   float64       `json:"healthMax"`
	// Mana per second while casting and outside the five-second rule.
	RegenCastingPS float64 `json:"regenCastingPerSec"`
	RegenIdlePS    float64 `json:"regenIdlePerSec"`
	WandDPS        float64 `json:"wandDps,omitempty"` // misses and crits weighed in
	// Life Tap as the character casts it: mana with the talents, health.
	TapMana   float64 `json:"tapMana,omitempty"`
	TapHealth float64 `json:"tapHealth,omitempty"`
}

// damageTable lists every known rank of a damage spell.
func damageTable(cat *catalog.Catalog, known map[int]bool, st *engine.State, fight engine.Fight) *DamageTable {
	casting, idle := st.Regen(fight)
	tgt := engine.NewTarget(st.Level, fight.TargetDelta)
	t := &DamageTable{TargetDelta: fight.TargetDelta, ManaMax: st.ManaMax, HealthMax: st.HealthMax,
		RegenCastingPS: casting, RegenIdlePS: idle, WandDPS: engine.WandDPS(st, tgt), MissPct: -1}
	if mana := st.LifeTap.ManaPerCast * (1 + st.Mods.LifeTapPct/100); mana > 0 {
		t.TapMana, t.TapHealth = mana, st.LifeTap.HealthPerCast
	}
	plain := *st // the same character without spell power
	plain.SP = [engine.NumSchools]float64{}
	firstLevel := map[int]int{} // chain -> level of its first known rank, for the order
	topLevel := map[int]int{}
	used := map[engine.School]bool{}
	for id := range known {
		sp := cat.Spells[id]
		if sp == nil || sp.Passive || sp.Heals() || (sp.Direct == nil && sp.Periodic == nil) {
			continue
		}
		sh := engine.Shape(sp, st, tgt)
		cast := engine.ResolveCast(sp, st, tgt, nil)
		if cast.Damage <= 0 {
			continue
		}
		r := DamageRow{SpellID: id, BaseID: sp.BaseID, Name: sp.Name, Rank: cat.RankNumber(id), Level: sp.Levels.Spell,
			School: sp.School.Name(), Mana: sh.ManaCost, CastSec: sh.CastSec, ExecuteSec: sh.ExecuteSec,
			CooldownSec: sh.CooldownSec, CritPct: sh.CritPct, HitPct: sh.HitChance * 100, PerCast: cast.Damage,
			Leech: cast.Leech, Area: sp.Area}
		if sh.DirectAvg > 0 {
			r.Kind = "direct"
			r.Min = sh.DirectAvg * (1 - sh.DirectVariance/2)
			r.Max = sh.DirectAvg * (1 + sh.DirectVariance/2)
		}
		if sh.Ticks > 0 && sh.TickValue > 0 {
			r.PerTick, r.Ticks, r.DurationSec = sh.TickBase, sh.Ticks, sh.DurationSec
			r.TickCrits = sh.TickValue > sh.TickBase
			switch {
			case sp.Channeled:
				r.Kind = "channel"
			case r.Kind == "direct":
				r.Kind = "both"
			default:
				r.Kind = "dot"
			}
			r.SelfDamage = sh.SelfTick * sh.Ticks
		}
		if r.ExecuteSec > 0 {
			r.DPS = r.PerCast / r.ExecuteSec
		}
		if r.Mana > 0 {
			r.DPM = r.PerCast / r.Mana
		}
		r.Bonus = r.PerCast - engine.ResolveCast(sp, &plain, tgt, nil).Damage
		if power := st.SP[sp.School]; power > 0 {
			r.CoefPct = r.Bonus / power * 100
		}
		if lv, ok := firstLevel[sp.BaseID]; !ok || r.Level < lv {
			firstLevel[sp.BaseID] = r.Level
		}
		if r.Level > topLevel[sp.BaseID] {
			topLevel[sp.BaseID] = r.Level
		}
		used[sp.School] = true
		// The miss chance of the table is that of a spell no talent helps to
		// land: the highest of the rows.
		if miss := 100 - r.HitPct; miss > t.MissPct {
			t.MissPct = miss
		}
		t.Rows = append(t.Rows, r)
	}
	if t.MissPct < 0 {
		t.MissPct = 0
	}
	for i := range t.Rows {
		t.Rows[i].Top = t.Rows[i].Level == topLevel[t.Rows[i].BaseID]
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
	for sc := engine.School(0); sc < engine.NumSchools; sc++ {
		if used[sc] {
			t.Schools = append(t.Schools, SchoolPower{School: sc.Name(), Power: st.SP[sc], CritPct: st.Crit[sc]})
		}
	}
	sort.SliceStable(t.Schools, func(i, j int) bool { return t.Schools[i].Power > t.Schools[j].Power })
	return t
}
