package engine

import (
	"math"
	"math/rand"
	"sort"
)

// The Monte-Carlo stage: the same priority rotation, played cast by cast
// over many simulated fights with real rolls (hit, crit, damage range).
// DoTs tick on their schedule and are refreshed when they fall off, Life
// Tap converts health on demand, the wand fills mana gaps, and spirit
// regeneration returns in full once five seconds pass without a cast.
//
// Evaluations share the per-iteration seed (common random numbers), so the
// difference between two gear states comes from the gear, not the dice.

// MCStats summarizes the sampled fights.
type MCStats struct {
	Iterations int     `json:"iterations"`
	Mean       float64 `json:"mean"`
	StdDev     float64 `json:"stdDev"`
	P10        float64 `json:"p10"`
	P90        float64 `json:"p90"`
}

const wandSwingSec = 1.5 // swing cadence used when no speed is exported

// RunMC plays the rotation `iterations` times and reports the metric's
// distribution. The metric follows Evaluate: dps, hps or ttoom.
func RunMC(st *State, fight Fight, actions []Action, metric string, iterations int, seed int64) MCStats {
	if fight.Seconds <= 0 {
		fight.Seconds = 60
	}
	if iterations <= 0 {
		iterations = 10000
	}
	tgt := NewTarget(st.Level, fight.TargetDelta)
	ctx := ContextOf(actions)

	// Pre-compute every spell's shape once; the rolls stay per cast.
	type slot struct {
		act   Action
		shape CastShape
	}
	var dots, cooldowns []slot
	var filler *slot
	for _, a := range actions {
		if a.Kind == "wand" || a.Spell == nil {
			continue
		}
		s := slot{act: a, shape: ShapeIn(a.Spell, st, tgt, ctx)}
		switch a.Kind {
		case "dot", "hot":
			if s.shape.Ticks > 0 {
				dots = append(dots, s)
			}
		case "cooldown":
			if s.shape.CooldownSec > 0 {
				cooldowns = append(cooldowns, s)
			}
		case "filler", "heal":
			if filler == nil {
				f := s
				filler = &f
			}
		}
	}
	// How much faster each DoT ticks while the filler is channeled (Wrack).
	channelFactor := make([]float64, len(dots))
	for d := range dots {
		channelFactor[d] = 1
		if filler != nil {
			channelFactor[d] += st.Mods.channelHaste(filler.act.Spell, dots[d].act.Spell) / 100
		}
	}
	fillerLeeches := filler != nil && (filler.shape.DirectLeech > 0 || filler.shape.TickLeech > 0)
	tapMana := st.LifeTap.ManaPerCast * (1 + st.Mods.LifeTapPct/100)
	// Life Tap may take the health down to this floor; leech and hp5 refill it.
	healthFloor := (1 - healthUseFraction) * st.HealthMax
	wandDPS := WandDPS(st, tgt)
	regenFull := st.RegenPS()
	regenCasting := st.MP5/5 + regenFull*st.Mods.CastingRegenPct/100
	// What the full rotation drains per second beyond the regeneration. When
	// the mana will not last for the rest of the fight, the player taps as
	// soon as the health bar is full, so that no leech is wasted.
	// Full means: the next cast would return more health than fits.
	drainPS, fullAt := 0.0, st.HealthMax
	if tapMana > 0 {
		closed := Evaluate(st, fight, actions, metric)
		drainPS = closed.ManaSpentPS - regenCasting
		next := lifeTapGCDSec
		if filler != nil {
			next = filler.shape.ExecuteSec
		}
		fullAt -= (closed.SelfHealPS + st.HP5/5) * next
	}

	values := make([]float64, iterations)
	oomAt := make([]float64, iterations)
	for i := 0; i < iterations; i++ {
		rng := rand.New(rand.NewSource(seed + int64(i)))
		var total float64 // damage or healing, by the metric
		mana := st.ManaMax
		health := st.HealthMax
		oom := fight.Seconds
		wentOOM := false
		lastCast := 0.0
		dotEnd := make([]float64, len(dots))
		dotRem := make([]float64, len(dots)) // seconds (at normal speed) until the next tick
		cdReady := make([]float64, len(cooldowns))
		t := 0.0

		gain := func(amount, leech float64) { // damage or healing done, and the health it returns
			total += amount
			health += amount * leech
			if health > st.HealthMax {
				health = st.HealthMax
			}
		}
		// advance moves the clock: dots and hots tick on their own schedule
		// (faster while the filler channel says so), health regenerates.
		advance := func(dur float64, channeling bool) {
			from, to := t, t+dur
			for d := range dots {
				end := math.Min(to, math.Min(dotEnd[d], fight.Seconds))
				if end <= from {
					continue
				}
				progress := end - from
				if channeling {
					progress *= channelFactor[d]
				}
				for progress >= dotRem[d]-1e-9 {
					progress -= dotRem[d]
					gain(dots[d].shape.TickValue, dots[d].shape.TickLeech)
					dotRem[d] = dots[d].shape.PeriodSec
				}
				dotRem[d] -= progress
			}
			health += st.HP5 / 5 * dur
			if health > st.HealthMax {
				health = st.HealthMax
			}
			t = to
		}
		spend := func(cost, dur float64, channeling bool) { // a cast: pay, regenerate, advance
			mana -= cost
			mana += regenCasting * dur
			if mana > st.ManaMax {
				mana = st.ManaMax
			}
			lastCast = t + dur
			advance(dur, channeling)
		}
		tap := func() {
			health -= st.LifeTap.HealthPerCast
			mana += tapMana
			spend(0, lifeTapGCDSec, false)
		}

		castFiller := func() {
			sh := &filler.shape
			gain(sh.SampleDirect(rng), sh.DirectLeech)
			if sh.Ticks > 0 { // a channel: its ticks land within the cast
				if rng.Float64() < sh.HitChance {
					gain(sh.Ticks*sh.TickValue, sh.TickLeech)
				}
			}
			spend(sh.ManaCost, sh.ExecuteSec, filler.act.Spell.Channeled)
		}

		for t < fight.Seconds {
			acted := false
			canTap := tapMana > 0 && health-st.LifeTap.HealthPerCast >= healthFloor
			// Too little health to tap and a filler that returns health
			// (Drain Life): it goes first, or mana and health both run dry.
			if tapMana > 0 && !canTap && fillerLeeches && mana >= filler.shape.ManaCost && mana < drainPS*(fight.Seconds-t) {
				castFiller()
				continue
			}
			for d := range dots { // keep every DoT/HoT up
				sh := &dots[d].shape
				if t >= dotEnd[d] && mana >= sh.ManaCost {
					if rng.Float64() < sh.HitChance {
						gain(sh.SampleLanded(rng), sh.DirectLeech) // the direct part of Immolate, Holy Fire
						dotEnd[d] = t + sh.DurationSec
						dotRem[d] = sh.PeriodSec
					}
					spend(sh.ManaCost, sh.ExecuteSec, false)
					acted = true
					break
				}
			}
			if acted {
				continue
			}
			for cd := range cooldowns {
				sh := &cooldowns[cd].shape
				if t >= cdReady[cd] && mana >= sh.ManaCost {
					gain(sh.SampleDirect(rng), sh.DirectLeech)
					cdReady[cd] = t + sh.CooldownSec
					spend(sh.ManaCost, sh.ExecuteSec, false)
					acted = true
					break
				}
			}
			if acted {
				continue
			}
			if canTap && health >= fullAt && mana+tapMana <= st.ManaMax && mana < drainPS*(fight.Seconds-t) {
				tap()
				continue
			}
			if filler != nil && mana >= filler.shape.ManaCost {
				castFiller()
				continue
			}
			// Out of mana for the filler: first note it, then tap or wand.
			if !wentOOM {
				wentOOM, oom = true, t
			}
			if canTap {
				tap()
				continue
			}
			// Wand (or idle): full spirit regeneration five seconds after
			// the last cast (the five-second rule).
			swing := wandSwingSec
			if st.Wand.Speed > 0 {
				swing = st.Wand.Speed
			}
			if metric == "dps" {
				total += wandDPS * swing
			}
			if t-lastCast >= 5 {
				mana += (st.MP5/5 + regenFull) * swing
			} else {
				mana += regenCasting * swing
			}
			if mana > st.ManaMax {
				mana = st.ManaMax
			}
			advance(swing, false)
		}
		values[i] = total / fight.Seconds
		oomAt[i] = oom
	}

	picked := values
	if metric == "ttoom" {
		picked = oomAt
	}
	return summarize(picked)
}

func summarize(values []float64) MCStats {
	n := len(values)
	s := MCStats{Iterations: n}
	if n == 0 {
		return s
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	s.Mean = sum / float64(n)
	var sq float64
	for _, v := range values {
		d := v - s.Mean
		sq += d * d
	}
	s.StdDev = math.Sqrt(sq / float64(n))
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	s.P10 = sorted[n/10]
	s.P90 = sorted[(n*9)/10]
	return s
}
