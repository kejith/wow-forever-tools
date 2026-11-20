package engine

// The Stage-1 evaluator: a priority rotation becomes a steady-state cast
// cycle in closed form. Each maintained DoT/HoT occupies execute/duration
// of the timeline, each cooldown spell execute/cooldown, and the filler
// takes the rest. The mana budget over the fight then decides how much of
// that cycle is affordable; a Warlock converts health with Life Tap first,
// and whatever casting is still not affordable falls back to the wand.

// Action is one entry of a spec's priority rotation.
type Action struct {
	Kind  string // "dot", "hot", "cooldown", "filler", "heal", "wand"
	Spell *Spell
}

// Row is one line of the metric breakdown shown in the app.
type Row struct {
	SpellID  int     `json:"spellId,omitempty"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`            // contribution to the metric per second
	SharePct float64 `json:"sharePct"`         // share of the fight time spent on it
	Kind     string  `json:"kind"`             // action kind, or "lifetap"
	PerMin   float64 `json:"perMin,omitempty"` // casts per minute (regen rows)
}

// Metrics is the result of one evaluation.
type Metrics struct {
	Metric       string   `json:"metric"`
	Value        float64  `json:"value"`
	DPS          float64  `json:"dps"`
	HPS          float64  `json:"hps"`
	TTOOMSec     *float64 `json:"ttoomSeconds"` // nil = never runs out of mana
	ManaSpentPS  float64  `json:"manaSpentPs"`  // of the unconstrained rotation
	ManaBudgetPS float64  `json:"manaBudgetPs"` // pool over the fight + regeneration
	Breakdown    []Row    `json:"breakdown"`
}

// healthUseFraction is how much of the health pool the model lets Life Tap
// consume over a fight (rule of thumb; bandages/healers refill the rest).
const healthUseFraction = 0.4

const lifeTapGCDSec = 1.5

// Evaluate computes the Stage-1 metrics of a rotation.
func Evaluate(st *State, fight Fight, actions []Action, metric string) Metrics {
	tgt := NewTarget(st.Level, fight.TargetDelta)
	m := Metrics{Metric: metric}
	if fight.Seconds <= 0 {
		fight.Seconds = 60
	}

	// Unconstrained cycle: time shares and per-second rates.
	type part struct {
		act   Action
		res   CastResult
		share float64 // fraction of the timeline
		dps   float64
		hps   float64
		mps   float64 // mana per second of the timeline
	}
	var parts []part
	used := 0.0
	fillerIdx := -1
	for _, a := range actions {
		if a.Kind == "wand" || a.Spell == nil {
			continue
		}
		res := ResolveCast(a.Spell, st, tgt, nil)
		p := part{act: a, res: res}
		switch a.Kind {
		case "dot", "hot":
			if res.DurationSec <= 0 {
				continue
			}
			p.share = res.ExecuteSec / res.DurationSec
			p.dps = res.Damage / res.DurationSec
			p.hps = res.Healing / res.DurationSec
			p.mps = res.ManaCost / res.DurationSec
		case "cooldown":
			cd := float64(a.Spell.CooldownMs) / 1000
			if cd <= 0 {
				continue
			}
			p.share = res.ExecuteSec / cd
			p.dps = res.Damage / cd
			p.hps = res.Healing / cd
			p.mps = res.ManaCost / cd
		case "filler", "heal":
			if fillerIdx >= 0 {
				continue // one filler is enough for the closed form
			}
			parts = append(parts, p)
			fillerIdx = len(parts) - 1
			continue
		}
		used += p.share
		parts = append(parts, p)
	}
	if used > 1 {
		// More maintenance than timeline: scale everything down evenly.
		for i := range parts {
			if i != fillerIdx {
				parts[i].share /= used
				parts[i].dps /= used
				parts[i].hps /= used
				parts[i].mps /= used
			}
		}
		used = 1
	}
	if fillerIdx >= 0 {
		f := &parts[fillerIdx]
		share := 1 - used
		f.share = share
		if f.res.ExecuteSec > 0 {
			f.dps = share * f.res.Damage / f.res.ExecuteSec
			f.hps = share * f.res.Healing / f.res.ExecuteSec
			f.mps = share * f.res.ManaCost / f.res.ExecuteSec
		}
	}

	var rotDPS, rotHPS, rotMPS float64
	for _, p := range parts {
		rotDPS += p.dps
		rotHPS += p.hps
		rotMPS += p.mps
	}

	// Mana budget: the pool spread over the fight plus regeneration that
	// works while casting. Spirit regeneration under the five-second rule
	// counts 0 for damage rotations in Stage 1 (healers refine this in M3).
	budgetPS := 0.0
	if fight.Seconds > 0 {
		budgetPS = st.ManaMax/fight.Seconds + st.MP5/5
	}
	m.ManaSpentPS = rotMPS
	m.ManaBudgetPS = budgetPS

	// Time to OOM at the full rotation without conversions.
	if rotMPS > st.MP5/5 && rotMPS > 0 {
		t := st.ManaMax / (rotMPS - st.MP5/5)
		m.TTOOMSec = &t
	}

	// How much of the cycle is affordable.
	castFraction := 1.0 // fraction of the fight spent on the rotation
	tapRate := 0.0      // Life Taps per second of the fight
	wandFraction := 0.0
	deficit := rotMPS - budgetPS
	if deficit > 0 {
		tapMana := st.LifeTap.ManaPerCast * (1 + st.Mods.LifeTapPct/100)
		if tapMana > 0 {
			// Taps replace rotation time: x taps/s cost x*gcd of the
			// timeline and must cover the deficit of the remaining cycle:
			//   x*tapMana = deficit' where spending shrinks with (1-x*gcd).
			x := deficit / (tapMana + rotMPS*lifeTapGCDSec)
			// Health budget caps the taps.
			healthBudget := healthUseFraction*st.HealthMax + fight.Seconds*st.HP5/5
			if st.LifeTap.HealthPerCast > 0 {
				maxRate := healthBudget / (fight.Seconds * st.LifeTap.HealthPerCast)
				if x > maxRate {
					x = maxRate
				}
			}
			tapRate = x
			castFraction = 1 - x*lifeTapGCDSec
			// Re-check affordability at the capped tap rate.
			if rotMPS*castFraction > budgetPS+x*tapMana && rotMPS > 0 {
				castFraction = (budgetPS + x*tapMana) / rotMPS
				wandFraction = 1 - castFraction - x*lifeTapGCDSec
			}
		} else if rotMPS > 0 {
			castFraction = budgetPS / rotMPS
			wandFraction = 1 - castFraction
		}
		if castFraction < 0 {
			castFraction = 0
		}
		if wandFraction < 0 {
			wandFraction = 0
		}
	}

	wandDPS := WandDPS(st, tgt)
	m.DPS = rotDPS*castFraction + wandDPS*wandFraction
	m.HPS = rotHPS * castFraction

	// Breakdown.
	for _, p := range parts {
		label := p.act.Spell.Name
		value := p.dps * castFraction
		if p.act.Spell.IsHeal {
			value = p.hps * castFraction
		}
		m.Breakdown = append(m.Breakdown, Row{
			SpellID:  p.act.Spell.ID,
			Label:    label,
			Value:    value,
			SharePct: p.share * castFraction * 100,
			Kind:     p.act.Kind,
		})
	}
	if tapRate > 0 {
		m.Breakdown = append(m.Breakdown, Row{
			Label:    "Life Tap",
			Kind:     "lifetap",
			SharePct: tapRate * lifeTapGCDSec * 100,
			PerMin:   tapRate * 60,
		})
	}
	if wandFraction > 0.0005 {
		m.Breakdown = append(m.Breakdown, Row{
			Label:    "Wand",
			Kind:     "wand",
			Value:    wandDPS * wandFraction,
			SharePct: wandFraction * 100,
		})
	}

	switch metric {
	case "hps":
		m.Value = m.HPS
	case "ttoom":
		if m.TTOOMSec != nil {
			m.Value = *m.TTOOMSec
		} else {
			m.Value = fight.Seconds
		}
	default:
		m.Value = m.DPS
	}
	return m
}
