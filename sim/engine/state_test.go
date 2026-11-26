package engine

import (
	"math"
	"testing"
)

func TestApplyDeltaEnchantKeys(t *testing.T) {
	base := &State{
		ClassFile: "HUNTER",
		Level:     60,
		HealthMax: 3000,
		ManaMax:   2500,
		MainHand:  Weapon{Min: 100, Max: 150, Speed: 2.5, DPS: 50},
		OffHand:   Weapon{Min: 50, Max: 80, Speed: 1.6, DPS: 40.625},
		Ranged:    Weapon{Min: 80, Max: 140, Speed: 2.8, DPS: 39.285714},
	}

	got := base.ApplyDelta(map[string]float64{
		"weaponDmg":  5,
		"offhandDmg": 4,
		"rangedDmg":  7,
		"rangedHit":  3,
		"rangedCrit": 2,
		"health":     100,
		"mana":       150,
	})

	if got.MainHand.Min != 105 || got.MainHand.Max != 155 || math.Abs(got.MainHand.DPS-52) > 1e-9 {
		t.Fatalf("MainHand = %+v, want Min=105 Max=155 DPS=52", got.MainHand)
	}
	if got.OffHand.Min != 54 || got.OffHand.Max != 84 || math.Abs(got.OffHand.DPS-43.125) > 1e-9 {
		t.Fatalf("OffHand = %+v, want Min=54 Max=84 DPS=43.125", got.OffHand)
	}
	if got.Ranged.Min != 87 || got.Ranged.Max != 147 || math.Abs(got.Ranged.DPS-(39.285714+7.0/2.8)) > 1e-6 {
		t.Fatalf("Ranged = %+v", got.Ranged)
	}
	if got.MeleeHit != 3 || got.RangedCrit != 2 {
		t.Fatalf("ranged hit/crit = %.1f/%.1f, want 3/2", got.MeleeHit, got.RangedCrit)
	}
	if got.HealthMax != 3100 || got.ManaMax != 2650 {
		t.Fatalf("health/mana = %.0f/%.0f, want 3100/2650", got.HealthMax, got.ManaMax)
	}

	// Non-hunter classes must not gain melee hit from rangedHit.
	warr := &State{ClassFile: "WARRIOR", Level: 60}
	if w := warr.ApplyDelta(map[string]float64{"rangedHit": 3}); w.MeleeHit != 0 {
		t.Fatalf("warrior MeleeHit = %.1f, want 0", w.MeleeHit)
	}
}
