package upgrades

import (
	"fmt"
	"math"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
	"github.com/kejith/wow-tools-forever/sim/tools/enginebundle"
)

// MeleeMiss is the yellow-hit miss chance against enemies 0..3 levels above.
var MeleeMiss = map[int]float64{0: 5.0, 1: 5.5, 2: 6.0, 3: 9.0}

// SlotLabelFor returns the localized slot name for the character's class
// (slot 18 is Wand for cloth casters, Relic for Druid/Shaman/Paladin, Ranged for Warrior/Rogue/Hunter).
func SlotLabelFor(slot int64, classFile string) string {
	if slot == 18 {
		switch classFile {
		case "DRUID", "SHAMAN", "PALADIN":
			return i18n.T("Relic")
		case "WARRIOR", "ROGUE", "HUNTER":
			return i18n.T("Ranged")
		}
	}
	return SlotLabel(slot)
}

func buildClassScale(char *sv.Table, classFile, spec, role, profile string, gearHit, gearCrit float64) *Scale {
	ch := char.Tab("character")
	st := ch.Tab("stats")
	level := orNum(ch, "level", 60)
	p := Profiles[profile]
	dungeon := profile == "dungeon"

	row := func(label string, weight float64, note string) *pyjson.Obj {
		return pyjson.New("label", label, "weight", weight, "note", note)
	}
	fmtWeight := func(label string, w float64) string {
		if math.Abs(w-math.Round(w)) < 0.005 {
			return fmt.Sprintf("%s %.0f", label, w)
		}
		return fmt.Sprintf("%s %.2g", label, w)
	}

	switch role {
	case "healer":
		intW, spiW, mp5W, critW, staW := 0.45, 0.40, 1.4, 4.5, 0.25
		if dungeon {
			intW, spiW, mp5W, staW = 0.55, 0.45, 1.8, 0.15
		}
		if classFile == "PALADIN" {
			spiW, critW, mp5W = 0.10, 8.0, 1.5
		} else if classFile == "SHAMAN" {
			spiW, mp5W = 0.15, 2.0
		}
		s := &Scale{
			Profile: profile, Label: i18n.T(p.Label),
			Weights: []Weight{
				{"healing", 1.0}, {"intellect", intW}, {"spirit", spiW},
				{"mp5", mp5W}, {"crit", critW}, {"stamina", staW},
			},
		}
		if classFile == "PRIEST" {
			wandW := 0.25
			if dungeon {
				wandW = 0.05
			}
			s.Weights = append(s.Weights, Weight{"wand_dps", wandW})
		}
		s.Rows = []*pyjson.Obj{
			row(i18n.T("Healing"), 1.0, i18n.T("reference")),
			row(i18n.T("Intellect"), intW, ""),
			row(i18n.T("Spirit"), spiW, ""),
			row(i18n.T("Mana per 5 sec."), mp5W, ""),
			row(i18n.T("Crit, per 1%"), critW, ""),
			row(i18n.T("Stamina"), staW, ""),
		}
		s.Text = strings.Join([]string{
			fmtWeight(i18n.T("Healing"), 1.0),
			fmtWeight(i18n.T("Int"), intW),
			fmtWeight(i18n.T("Spi"), spiW),
			fmtWeight(i18n.T("Mana/5s"), mp5W),
			fmtWeight(i18n.T("Crit, per 1%"), critW),
			fmtWeight(i18n.T("Sta"), staW),
		}, ", ")
		return s

	case "caster":
		spWeights, spLabel, topSchool := casterSchoolWeights(classFile, spec)
		sp := orNum(st.Tab("spellDamage"), topSchool, 0)
		pct := (BasePerLevel*level + sp) / 100
		baseHit := orNum(st, "spellHit", 0)
		cap := SpellMiss[p.Target] - MinMiss
		need := math.Max(0.0, cap-baseHit)
		critW := pyfmt.Round(pct*0.85*CritBonus, 2)
		intW, spiW, mp5W, staW, wandW := 0.45, 0.20, 0.80, 0.25, 0.40
		if dungeon {
			intW, spiW, mp5W, staW, wandW = 0.35, 0.08, 0.50, 0.15, 0.05
		}
		if classFile == "DRUID" || classFile == "SHAMAN" {
			wandW = 0
		}
		weights := append([]Weight{}, spWeights...)
		weights = append(weights, Weight{"crit", critW}, Weight{"intellect", intW},
			Weight{"spirit", spiW}, Weight{"mp5", mp5W}, Weight{"stamina", staW})
		if wandW > 0 {
			weights = append(weights, Weight{"wand_dps", wandW})
		}
		s := &Scale{Profile: profile, Label: i18n.T(p.Label), Pct: pct, HitSteps: [][2]float64{{need, 1.0}}, Weights: weights}
		hitNow := 0.0
		if gearHit < need {
			hitNow = pct
		}
		s.Rows = []*pyjson.Obj{
			row(spLabel, 1.0, i18n.T("reference")),
			row(i18n.T("Hit, next 1%"), pyfmt.Round(hitNow, 2), i18n.T("hit cap against +{target} levels is {cap:g}%", "target", p.Target, "cap", cap)),
			row(i18n.T("Crit, per 1%"), critW, ""),
			row(i18n.T("Intellect"), intW, ""),
			row(i18n.T("Spirit"), spiW, ""),
			row(i18n.T("Mana per 5 sec."), mp5W, ""),
			row(i18n.T("Stamina"), staW, ""),
		}
		if wandW > 0 {
			s.Rows = append(s.Rows, row(i18n.T("Wand DPS"), wandW, i18n.T("wand slot only")))
		}
		s.Text = strings.Join([]string{
			fmtWeight(spLabel, 1.0),
			fmtWeight(i18n.T("Hit, next 1%"), pyfmt.Round(hitNow, 1)),
			fmtWeight(i18n.T("Crit, per 1%"), critW),
			fmtWeight(i18n.T("Int"), intW),
			fmtWeight(i18n.T("Mana/5s"), mp5W),
			fmtWeight(i18n.T("Sta"), staW),
		}, ", ")
		return s

	case "ranged":
		ap := orNum(st, "rangedAttackPower", 0)
		pct := pyfmt.Round((8*level+ap)/100, 2)
		cap := MeleeMiss[p.Target]
		baseHit := orNum(st, "meleeHit", 0)
		need := math.Max(0.0, cap-baseHit)
		critW := pyfmt.Round(pct*1.1, 1)
		agiW := pyfmt.Round(2.0+critW/53.0, 2)
		intW, mp5W, staW, rdpsW := 0.6, 1.2, 0.5, 12.0
		if dungeon {
			intW, mp5W, staW = 0.5, 1.0, 0.2
		}
		s := &Scale{
			Profile: profile, Label: i18n.T(p.Label), Pct: pct * 1.2,
			HitSteps: [][2]float64{{need, 1.0}},
			Weights: []Weight{
				{"rap", 1.0}, {"agility", agiW}, {"crit", critW},
				{"ranged_dps", rdpsW}, {"intellect", intW}, {"mp5", mp5W}, {"stamina", staW},
			},
		}
		hitNow := 0.0
		if gearHit < need {
			hitNow = pyfmt.Round(pct*1.2, 1)
		}
		s.Rows = []*pyjson.Obj{
			row(i18n.T("Ranged attack power"), 1.0, i18n.T("reference")),
			row(i18n.T("Agility"), agiW, ""),
			row(i18n.T("Ranged DPS"), rdpsW, i18n.T("ranged slot only")),
			row(i18n.T("Hit, next 1%"), hitNow, i18n.T("hit cap against +{target} levels is {cap:g}%", "target", p.Target, "cap", cap)),
			row(i18n.T("Crit, per 1%"), critW, ""),
			row(i18n.T("Intellect"), intW, ""),
			row(i18n.T("Mana per 5 sec."), mp5W, ""),
			row(i18n.T("Stamina"), staW, ""),
		}
		s.Text = strings.Join([]string{
			fmtWeight(i18n.T("RAP"), 1.0),
			fmtWeight(i18n.T("Agi"), agiW),
			fmtWeight(i18n.T("Ranged DPS"), rdpsW),
			fmtWeight(i18n.T("Hit, next 1%"), hitNow),
			fmtWeight(i18n.T("Crit, per 1%"), critW),
			fmtWeight(i18n.T("Int"), intW),
		}, ", ")
		return s

	case "tank":
		bear := classFile == "DRUID"
		pal := classFile == "PALADIN"
		armorW, defW, dodgeW, parryW, blockW, bvW := 0.04, 1.2, 6.0, 5.5, 3.5, 0.35
		strW, agiW, hitW, wdpsW, feralW := 0.45, 0.55, 4.0, 1.5, 0.0
		if bear {
			armorW, agiW, parryW, blockW, bvW, wdpsW, feralW = 0.08, 0.85, 0, 0, 0, 0, 0.25
		}
		weights := []Weight{
			{"stamina", 1.0}, {"armor", armorW}, {"defense", defW}, {"dodge", dodgeW},
			{"strength", strW}, {"agility", agiW}, {"hit", hitW},
		}
		if parryW > 0 {
			weights = append(weights, Weight{"parry", parryW})
		}
		if blockW > 0 {
			weights = append(weights, Weight{"block", blockW}, Weight{"block_value", bvW})
		}
		if wdpsW > 0 {
			weights = append(weights, Weight{"weapon_dps", wdpsW})
		}
		if feralW > 0 {
			weights = append(weights, Weight{"feral_ap", feralW})
		}
		if pal {
			weights = append(weights, Weight{"sp_all", 0.5}, Weight{"intellect", 0.35}, Weight{"mp5", 0.8})
		}
		s := &Scale{Profile: profile, Label: i18n.T(p.Label), Weights: weights}
		s.Rows = []*pyjson.Obj{
			row(i18n.T("Stamina"), 1.0, i18n.T("reference")),
			row(i18n.T("Armor"), armorW, ""),
			row(i18n.T("Defense"), defW, ""),
			row(i18n.T("Dodge"), dodgeW, ""),
		}
		if parryW > 0 {
			s.Rows = append(s.Rows, row(i18n.T("Parry"), parryW, ""), row(i18n.T("Block"), blockW, ""), row(i18n.T("Block value"), bvW, ""))
		}
		s.Rows = append(s.Rows, row(i18n.T("Strength"), strW, ""), row(i18n.T("Agility"), agiW, ""), row(i18n.T("Hit, next 1%"), hitW, ""))
		if feralW > 0 {
			s.Rows = append(s.Rows, row(i18n.T("Feral attack power"), feralW, i18n.T("cat and bear form")))
		}
		if pal {
			s.Rows = append(s.Rows, row(i18n.T("Spell power"), 0.5, ""), row(i18n.T("Intellect"), 0.35, ""))
		}
		parts := []string{
			fmtWeight(i18n.T("Sta"), 1.0), fmtWeight(i18n.T("Armor"), armorW),
			fmtWeight(i18n.T("Def"), defW), fmtWeight(i18n.T("Dodge"), dodgeW),
			fmtWeight(i18n.T("Str"), strW), fmtWeight(i18n.T("Agi"), agiW),
		}
		if pal {
			parts = append(parts, fmtWeight(i18n.T("SP"), 0.5))
		}
		s.Text = strings.Join(parts, ", ")
		return s

	default: // "melee": Warrior (Arms/Fury), Rogue, Feral Cat, Ret Paladin, Enhancement Shaman
		ap := orNum(st, "attackPower", 0)
		pct := pyfmt.Round((8*level+ap)/100, 2)
		cap := MeleeMiss[p.Target]
		baseHit := orNum(st, "meleeHit", 0)
		need := math.Max(0.0, cap-baseHit)
		critW := pyfmt.Round(pct*1.1, 1)
		hitW := pyfmt.Round(pct*1.3, 1)

		strW, agiW, wdpsW, feralW := 2.0, pyfmt.Round(critW/20.0, 2), 11.0, 0.0
		switch classFile {
		case "ROGUE":
			strW = 1.0
			agiW = pyfmt.Round(1.0+critW/29.0, 2)
			wdpsW = 10.0
		case "DRUID": // Feral Cat
			strW = 2.0
			agiW = pyfmt.Round(1.0+critW/20.0, 2)
			wdpsW = 0
			feralW = 1.0
		}
		staW := 0.7
		if dungeon {
			staW = 0.25
		}
		weights := []Weight{
			{"ap", 1.0}, {"strength", strW}, {"agility", agiW}, {"crit", critW}, {"stamina", staW},
		}
		if wdpsW > 0 {
			weights = append(weights, Weight{"weapon_dps", wdpsW})
		}
		if feralW > 0 {
			weights = append(weights, Weight{"feral_ap", feralW})
		}
		hybridCaster := classFile == "PALADIN" || classFile == "SHAMAN"
		if hybridCaster {
			weights = append(weights, Weight{"sp_all", 0.35}, Weight{"intellect", 0.5}, Weight{"mp5", 1.0})
		}
		s := &Scale{
			Profile: profile, Label: i18n.T(p.Label), Pct: hitW,
			HitSteps: [][2]float64{{need, 1.0}},
			Weights:  weights,
		}
		hitNow := 0.0
		if gearHit < need {
			hitNow = hitW
		}
		s.Rows = []*pyjson.Obj{
			row(i18n.T("Attack power"), 1.0, i18n.T("reference")),
			row(i18n.T("Strength"), strW, ""),
			row(i18n.T("Agility"), agiW, ""),
			row(i18n.T("Hit, next 1%"), hitNow, i18n.T("hit cap against +{target} levels is {cap:g}%", "target", p.Target, "cap", cap)),
			row(i18n.T("Crit, per 1%"), critW, ""),
		}
		if wdpsW > 0 {
			s.Rows = append(s.Rows, row(i18n.T("Weapon DPS"), wdpsW, i18n.T("melee weapon slots")))
		}
		if feralW > 0 {
			s.Rows = append(s.Rows, row(i18n.T("Feral attack power"), feralW, i18n.T("cat and bear form")))
		}
		if hybridCaster {
			s.Rows = append(s.Rows, row(i18n.T("Spell power"), 0.35, ""), row(i18n.T("Intellect"), 0.5, ""), row(i18n.T("Mana per 5 sec."), 1.0, ""))
		}
		s.Rows = append(s.Rows, row(i18n.T("Stamina"), staW, ""))
		parts := []string{
			fmtWeight(i18n.T("AP"), 1.0), fmtWeight(i18n.T("Str"), strW), fmtWeight(i18n.T("Agi"), agiW),
			fmtWeight(i18n.T("Hit, next 1%"), hitNow), fmtWeight(i18n.T("Crit, per 1%"), critW),
		}
		if wdpsW > 0 {
			parts = append(parts, fmtWeight(i18n.T("Weapon DPS"), wdpsW))
		}
		if feralW > 0 {
			parts = append(parts, fmtWeight(i18n.T("Feral AP"), feralW))
		}
		s.Text = strings.Join(parts, ", ")
		return s
	}
}

func casterSchoolWeights(classFile, spec string) ([]Weight, string, string) {
	switch classFile {
	case "MAGE":
		switch spec {
		case "fire":
			return []Weight{{"sp_fire", 1.0}}, i18n.T("Spell power"), "fire"
		case "frost":
			return []Weight{{"sp_frost", 1.0}}, i18n.T("Spell power"), "frost"
		case "arcane":
			return []Weight{{"sp_arcane", 1.0}}, i18n.T("Spell power"), "arcane"
		default:
			return []Weight{{"sp_frost", 0.5}, {"sp_fire", 0.5}}, i18n.T("Spell power"), "frost"
		}
	case "PRIEST":
		return []Weight{{"sp_shadow", 1.0}}, i18n.T("Shadow / spell power"), "shadow"
	case "DRUID":
		return []Weight{{"sp_nature", 0.55}, {"sp_arcane", 0.45}}, i18n.T("Spell power"), "nature"
	case "SHAMAN":
		return []Weight{{"sp_nature", 0.75}, {"sp_fire", 0.25}}, i18n.T("Spell power"), "nature"
	default:
		return []Weight{{"sp_all", 1.0}}, i18n.T("Spell power"), "shadow"
	}
}

// isPhysicalRole reports whether the character relies on weapon hits (melee/ranged/tank).
func isPhysicalRole(char *sv.Table) bool {
	r := enginebundle.RoleForChar(char)
	return r == "melee" || r == "ranged" || r == "tank"
}
