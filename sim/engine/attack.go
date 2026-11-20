package engine

// White-hit attack table against a monster, by weapon skill vs defense.
// These are the community-measured Classic formulas (see WoWSims,
// sim/core/attack tables; MIT) and are unverified for Forever — they are
// used for wands in Stage 1 and for melee classes later.

// AttackTable is the single-roll outcome distribution of one white swing,
// in percent. Entries that cannot happen (a wand cannot be dodged) are
// zeroed by the caller's accessor.
type AttackTable struct {
	Miss          float64
	Dodge         float64
	Glancing      float64
	GlancePenalty float64 // average damage fraction kept by a glancing blow
	Crit          float64
}

// MeleeTable builds the attack table for a white swing from the front.
func MeleeTable(skill, defense, critPct float64, dualWield bool) AttackTable {
	diff := defense - skill
	t := AttackTable{}

	// Miss: 5% base; above 10 points of difference the slope steepens.
	if diff <= 10 {
		t.Miss = 5 + diff*0.1
	} else {
		t.Miss = 6 + (diff-10)*0.4
	}
	if dualWield {
		t.Miss += 19
	}
	if t.Miss < 0 {
		t.Miss = 0
	}

	t.Dodge = 5 + diff*0.1
	if t.Dodge < 0 {
		t.Dodge = 0
	}

	// Glancing blows happen only against targets whose defense exceeds the
	// character's maximum skill for their level; chance 10% + 2% per point.
	if diff > 0 {
		t.Glancing = 10 + diff*2
		if t.Glancing > 40 {
			t.Glancing = 40
		}
		low := 1.3 - 0.05*diff
		if low > 0.91 {
			low = 0.91
		}
		if low < 0.01 {
			low = 0.01
		}
		high := 1.2 - 0.03*diff
		if high > 0.99 {
			high = 0.99
		}
		if high < 0.2 {
			high = 0.2
		}
		t.GlancePenalty = (low + high) / 2
	}

	// Crit is suppressed by 0.2% per point of defense above skill (1% per
	// 5 points), the commonly measured Classic value.
	t.Crit = critPct - diff*0.2
	if t.Crit < 0 {
		t.Crit = 0
	}

	// The table is single-roll: cap crit by what is left.
	rest := 100 - t.Miss - t.Dodge - t.Glancing
	if rest < 0 {
		rest = 0
	}
	if t.Crit > rest {
		t.Crit = rest
	}
	return t
}

// RangedMiss is the miss chance of a wand or ranged white shot (no dodge,
// no glancing at range).
func RangedMiss(skill, defense float64) float64 {
	diff := defense - skill
	var miss float64
	if diff <= 10 {
		miss = 5 + diff*0.1
	} else {
		miss = 6 + (diff-10)*0.4
	}
	if miss < 0 {
		miss = 0
	}
	if miss > 100 {
		miss = 100
	}
	return miss
}

// WandDPS is the expected damage per second of wanding: exported wand DPS
// through the ranged miss and crit rolls (wand shots cannot be dodged;
// ranged crits do double damage).
func WandDPS(st *State, tgt Target) float64 {
	if st.Wand.DPS <= 0 {
		return 0
	}
	skill := st.Wand.Skill
	if skill <= 0 {
		skill = float64(st.Level * 5)
	}
	miss := RangedMiss(skill, tgt.Defense)
	crit := st.RangedCrit - (tgt.Defense-skill)*0.2
	if crit < 0 {
		crit = 0
	}
	return st.Wand.DPS * (1 - miss/100) * (1 + crit/100*WhiteCritBonus)
}
