package engine

import "math"

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
	Rank     int     `json:"rank,omitempty"`   // the spell's rank number (run.go fills it in)
	Leech    float64 `json:"leech,omitempty"`  // health per second the spell returns to the caster
}

// Metrics is the result of one evaluation.
type Metrics struct {
	Metric       string   `json:"metric"`
	Value        float64  `json:"value"`
	DPS          float64  `json:"dps"`
	HPS          float64  `json:"hps"`
	TTOOMSec     *float64 `json:"ttoomSeconds"`         // nil = never runs out of mana
	ManaSpentPS  float64  `json:"manaSpentPs"`          // of the unconstrained rotation
	ManaBudgetPS float64  `json:"manaBudgetPs"`         // pool over the fight + regeneration
	SelfHealPS   float64  `json:"selfHealPs,omitempty"` // health per second from leech spells (feeds Life Tap)
	Breakdown    []Row    `json:"breakdown"`
	MC           *MCStats `json:"mc,omitempty"` // set when the Monte-Carlo runs played the rotation
}

// healthUseFraction is how much of the health pool the model lets Life Tap
// consume over a fight (rule of thumb; bandages/healers refill the rest).
// On top of that Life Tap may spend what the character regenerates (hp5) and
// what the rotation's leech spells return (Siphon Life, Drain Life, Death
// Coil): the model taps early enough that this health is never wasted on a
// full health bar.
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
		leech float64 // health per second the caster gains
		haste float64 // how much faster it ticks per unit of filler share (a channel like Wrack)
	}
	ctx := ContextOf(actions)
	var parts []part
	used := 0.0
	fillerIdx := -1
	for _, a := range actions {
		if a.Kind == "wand" || a.Spell == nil {
			continue
		}
		res := ResolveCastIn(a.Spell, st, tgt, ctx, nil)
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
			p.leech = res.Leech / res.DurationSec
		case "cooldown":
			cd := float64(a.Spell.CooldownMs) / 1000
			if cd <= 0 {
				continue
			}
			p.share = res.ExecuteSec / cd
			p.dps = res.Damage / cd
			p.hps = res.Healing / cd
			p.mps = res.ManaCost / cd
			p.leech = res.Leech / cd
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
				parts[i].leech /= used
			}
		}
		used = 1
	}

	// The filler takes what the maintenance leaves of the timeline. Its
	// rates are kept per unit of that share, because a mana shortage takes
	// time from the filler first.
	fillerShare := 0.0
	var fDPS, fHPS, fMPS, fLeech float64
	if fillerIdx >= 0 {
		f := &parts[fillerIdx]
		fillerShare = 1 - used
		// A DoT or cooldown that comes due while the filler is being cast or
		// channeled waits for that cast to end: on average half a cast, for
		// the share of the timeline the filler takes. That stretches every
		// maintenance cycle and gives the freed time back to the filler.
		if wait := fillerShare * f.res.ExecuteSec / 2; wait > 0 {
			for i := range parts {
				if i == fillerIdx {
					continue
				}
				cycle := parts[i].res.DurationSec
				if parts[i].act.Kind == "cooldown" {
					cycle = float64(parts[i].act.Spell.CooldownMs) / 1000
				}
				if cycle <= 0 {
					continue
				}
				k := cycle / (cycle + wait)
				freed := parts[i].share * (1 - k)
				fillerShare += freed
				used -= freed
				parts[i].share *= k
				parts[i].dps *= k
				parts[i].hps *= k
				parts[i].mps *= k
				parts[i].leech *= k
			}
		}
		if f.res.ExecuteSec > 0 {
			fDPS = f.res.Damage / f.res.ExecuteSec
			fHPS = f.res.Healing / f.res.ExecuteSec
			fMPS = f.res.ManaCost / f.res.ExecuteSec
			fLeech = f.res.Leech / f.res.ExecuteSec
		}
		// A channel that makes the DoTs tick faster (Wrack): for the share
		// of the timeline it is channeled, those DoTs deal that much more.
		for i := range parts {
			if parts[i].act.Kind == "dot" {
				parts[i].haste = st.Mods.channelHaste(f.act.Spell, parts[i].act.Spell) / 100
			}
		}
	}
	// maintenance sums what everything but the filler does per second while
	// the filler holds `share` of the timeline.
	maintenance := func(share float64) (dps, hps, leech float64) {
		for i, p := range parts {
			if i == fillerIdx {
				continue
			}
			k := 1 + share*p.haste
			dps += p.dps * k
			hps += p.hps
			leech += p.leech * k
		}
		return
	}
	maintMPS := 0.0
	for i, p := range parts {
		if i != fillerIdx {
			maintMPS += p.mps
		}
	}
	rotMPS := maintMPS + fillerShare*fMPS

	// Mana budget: the pool spread over the fight, gear mp5, and the share
	// of spirit regeneration that continues while casting (Meditation).
	// The model casts for the whole fight, so the rest of the spirit
	// regeneration counts 0 under the five-second rule.
	regenPS := st.MP5/5 + st.RegenPS()*st.Mods.CastingRegenPct/100
	budgetPS := 0.0
	if fight.Seconds > 0 {
		budgetPS = st.ManaMax/fight.Seconds + regenPS
	}
	m.ManaSpentPS = rotMPS
	m.ManaBudgetPS = budgetPS

	// Time to OOM at the full rotation without conversions.
	if rotMPS > regenPS && rotMPS > 0 {
		t := st.ManaMax / (rotMPS - regenPS)
		m.TTOOMSec = &t
	}

	// How much of the cycle is affordable. The DoTs, HoTs and cooldowns keep
	// their priority: Life Tap takes its time from the filler, and mana that
	// is still missing is the filler's too. Only when not even the
	// maintenance is affordable does it shrink as well.
	share := fillerShare // the filler's real share of the fight
	maintScale := 1.0
	tapRate := 0.0 // Life Taps per second of the fight
	wandFraction := 0.0
	if deficit := rotMPS - budgetPS; deficit > 0 {
		tapMana := st.LifeTap.ManaPerCast * (1 + st.Mods.LifeTapPct/100)
		if tapMana > 0 {
			// x taps per second replace x*gcd of filler time and must
			// cover what the remaining cycle lacks.
			x := deficit / (tapMana + fMPS*lifeTapGCDSec)
			// The health budget caps the taps: the share of the pool the
			// model spends, hp5, and what the leech spells return. The
			// leech depends on the filler's share, which depends on the
			// taps, so the cap is found by iteration.
			healthPS := healthUseFraction*st.HealthMax/fight.Seconds + st.HP5/5
			hpc := st.LifeTap.HealthPerCast
			for iter := 0; iter < 50; iter++ {
				if most := (1 - used) / lifeTapGCDSec; x > most {
					x = most
				}
				share = math.Max(0, fillerShare-x*lifeTapGCDSec)
				if fMPS > 0 {
					if afford := (budgetPS + x*tapMana - maintMPS) / fMPS; afford < share {
						share = math.Max(0, afford)
					}
				}
				if hpc <= 0 {
					break
				}
				_, _, leech := maintenance(share)
				most := (healthPS + leech + share*fLeech) / hpc
				if x <= most+1e-12 {
					break
				}
				x = most
			}
			tapRate = x
		} else if fMPS > 0 {
			share = math.Min(fillerShare, math.Max(0, (budgetPS-maintMPS)/fMPS))
		}
		if have := budgetPS + tapRate*tapMana; maintMPS > have {
			maintScale = math.Max(0, have/maintMPS)
			share = 0
		}
		wandFraction = math.Max(0, 1-used*maintScale-share-tapRate*lifeTapGCDSec)
	}

	wandDPS := WandDPS(st, tgt)
	maintDPS, maintHPS, maintLeech := maintenance(share)
	m.DPS = maintDPS*maintScale + share*fDPS + wandDPS*wandFraction
	m.HPS = maintHPS*maintScale + share*fHPS
	m.SelfHealPS = maintLeech*maintScale + share*fLeech

	// Breakdown.
	for i, p := range parts {
		row := Row{SpellID: p.act.Spell.ID, Label: p.act.Spell.Name, Kind: p.act.Kind}
		dps, hps := share*fDPS, share*fHPS
		if i == fillerIdx {
			row.SharePct = share * 100
			row.Leech = share * fLeech
		} else {
			k := (1 + share*p.haste) * maintScale
			dps, hps = p.dps*k, p.hps*maintScale
			row.SharePct = p.share * maintScale * 100
			row.Leech = p.leech * k
		}
		row.Value = dps
		if p.act.Spell.IsHeal {
			row.Value = hps
		}
		m.Breakdown = append(m.Breakdown, row)
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
		// The wand is damage: in a healing breakdown the row only says how
		// much of the fight is left without mana.
		wandValue := wandDPS * wandFraction
		if metric != "dps" {
			wandValue = 0
		}
		m.Breakdown = append(m.Breakdown, Row{
			Label:    "Wand",
			Kind:     "wand",
			Value:    wandValue,
			SharePct: wandFraction * 100,
		})
	}

	switch metric {
	case "hps":
		m.Value = m.HPS
	case "ttoom":
		m.Value = 3600 // a sustainable rotation counts as an hour, so gains stay comparable
		if m.TTOOMSec != nil && *m.TTOOMSec < 3600 {
			m.Value = *m.TTOOMSec
		}
	default:
		m.Value = m.DPS
	}
	return m
}
