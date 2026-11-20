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
	// Kind "recover" is a second filler that returns health (Drain Life):
	// the played fight casts it when the health is too low to convert. The
	// closed form has one filler and ignores it.
	Kind  string // "dot", "hot", "cooldown", "filler", "heal", "wand", "mana", "recover"
	Spell *Spell
	// CycleSec is the least time between two casts when something other
	// than the spell's own cooldown limits it: Weakened Soul after Power
	// Word: Shield on the one target the model heals.
	CycleSec float64
	// Kind "mana" is a spell that trades health for mana (Dark Sacrifice),
	// cast on demand like Life Tap: it returns Spell.Energize per tick over
	// its duration plus ManaPerSpirit times the spirit, and costs HealthCost
	// per tick.
	HealthCost    *Effect
	ManaPerSpirit float64
}

// converter is what turns health into mana when the rotation runs short:
// the Warlock's Life Tap (numbers from the spell text in the snapshot) or a
// "mana" action of the rotation.
type converter struct {
	kind     string  // row kind: "lifetap" or "mana"
	spell    *Spell  // nil for Life Tap
	mana     float64 // per cast
	health   float64 // per cast
	gcd      float64
	duration float64 // seconds over which the mana arrives; 0 = at once
	cooldown float64 // seconds; 0 = none
	maxRate  float64 // casts per second the cooldown allows over the fight; 0 = no limit
}

func converterOf(st *State, fight Fight, actions []Action) *converter {
	if mana := st.LifeTap.ManaPerCast * (1 + st.Mods.LifeTapPct/100); mana > 0 {
		return &converter{kind: "lifetap", mana: mana, health: st.LifeTap.HealthPerCast, gcd: lifeTapGCDSec}
	}
	for _, a := range actions {
		if a.Kind != "mana" || a.Spell == nil || a.Spell.Energize == nil {
			continue
		}
		sp := a.Spell
		ticks := 1.0
		duration := float64(sp.DurationMs) / 1000
		if sp.Energize.PeriodMs > 0 && sp.DurationMs > 0 {
			ticks = float64(sp.DurationMs) / float64(sp.Energize.PeriodMs)
		}
		c := &converter{kind: "mana", spell: sp, gcd: lifeTapGCDSec, duration: duration, cooldown: CooldownSec(sp, st)}
		if sp.GCDMs > 0 {
			c.gcd = float64(sp.GCDMs) / 1000
		}
		c.mana = ticks*sp.Energize.avgBase(st.Level, sp.Levels) + a.ManaPerSpirit*st.Spirit
		if a.HealthCost != nil {
			c.health = ticks * a.HealthCost.avgBase(st.Level, sp.Levels)
		}
		if c.cooldown < duration {
			c.cooldown = duration // it cannot overlap itself
		}
		if c.mana <= 0 || c.cooldown <= 0 {
			continue
		}
		// Casts that pay out within the fight: the first right away, another
		// one per cooldown; a fight shorter than the effect gets its share.
		casts := fight.Seconds / c.cooldown
		if duration > 0 {
			if fight.Seconds < duration {
				casts = fight.Seconds / duration
			} else {
				casts = 1 + math.Floor((fight.Seconds-duration)/c.cooldown)
			}
		}
		c.maxRate = casts / fight.Seconds
		return c
	}
	return nil
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
	Absorb   bool    `json:"absorb,omitempty"` // the value is damage a shield absorbs
	// Rows that trade health for mana (Life Tap, Dark Sacrifice), per second
	// of the fight. A healer's health cost is taken off the healing.
	ManaGain   float64 `json:"manaGain,omitempty"`
	HealthCost float64 `json:"healthCost,omitempty"`
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
	// KillBuffPct is the share of the fight a buff from kills is up (Spirit
	// Tap), by the kills the fight assumes.
	KillBuffPct float64  `json:"killBuffPct,omitempty"`
	Breakdown   []Row    `json:"breakdown"`
	MC          *MCStats `json:"mc,omitempty"` // set when the Monte-Carlo runs played the rotation
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
		cycle float64 // seconds between two casts
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
		if a.Kind == "wand" || a.Kind == "mana" || a.Kind == "recover" || a.Spell == nil {
			continue
		}
		res := ResolveCastIn(a.Spell, st, tgt, ctx, nil)
		p := part{act: a, res: res}
		switch a.Kind {
		case "dot", "hot", "cooldown":
			p.cycle = res.DurationSec
			if a.Kind == "cooldown" {
				p.cycle = math.Max(res.CooldownSec, a.CycleSec)
			}
			if p.cycle <= 0 {
				continue
			}
			p.share = res.ExecuteSec / p.cycle
			p.dps = res.Damage / p.cycle
			p.hps = res.Healing / p.cycle
			p.mps = res.ManaCost / p.cycle
			p.leech = res.Leech / p.cycle
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
				cycle := parts[i].cycle
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
	// of spirit regeneration that continues while casting (Meditation, and
	// Spirit Tap for the share of the fight it is up). The model casts for
	// the whole fight, so the rest of the spirit regeneration counts 0 under
	// the five-second rule.
	regenPS, _ := st.Regen(fight)
	m.KillBuffPct = st.KillProcUptime(fight) * 100
	budgetPS := st.ManaMax/fight.Seconds + regenPS
	m.ManaSpentPS = rotMPS
	m.ManaBudgetPS = budgetPS

	conv := converterOf(st, fight, actions)
	// A healer pays the health with healing (their own, or someone else's):
	// it comes off the result. A damage dealer has a health budget instead.
	netHealth := metric != "dps"

	// Time to OOM at the full rotation. Life Tap does not count (it would
	// never end); a conversion with a cooldown does: one cast right away and
	// one more each time the cooldown ends before the mana does.
	ttoomCasts := 0.0
	if drain := rotMPS - regenPS; drain > 0 {
		t := st.ManaMax / drain
		sustained := false
		if conv != nil && conv.cooldown > 0 {
			if conv.mana/conv.cooldown >= drain {
				sustained = true
			}
			for k := 1; !sustained && t > float64(k-1)*conv.cooldown; k++ {
				t = (st.ManaMax + float64(k)*conv.mana) / drain
				ttoomCasts = float64(k)
			}
		}
		if !sustained {
			m.TTOOMSec = &t
		}
	}

	// How much of the cycle is affordable. The DoTs, HoTs and cooldowns keep
	// their priority: a conversion (Life Tap, Dark Sacrifice) takes its time
	// from the filler, and mana that is still missing is the filler's too.
	// Only when not even the maintenance is affordable does it shrink as well.
	type plan struct {
		share      float64 // the filler's real share of the fight
		maintScale float64
		rate       float64 // conversions per second of the fight
		gcd        float64
		wand       float64
	}
	solve := func(c *converter) plan {
		p := plan{share: fillerShare, maintScale: 1}
		deficit := rotMPS - budgetPS
		if deficit <= 0 {
			return p
		}
		gained := 0.0
		if c != nil {
			p.gcd = c.gcd
			// x casts per second replace x*gcd of filler time and must
			// cover what the remaining cycle lacks.
			x := deficit / (c.mana + fMPS*c.gcd)
			// The health budget caps them: the share of the pool the model
			// spends, hp5, and what the leech spells return. The leech
			// depends on the filler's share, which depends on the casts,
			// so the cap is found by iteration.
			healthPS := healthUseFraction*st.HealthMax/fight.Seconds + st.HP5/5
			for iter := 0; iter < 50; iter++ {
				if most := (1 - used) / c.gcd; x > most {
					x = most
				}
				if c.maxRate > 0 && x > c.maxRate {
					x = c.maxRate
				}
				p.share = math.Max(0, fillerShare-x*c.gcd)
				if fMPS > 0 {
					if afford := (budgetPS + x*c.mana - maintMPS) / fMPS; afford < p.share {
						p.share = math.Max(0, afford)
					}
				}
				if c.health <= 0 || netHealth {
					break
				}
				_, _, leech := maintenance(p.share)
				most := (healthPS + leech + p.share*fLeech) / c.health
				if x <= most+1e-12 {
					break
				}
				x = most
			}
			p.rate = x
			gained = x * c.mana
		} else if fMPS > 0 {
			p.share = math.Min(fillerShare, math.Max(0, (budgetPS-maintMPS)/fMPS))
		}
		if have := budgetPS + gained; maintMPS > have {
			p.maintScale = math.Max(0, have/maintMPS)
			p.share = 0
		}
		p.wand = math.Max(0, 1-used*p.maintScale-p.share-p.rate*p.gcd)
		return p
	}
	healing := func(p plan, c *converter) float64 {
		_, hps, _ := maintenance(p.share)
		hps = hps*p.maintScale + p.share*fHPS
		if c != nil && netHealth {
			hps -= p.rate * c.health
		}
		return hps
	}
	chosen := solve(conv)
	if conv != nil && netHealth {
		// A healer converts only if the mana heals more than the health costs.
		if without := solve(nil); healing(without, nil) >= healing(chosen, conv) {
			chosen = without
		}
	}
	share, maintScale, wandFraction := chosen.share, chosen.maintScale, chosen.wand

	wandDPS := WandDPS(st, tgt)
	maintDPS, maintHPS, maintLeech := maintenance(share)
	m.DPS = maintDPS*maintScale + share*fDPS + wandDPS*wandFraction
	m.HPS = maintHPS*maintScale + share*fHPS
	if conv != nil && netHealth {
		m.HPS -= chosen.rate * conv.health
	}
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
		if p.act.Spell.Heals() {
			row.Value = hps
			row.Absorb = p.act.Spell.Absorb != nil && !p.act.Spell.IsHeal
		}
		m.Breakdown = append(m.Breakdown, row)
	}
	if conv != nil {
		rate := chosen.rate
		if rate == 0 && metric == "ttoom" && m.TTOOMSec != nil && *m.TTOOMSec > 0 {
			rate = ttoomCasts / *m.TTOOMSec // the casts that stretch the mana
		}
		if rate > 0 {
			row := Row{
				Label:      "Life Tap",
				Kind:       conv.kind,
				SharePct:   rate * conv.gcd * 100,
				PerMin:     rate * 60,
				ManaGain:   rate * conv.mana,
				HealthCost: rate * conv.health,
			}
			if conv.spell != nil {
				row.SpellID, row.Label = conv.spell.ID, conv.spell.Name
			}
			m.Breakdown = append(m.Breakdown, row)
		}
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
