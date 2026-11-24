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

// ArmorMultiplier returns the fraction of physical damage that passes through
// a monster's armor (Classic/Forever formula: 1 - armor/(armor + 400 + 85*level)).
func ArmorMultiplier(attackerLevel, targetLevel int) float64 {
	if attackerLevel <= 0 {
		attackerLevel = 60
	}
	if targetLevel <= 0 {
		targetLevel = attackerLevel
	}
	armor := float64(targetLevel) * 55.0
	dr := armor / (armor + 400.0 + 85.0*float64(attackerLevel))
	if dr < 0 {
		dr = 0
	}
	if dr > 0.75 {
		dr = 0.75
	}
	return 1 - dr
}

// EffectiveAP returns melee attack power (including Feral AP for Feral Druids).
func EffectiveAP(st *State) float64 {
	ap := st.AttackPower
	if st.ClassFile == "DRUID" && st.Spec == "feral" {
		ap += st.FeralAP
	}
	return ap
}

// WeaponHitAvg computes the average pre-mitigation weapon hit for a physical ability.
func WeaponHitAvg(st *State, sp *Spell) float64 {
	if sp.IsRanged {
		w := st.Ranged
		speed := w.Speed
		if speed <= 0 {
			speed = 2.8
		}
		if sp.Normalized {
			speed = 2.8
		}
		return w.Avg() + st.RangedAttackPower/14.0*speed
	}
	// Feral Cat form uses a 1.0s claw base instead of equipped weapon damage.
	if st.ClassFile == "DRUID" && st.Spec == "feral" {
		baseClaw := float64(st.Level) * 0.85
		return baseClaw + EffectiveAP(st)/14.0
	}
	w := st.MainHand
	speed := w.Speed
	if speed <= 0 {
		speed = 2.4
	}
	norm := w.NormSpeed
	if norm <= 0 {
		if speed >= 3.0 {
			norm = 3.3
		} else if speed <= 1.8 {
			norm = 1.7
		} else {
			norm = 2.4
		}
	}
	apSpeed := speed
	if sp.Normalized {
		apSpeed = norm
	}
	return w.Avg() + EffectiveAP(st)/14.0*apSpeed
}

// MeleeWhiteDPS returns expected white melee auto-attack DPS (main hand + off hand).
func MeleeWhiteDPS(st *State, tgt Target) float64 {
	haste := 1 + st.MeleeHastePct/100
	if haste <= 0 {
		haste = 1
	}
	armor := ArmorMultiplier(st.Level, tgt.Level)
	// Feral Cat form: 1.0s white swings scaling with AP + Feral AP.
	if st.ClassFile == "DRUID" && st.Spec == "feral" {
		skill := float64(st.Level * 5)
		tbl := MeleeTable(skill, tgt.Defense, st.MeleeCrit, false)
		tbl.Miss -= st.MeleeHit
		if tbl.Miss < 0 {
			tbl.Miss = 0
		}
		avg := float64(st.Level)*0.85 + EffectiveAP(st)/14.0
		mult := (100 - tbl.Miss - tbl.Dodge - tbl.Glancing - tbl.Crit)/100 +
			(tbl.Glancing/100)*tbl.GlancePenalty + (tbl.Crit/100)*(1+WhiteCritBonus)
		if mult < 0 {
			mult = 0
		}
		return avg * mult * armor * haste
	}
	mh := st.MainHand
	if mh.Speed <= 0 {
		return 0
	}
	dw := st.OffHand.Speed > 0
	skill := mh.Skill
	if skill <= 0 {
		skill = float64(st.Level * 5)
	}
	swingDPS := func(w Weapon, offhand bool) float64 {
		wSkill := w.Skill
		if wSkill <= 0 {
			wSkill = skill
		}
		tbl := MeleeTable(wSkill, tgt.Defense, st.MeleeCrit, dw)
		tbl.Miss -= st.MeleeHit
		if tbl.Miss < 0 {
			tbl.Miss = 0
		}
		hit := w.Avg() + EffectiveAP(st)/14.0*w.Speed
		if offhand {
			hit *= 0.5
		}
		normal := 100 - tbl.Miss - tbl.Dodge - tbl.Glancing - tbl.Crit
		if normal < 0 {
			normal = 0
		}
		mult := normal/100 + (tbl.Glancing/100)*tbl.GlancePenalty + (tbl.Crit/100)*(1+WhiteCritBonus)
		return (hit / (w.Speed / haste)) * mult * armor
	}
	dps := swingDPS(mh, false)
	if dw {
		dps += swingDPS(st.OffHand, true)
	}
	return dps
}

// RangedAutoShotDPS returns expected Hunter Auto Shot DPS.
func RangedAutoShotDPS(st *State, tgt Target) float64 {
	w := st.Ranged
	if w.Speed <= 0 {
		return 0
	}
	haste := 1 + st.RangedHastePct/100
	if haste <= 0 {
		haste = 1
	}
	skill := w.Skill
	if skill <= 0 {
		skill = float64(st.Level * 5)
	}
	miss := RangedMiss(skill, tgt.Defense) - st.MeleeHit
	if miss < 0 {
		miss = 0
	}
	crit := st.RangedCrit - (tgt.Defense-skill)*0.2
	if crit < 0 {
		crit = 0
	}
	hit := w.Avg() + st.RangedAttackPower/14.0*w.Speed
	armor := ArmorMultiplier(st.Level, tgt.Level)
	return (hit / (w.Speed / haste)) * (1 - miss/100) * (1 + crit/100*WhiteCritBonus) * armor
}

func rageBudgetPS(level int, autoAttackDPS float64) float64 {
	l := float64(level)
	if l <= 0 {
		l = 60
	}
	conv := 0.0091107836*l*l + 3.225598133*l + 4.2652911
	return 2.0 + autoAttackDPS*7.5/conv
}

func hunterPetDPS(st *State) float64 {
	if st.PetDPS > 0 {
		return st.PetDPS
	}
	return float64(st.Level)*1.1 + st.RangedAttackPower*0.08
}

// passiveSources computes passive DPS from auto-attacks, auto-shots, and pets.
func passiveSources(st *State, tgt Target, actions []Action) (autoAtk, autoShot, pet float64) {
	for _, a := range actions {
		switch a.Kind {
		case "autoattack":
			autoAtk = MeleeWhiteDPS(st, tgt)
		case "autoshot":
			autoShot = RangedAutoShotDPS(st, tgt)
		case "pet":
			pet = hunterPetDPS(st)
		}
	}
	return autoAtk, autoShot, pet
}

// usesNonMana reports whether a rotation spends Rage or Energy instead of Mana.
func usesNonMana(actions []Action) (usesRage, usesEnergy bool) {
	for _, a := range actions {
		if a.Spell == nil {
			continue
		}
		if a.Spell.Cost.Rage > 0 {
			usesRage = true
		}
		if a.Spell.Cost.Energy > 0 {
			usesEnergy = true
		}
	}
	return usesRage, usesEnergy
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
	return st.Wand.DPS * (1 - miss/100) * (1 + crit/100*WhiteCritBonus) * (1 + st.Mods.WandPct/100)
}
