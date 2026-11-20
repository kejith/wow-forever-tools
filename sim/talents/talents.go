// Package talents turns the character's talent ranks into engine modifiers.
//
// The generic path decodes the talent spell's own aura effects from the DB2:
// aura 79 (percent damage done by school) and auras 107/108 (flat/percent
// spell modifiers with a class-mask filter). Auras the character sheet
// already includes (flat stats, flat bonus damage, spell crit) are skipped
// on purpose — the snapshot carries them, decoding them again would count
// them twice. What the generic path cannot express comes from the embedded
// overrides file, every entry with a source note.
package talents

import (
	"encoding/json"
	"fmt"

	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/data"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

const (
	auraModDamagePctDone = 79
	auraAddFlatModifier  = 107
	auraAddPctModifier   = 108
)

type override struct {
	Name         string   `json:"name"`
	Spells       []int    `json:"spells"`
	Kind         string   `json:"kind"`
	PerRank      float64  `json:"perRank"`
	Schools      []string `json:"schools"`
	AffectsBases []int    `json:"affectsBases"`
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
// exported (rank-spell id -> points in it).
func Decode(cat *catalog.Catalog, ranks map[int]int) (engine.Mods, error) {
	mods := engine.Mods{HitFlatByBase: map[int]float64{}}
	over, err := loadOverrides()
	if err != nil {
		return mods, err
	}
	for id, rank := range ranks {
		if rank <= 0 {
			continue
		}
		if o, ok := over[id]; ok {
			apply(&mods, o, rank)
			continue
		}
		for _, a := range cat.Auras[id] {
			switch a.Aura {
			case auraModDamagePctDone:
				for s := engine.Physical; s < engine.NumSchools; s++ {
					if a.Misc0&(1<<uint(s)) != 0 {
						mods.SchoolDamagePct[s] += a.BP
					}
				}
			case auraAddFlatModifier, auraAddPctModifier:
				mods.SpellMods = append(mods.SpellMods, engine.SpellMod{
					Op:    engine.SpellModOp(a.Misc0),
					Pct:   a.Aura == auraAddPctModifier,
					Value: a.BP,
					Mask:  a.Mask,
				})
			}
		}
	}
	return mods, nil
}

func apply(mods *engine.Mods, o override, rank int) {
	v := o.PerRank * float64(rank)
	switch o.Kind {
	case "spell_hit_flat":
		for _, base := range o.AffectsBases {
			mods.HitFlatByBase[base] += v
		}
	case "life_tap_pct":
		mods.LifeTapPct += v
	case "dot_crit":
		for _, name := range o.Schools {
			if s := engine.SchoolByName(name); s >= 0 {
				mods.DotCritSchools |= 1 << uint(s)
			}
		}
	case "healing_pct":
		mods.HealingPct += v
	case "school_damage_pct":
		for _, name := range o.Schools {
			if s := engine.SchoolByName(name); s >= 0 {
				mods.SchoolDamagePct[s] += v
			}
		}
	case "casting_regen_pct":
		mods.CastingRegenPct += v
	case "wand_pct":
		mods.WandPct += v
	case "sp_per_spirit":
		mods.SPPerSpirit += v
	}
}
