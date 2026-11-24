package upgrades

import (
	"testing"
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
	if !shaman[3] || !shaman[6] {
		t.Errorf("level 45 shaman should equip mail and shields: %v", shaman)
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
