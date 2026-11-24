package enginebundle

import (
	"testing"

	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

func makeCharWithGroups(classFile string, groups [][2]any, formSpellID int) *sv.Table {
	c := sv.NewTable()
	ch := sv.NewTable()
	ch.Set("classFile", classFile)
	if formSpellID > 0 {
		st := sv.NewTable()
		form := sv.NewTable()
		form.Set("spellID", int64(formSpellID))
		st.Set("form", form)
		ch.Set("stats", st)
	}
	c.Set("character", ch)

	grpList := sv.NewTable()
	for i, g := range groups {
		gt := sv.NewTable()
		gt.Set("name", g[0])
		gt.Set("spent", g[1])
		grpList.Set(int64(i+1), gt)
	}
	tree := sv.NewTable()
	tree.Set("groups", grpList)
	classTrees := sv.NewTable()
	classTrees.Set(int64(1), tree)
	tal := sv.NewTable()
	tal.Set("class", classTrees)
	c.Set("talents", tal)
	return c
}

func TestSpecOfGroupsAndRoles(t *testing.T) {
	cases := []struct {
		classFile string
		groups    [][2]any
		formID    int
		wantSpec  string
		wantName  string
		wantRole  string
		wantMet   string
	}{
		{"PRIEST", [][2]any{{"Discipline", int64(0)}, {"Holy", int64(5)}, {"Shadow", int64(31)}}, 0, "shadow", "Shadow", "caster", "dps"},
		{"PRIEST", [][2]any{{"Disziplin", int64(18)}, {"Heilig", int64(0)}, {"Schattenmagie", int64(2)}}, 0, "discipline", "Disziplin", "healer", "hps"},
		{"WARLOCK", [][2]any{{"Affliction", int64(5)}, {"Demonology", int64(0)}, {"Destruction", int64(30)}}, 0, "destruction", "Destruction", "caster", "dps"},
		{"MAGE", [][2]any{{"Arcane", int64(0)}, {"Fire", int64(0)}, {"Frost", int64(31)}}, 0, "frost", "Frost", "caster", "dps"},
		{"WARRIOR", [][2]any{{"Arms", int64(31)}, {"Fury", int64(20)}, {"Protection", int64(0)}}, 0, "arms", "Arms", "melee", "dps"},
		{"WARRIOR", [][2]any{{"Arms", int64(5)}, {"Fury", int64(5)}, {"Protection", int64(31)}}, 0, "protection", "Protection", "tank", "dps"},
		{"ROGUE", [][2]any{{"Assassination", int64(15)}, {"Combat", int64(31)}, {"Subtlety", int64(5)}}, 0, "combat", "Combat", "melee", "dps"},
		{"HUNTER", [][2]any{{"Beast Mastery", int64(0)}, {"Marksmanship", int64(31)}, {"Survival", int64(20)}}, 0, "marksmanship", "Marksmanship", "ranged", "dps"},
		{"PALADIN", [][2]any{{"Holy", int64(31)}, {"Protection", int64(20)}, {"Retribution", int64(0)}}, 0, "holy", "Holy", "healer", "hps"},
		{"PALADIN", [][2]any{{"Holy", int64(0)}, {"Protection", int64(31)}, {"Retribution", int64(0)}}, 0, "protection", "Protection", "tank", "dps"},
		{"PALADIN", [][2]any{{"Holy", int64(0)}, {"Protection", int64(0)}, {"Retribution", int64(31)}}, 0, "retribution", "Retribution", "melee", "dps"},
		{"SHAMAN", [][2]any{{"Elemental", int64(31)}, {"Enhancement", int64(0)}, {"Restoration", int64(20)}}, 0, "elemental", "Elemental", "caster", "dps"},
		{"SHAMAN", [][2]any{{"Elemental", int64(0)}, {"Enhancement", int64(31)}, {"Restoration", int64(0)}}, 0, "enhancement", "Enhancement", "melee", "dps"},
		{"SHAMAN", [][2]any{{"Elemental", int64(0)}, {"Enhancement", int64(0)}, {"Restoration", int64(31)}}, 0, "restoration", "Restoration", "healer", "hps"},
		{"DRUID", [][2]any{{"Balance", int64(31)}, {"Feral Combat", int64(0)}, {"Restoration", int64(20)}}, 0, "balance", "Balance", "caster", "dps"},
		{"DRUID", [][2]any{{"Balance", int64(0)}, {"Feral Combat", int64(31)}, {"Restoration", int64(0)}}, 0, "feral", "Feral Combat", "melee", "dps"},
		{"DRUID", [][2]any{{"Balance", int64(0)}, {"Feral Combat", int64(31)}, {"Restoration", int64(0)}}, 9634, "feral", "Feral Combat", "tank", "dps"},
		{"DRUID", [][2]any{{"Balance", int64(0)}, {"Feral Combat", int64(0)}, {"Restoration", int64(31)}}, 0, "restoration", "Restoration", "healer", "hps"},
		{"MAGE", [][2]any{{"Arcane", int64(0)}, {"Fire", int64(0)}, {"Frost", int64(0)}}, 0, "", "", "caster", "dps"},
	}
	for _, tc := range cases {
		c := makeCharWithGroups(tc.classFile, tc.groups, tc.formID)
		if got := SpecOf(c); got != tc.wantSpec {
			t.Errorf("%s: SpecOf = %q, want %q", tc.classFile, got, tc.wantSpec)
		}
		if got := SpecName(c); got != tc.wantName {
			t.Errorf("%s: SpecName = %q, want %q", tc.classFile, got, tc.wantName)
		}
		if got := RoleForChar(c); got != tc.wantRole {
			t.Errorf("%s/%s: RoleForChar = %q, want %q", tc.classFile, tc.wantSpec, got, tc.wantRole)
		}
		if got := DefaultMetric(tc.classFile, tc.wantSpec); got != tc.wantMet {
			t.Errorf("%s/%s: DefaultMetric = %q, want %q", tc.classFile, tc.wantSpec, got, tc.wantMet)
		}
	}
}
