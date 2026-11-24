package upgrades

import (
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

func TestUsableWeaponsByClass(t *testing.T) {
	priest := usableWeaponsFor("PRIEST")
	if !priest[4] {
		t.Error("priest must be able to equip 1H maces (subclass 4)")
	}
	if priest[7] {
		t.Error("priest must not equip 1H swords (subclass 7)")
	}
	warlock := usableWeaponsFor("WARLOCK")
	if !warlock[7] || warlock[4] {
		t.Errorf("warlock weapons unexpected: %v", warlock)
	}
	shaman := usableArmorFor("SHAMAN", 45)
	if !shaman[3] || !shaman[6] || !shaman[9] {
		t.Errorf("level 45 shaman should equip mail, shields, and totems: %v", shaman)
	}
	druid := usableArmorFor("DRUID", 45)
	if !druid[8] || druid[3] {
		t.Errorf("druid should equip idols (8) and not mail (3): %v", druid)
	}
	paladin := usableArmorFor("PALADIN", 45)
	if !paladin[4] || !paladin[7] {
		t.Errorf("level 45 paladin should equip plate (4) and librams (7): %v", paladin)
	}
}

func TestSlotOfRelicsAndOffhand(t *testing.T) {
	for _, tc := range []struct {
		loc  string
		want int64
	}{
		{"INVTYPE_RELIC", 18},
		{"INVTYPE_THROWN", 18},
		{"INVTYPE_WEAPONOFFHAND", 17},
	} {
		got := SlotOf[tc.loc]
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("SlotOf[%q] = %v, want [%d]", tc.loc, got, tc.want)
		}
	}
}

func TestDualWieldAndSlotLabels(t *testing.T) {
	if !canDualWield("ROGUE", 10, "combat", nil) || canDualWield("ROGUE", 9, "combat", nil) {
		t.Error("rogue dual-wield threshold should be level 10")
	}
	if !canDualWield("WARRIOR", 20, "fury", nil) || canDualWield("MAGE", 60, "frost", nil) {
		t.Error("warrior dual-wield or mage check failed")
	}
	if !canDualWield("SHAMAN", 40, "enhancement", nil) || canDualWield("SHAMAN", 40, "elemental", nil) {
		t.Error("shaman dual-wield should require enhancement")
	}
	if got := SlotLabelFor(18, "DRUID"); got != "Relic" {
		t.Errorf("druid slot 18 = %q, want Relic", got)
	}
	if got := SlotLabelFor(18, "HUNTER"); got != "Ranged" {
		t.Errorf("hunter slot 18 = %q, want Ranged", got)
	}
	if got := SlotLabelFor(18, "WARLOCK"); got != "Wand" {
		t.Errorf("warlock slot 18 = %q, want Wand", got)
	}
}

func TestIsUnique(t *testing.T) {
	for _, tip := range [][]string{
		{"Unique"},
		{"Unique-Equipped"},
		{"Einzigartig"},
		{"Einzigartig anlegbar"},
		{"Unique-Equipped: Ring (1)"},
	} {
		if !isUnique(tip) {
			t.Errorf("expected unique for %v", tip)
		}
	}
	if isUnique([]string{"Binds when equipped", "+10 Intellect"}) {
		t.Error("non-unique tooltip flagged as unique")
	}
}

func TestClassScalesAllNineClasses(t *testing.T) {
	makeChar := func(cls string, groups [][2]any) *sv.Table {
		c := sv.NewTable()
		ch := sv.NewTable()
		ch.Set("classFile", cls)
		ch.Set("level", int64(60))
		st := sv.NewTable()
		st.Set("attackPower", int64(600))
		st.Set("rangedAttackPower", int64(650))
		st.Set("healthMax", int64(3500))
		st.Set("powerMax", int64(3000))
		ch.Set("stats", st)
		c.Set("character", ch)
		if len(groups) > 0 {
			gl := sv.NewTable()
			for i, g := range groups {
				gt := sv.NewTable()
				gt.Set("name", g[0])
				gt.Set("spent", g[1])
				gl.Set(int64(i+1), gt)
			}
			tr := sv.NewTable()
			tr.Set("groups", gl)
			ct := sv.NewTable()
			ct.Set(int64(1), tr)
			tal := sv.NewTable()
			tal.Set("class", ct)
			c.Set("talents", tal)
		}
		return c
	}

	strItem := sv.NewTable()
	strStats := sv.NewTable()
	strStats.Set("ITEM_MOD_STRENGTH_SHORT", int64(20))
	strStats.Set("ITEM_MOD_ATTACK_POWER_SHORT", int64(30))
	strItem.Set("stats", strStats)

	agiItem := sv.NewTable()
	agiStats := sv.NewTable()
	agiStats.Set("ITEM_MOD_AGILITY_SHORT", int64(20))
	agiStats.Set("ITEM_MOD_RANGED_ATTACK_POWER_SHORT", int64(30))
	agiItem.Set("stats", agiStats)

	healItem := sv.NewTable()
	healStats := sv.NewTable()
	healStats.Set("ITEM_MOD_SPELL_HEALING_DONE_SHORT", int64(40))
	healStats.Set("ITEM_MOD_INTELLECT_SHORT", int64(10))
	healItem.Set("stats", healStats)

	tankItem := sv.NewTable()
	tankStats := sv.NewTable()
	tankStats.Set("ITEM_MOD_STAMINA_SHORT", int64(20))
	tankStats.Set("ITEM_MOD_DEFENSE_SKILL_RATING_SHORT", int64(10))
	tankStats.Set("RESISTANCE0_NAME", int64(500))
	tankItem.Set("stats", tankStats)

	fireItem := sv.NewTable()
	fireStats := sv.NewTable()
	fireStats.Set("ITEM_MOD_FIRE_DAMAGE_DONE_SHORT", int64(30))
	fireItem.Set("stats", fireStats)

	// Warrior Fury values Strength/AP
	wScale := BuildScale(makeChar("WARRIOR", [][2]any{{"Arms", int64(0)}, {"Fury", int64(31)}, {"Protection", int64(0)}}), "dungeon", 0, 0)
	if v := SetValue(NormStats(strItem, false, nil), wScale); v <= 50 {
		t.Errorf("fury warrior should value Str+AP item strongly, got %.2f", v)
	}
	// Warrior Prot values Stamina/Defense/Armor
	protScale := BuildScale(makeChar("WARRIOR", [][2]any{{"Arms", int64(0)}, {"Fury", int64(0)}, {"Protection", int64(31)}}), "dungeon", 0, 0)
	if v := SetValue(NormStats(tankItem, false, nil), protScale); v <= 40 {
		t.Errorf("prot warrior should value Sta+Def+Armor strongly, got %.2f", v)
	}
	// Hunter values Agi+RAP
	hScale := BuildScale(makeChar("HUNTER", [][2]any{{"BM", int64(0)}, {"MM", int64(31)}, {"Surv", int64(0)}}), "dungeon", 0, 0)
	if v := SetValue(NormStats(agiItem, false, nil), hScale); v <= 50 {
		t.Errorf("hunter should value Agi+RAP strongly, got %.2f", v)
	}
	// Fire Mage values Fire spell power
	mScale := BuildScale(makeChar("MAGE", [][2]any{{"Arcane", int64(0)}, {"Fire", int64(31)}, {"Frost", int64(0)}}), "dungeon", 0, 0)
	if v := SetValue(NormStats(fireItem, false, nil), mScale); v != 30 {
		t.Errorf("fire mage should value +30 fire SP as 30, got %.2f", v)
	}
	// Paladin Holy values Healing
	pScale := BuildScale(makeChar("PALADIN", [][2]any{{"Holy", int64(31)}, {"Prot", int64(0)}, {"Ret", int64(0)}}), "dungeon", 0, 0)
	if v := SetValue(NormStats(healItem, false, nil), pScale); v <= 40 {
		t.Errorf("holy paladin should value healing+int strongly, got %.2f", v)
	}
}
