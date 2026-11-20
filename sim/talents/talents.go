// Package talents turns the character's talent ranks into engine modifiers
// and says, per talent, what the model does with it.
//
// The generic path decodes the talent spell's own aura effects from the DB2:
// aura 79 (percent damage done by school), 72 (mana cost by school), 107/108
// (flat/percent spell modifiers with a class-mask filter), 134 (mana
// regeneration while casting) and a few more. The value of each effect at
// the character's rank comes from the trait tables (catalog.TalentValue):
// Forever has one spell per talent, and its SpellEffect row holds one rank
// only. Auras the character sheet already includes (hit, crit, attributes)
// are not applied again — the snapshot carries them. What the generic path
// cannot express comes from the embedded overrides file, every entry with a
// source note.
package talents

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/data"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

// Aura numbers of the client the decoder knows.
const (
	auraModThreat          = 10
	auraModDamageDone      = 13
	auraModResistance      = 22
	auraModStat            = 29
	auraModHealth          = 34
	auraModPower           = 35
	auraProcTriggerSpell   = 42
	auraModCrit            = 52
	auraModSpellHit        = 54
	auraModHit             = 55
	auraModSpellCrit       = 57
	auraModSpellCritSchool = 71
	auraModCostSchoolPct   = 72
	auraModDamagePctDone   = 79
	auraModPowerRegen      = 85
	auraModDamagePctTaken  = 87
	auraAddFlatModifier    = 107
	auraAddPctModifier     = 108
	auraModManaRegenCast   = 134
	auraModHealingDone     = 135
	auraModTotalStatPct    = 137
	auraSpellDamageOfStat  = 174
	auraSpellHealingOfStat = 175
)

// What the model does with a talent or a spell.
const (
	StatusUsed      = "used"      // changes the number
	StatusSheet     = "sheet"     // already part of the character sheet's numbers
	StatusIdle      = "idle"      // modeled, but its spells are not in the best rotation
	StatusUnmodeled = "unmodeled" // would matter, the model does not have it
	StatusNone      = "none"      // cannot change the metric
)

// rank orders the statuses: a talent takes the best one of its effects.
var statusRank = map[string]int{StatusUsed: 5, StatusSheet: 4, StatusIdle: 3, StatusUnmodeled: 2, StatusNone: 1, "": 0}

// Better reports whether status a says more than status b.
func Better(a, b string) bool { return statusRank[a] > statusRank[b] }

// Info is what the decoder did with one talent. Spell modifiers and rules
// that name spells are judged later against the rotation (run.go): Mods and
// Bases say which spells the talent works on.
type Info struct {
	SpellID    int
	Rank       int
	Status     string // best status of the effects that do not depend on the rotation
	Reason     string
	Unverified bool   // the value or the mechanic is an assumption, not measured in Forever
	Note       string // the override's source note
	Exact      bool   // every value came from the trait tables
	Mods       []engine.SpellMod
	Bases      map[int]bool // base ids of the spells a rule changes
}

func (i *Info) set(status, reason string) {
	if Better(status, i.Status) {
		i.Status, i.Reason = status, reason
	}
}

type override struct {
	Name         string   `json:"name"`
	Spells       []int    `json:"spells"`
	Kind         string   `json:"kind"`
	Effect       *int     `json:"effect"`  // effect index whose per-rank value the rule uses
	PerRank      float64  `json:"perRank"` // value per rank when the tables have none
	Value        float64  `json:"value"`   // fixed value, whatever the rank
	Schools      []string `json:"schools"`
	AffectsBases []int    `json:"affectsBases"`
	CountsBases  []int    `json:"countsBases"`
	Max          int      `json:"max"`
	Generic      bool     `json:"generic"` // also decode the spell's auras
	Reason       string   `json:"reason"`  // kind "ignore": why
	Unverified   bool     `json:"unverified"`
	Source       string   `json:"source"`
}

type overridesFile struct {
	Entries []override `json:"entries"`
}

func loadOverrides() (map[int]override, error) {
	var f overridesFile
	if err := json.Unmarshal(data.Overrides, &f); err != nil {
		return nil, fmt.Errorf("overrides.json: %w", err)
	}
	byID := map[int]override{}
	for _, e := range f.Entries {
		for _, id := range e.Spells {
			byID[id] = e
		}
	}
	return byID, nil
}

// Decode builds the engine modifiers from the talent ranks the addon
// exported (talent spell id -> points in it) and reports per talent what
// became of it, ordered by spell id.
func Decode(cat *catalog.Catalog, ranks map[int]int) (engine.Mods, []Info, error) {
	var mods engine.Mods
	over, err := loadOverrides()
	if err != nil {
		return mods, nil, err
	}
	ids := make([]int, 0, len(ranks))
	for id, rank := range ranks {
		if rank > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	var infos []Info
	for _, id := range ids {
		rank := ranks[id]
		info := Info{SpellID: id, Rank: rank, Exact: true}
		o, hasRule := over[id]
		if hasRule {
			applyRule(cat, &mods, &info, o)
		}
		if !hasRule || o.Generic {
			for _, a := range cat.Auras[id] {
				v, exact := cat.TalentValue(id, a.Index, rank, a.BP)
				if !exact {
					info.Exact = false
				}
				decodeAura(&mods, &info, a, v)
			}
		}
		if info.Status == "" && len(info.Mods) == 0 && len(info.Bases) == 0 {
			info.set(StatusUnmodeled, "unknown")
		}
		infos = append(infos, info)
	}
	return mods, infos, nil
}

// decodeAura applies one aura effect of a talent with its value at the rank.
func decodeAura(mods *engine.Mods, info *Info, a catalog.Aura, v float64) {
	switch a.Aura {
	case auraModDamagePctDone:
		for s := engine.Physical; s < engine.NumSchools; s++ {
			if a.Misc0&(1<<uint(s)) != 0 {
				mods.SchoolDamagePct[s] += v
			}
		}
		info.set(StatusUsed, "school")
	case auraModCostSchoolPct:
		for s := engine.Physical; s < engine.NumSchools; s++ {
			if a.Misc0&(1<<uint(s)) != 0 {
				mods.SchoolCostPct[s] += v
			}
		}
		info.set(StatusUsed, "cost")
	case auraAddFlatModifier, auraAddPctModifier:
		op := engine.SpellModOp(a.Misc0)
		switch {
		case engine.Understood(op):
			m := engine.SpellMod{Op: op, Pct: a.Aura == auraAddPctModifier, Value: v, Mask: a.Mask}
			mods.SpellMods = append(mods.SpellMods, m)
			info.Mods = append(info.Mods, m)
		case op == engine.OpRange || op == engine.OpPushback:
			info.set(StatusNone, "utility")
		default:
			info.set(StatusUnmodeled, "unknown")
		}
	case auraModManaRegenCast:
		mods.CastingRegenPct += v
		info.set(StatusUsed, "regen")
	case auraModTotalStatPct:
		// MiscValue_1 is the attribute mask: bit 2 stamina, 3 intellect, 4 spirit.
		if a.Misc1&(1<<2) != 0 {
			mods.StaminaPct += v
		}
		if a.Misc1&(1<<3) != 0 {
			mods.IntellectPct += v
		}
		if a.Misc1&(1<<4) != 0 {
			mods.SpiritPct += v
		}
		info.set(StatusSheet, "sheet")
	case auraSpellDamageOfStat:
		if a.Misc1 == 4 { // spirit
			mods.SPPerSpirit += v / 100
		}
		info.set(StatusSheet, "sheet")
	case auraSpellHealingOfStat:
		if a.Misc0 == 4 { // spirit
			mods.HealPerSpirit += v / 100
		}
		info.set(StatusSheet, "sheet")
	case auraModDamageDone, auraModStat, auraModHealth, auraModPower, auraModCrit, auraModSpellHit, auraModHit,
		auraModSpellCrit, auraModSpellCritSchool, auraModPowerRegen, auraModHealingDone:
		info.set(StatusSheet, "sheet")
	case auraModThreat, auraModResistance, auraModDamagePctTaken:
		info.set(StatusNone, "utility")
	case auraProcTriggerSpell:
		info.set(StatusUnmodeled, "situational")
	default:
		info.set(StatusUnmodeled, "unknown")
	}
}

// applyRule applies a hand-written rule from overrides.json.
func applyRule(cat *catalog.Catalog, mods *engine.Mods, info *Info, o override) {
	info.Note, info.Unverified = o.Source, o.Unverified
	v := o.PerRank * float64(info.Rank)
	switch {
	case o.Effect != nil:
		bp := v
		if o.PerRank == 0 {
			for _, a := range cat.Auras[info.SpellID] {
				if a.Index == *o.Effect {
					bp = a.BP
				}
			}
		}
		var exact bool
		if v, exact = cat.TalentValue(info.SpellID, *o.Effect, info.Rank, bp); !exact && o.PerRank != 0 {
			info.Exact = false
		}
	case o.Value != 0:
		v = o.Value
	}
	schools := func(apply func(engine.School)) {
		for _, name := range o.Schools {
			if s := engine.SchoolByName(name); s >= 0 {
				apply(s)
			}
		}
	}
	bases := func(ids []int) map[int]bool {
		m := map[int]bool{}
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	switch o.Kind {
	case "life_tap_pct":
		mods.LifeTapPct += v
		info.set(StatusUsed, "rule")
	case "dot_crit":
		schools(func(s engine.School) { mods.DotCritSchools |= 1 << uint(s) })
		info.set(StatusUsed, "rule")
	case "healing_pct":
		mods.HealingPct += v
		info.set(StatusUsed, "rule")
	case "school_damage_pct":
		schools(func(s engine.School) { mods.SchoolDamagePct[s] += v })
		info.set(StatusUsed, "rule")
	case "wand_pct":
		mods.WandPct += v
		info.set(StatusUsed, "wand")
	case "dot_stack":
		info.Bases = bases(o.AffectsBases)
		mods.DotStacks = append(mods.DotStacks, engine.DotStack{
			PctPerDot: v, Max: o.Max, Affects: info.Bases, Counts: bases(o.CountsBases),
		})
	case "channel_dot_haste":
		channel := info.SpellID
		if base, ok := cat.BaseOf[channel]; ok {
			channel = base
		}
		info.Bases = map[int]bool{channel: true}
		mods.ChannelHastes = append(mods.ChannelHastes, engine.ChannelHaste{
			Channel: channel, Pct: v, Affects: bases(o.AffectsBases),
		})
	case "kill_proc":
		// The talent's value is the chance; the buff it triggers says what it
		// gives and for how long. Whether it counts depends on the kills the
		// fight assumes, which run.go knows.
		proc := engine.KillProc{ChancePct: v}
		for _, a := range cat.Auras[info.SpellID] {
			if a.Aura != auraProcTriggerSpell || (o.Effect != nil && a.Index != *o.Effect) {
				continue
			}
			proc.DurationSec = float64(cat.DurationMs[a.Trigger]) / 1000
			for _, b := range cat.Auras[a.Trigger] {
				switch {
				case b.Aura == auraModTotalStatPct && b.Misc1&(1<<4) != 0:
					proc.SpiritPct += b.BP
				case b.Aura == auraModManaRegenCast:
					proc.CastingRegenPct += b.BP
				}
			}
		}
		if proc.DurationSec > 0 && (proc.SpiritPct != 0 || proc.CastingRegenPct != 0) {
			mods.KillProcs = append(mods.KillProcs, proc)
			info.set(StatusUsed, "kills")
		} else {
			info.set(StatusUnmodeled, "situational")
		}
	case "generic":
		// Only the flags of the entry count; the auras are decoded.
	case "ignore":
		reason := o.Reason
		if reason == "" {
			reason = "situational"
		}
		info.set(StatusUnmodeled, reason)
	}
}
