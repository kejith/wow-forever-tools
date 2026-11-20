package engine

import (
	"math"
	"math/rand"
)

// The played fight: the priority rotation cast by cast. DoTs tick on their
// schedule and are refreshed when they fall off, Life Tap (or Dark
// Sacrifice) converts health on demand, a spell that returns health goes
// first when the health is too low to convert, the wand fills mana gaps,
// and spirit regeneration returns in full once five seconds pass without a
// cast. The fight ends when its time is up: a DoT cast late ticks only for
// what is left, and a cast or channel that runs past the end counts for the
// part inside the fight.
//
// With a random source every cast rolls (hit, damage range, crit): that is
// one Monte-Carlo fight. Without one every cast is worth its expectation:
// the one fight an average of many would look like, which is what the
// rotation guide shows as a timeline.

const wandSwingSec = 1.5 // swing cadence used when no speed is exported

// Event is one action of a played fight.
type Event struct {
	T       float64 `json:"t"`                 // seconds into the fight when the action starts
	Kind    string  `json:"kind"`              // the action's kind, "lifetap" or "wand"
	SpellID int     `json:"spellId,omitempty"` // 0 for the wand and for Life Tap from the spell text
	Mana    float64 `json:"mana"`              // before the action
	Health  float64 `json:"health"`
	Total   float64 `json:"total"` // damage or healing done before the action
}

// Playback is one fight played at the expectation of every cast.
type Playback struct {
	Value  float64 // the metric: damage or healing per second, or seconds until the mana is gone
	PerSec float64 // damage or healing per second
	// OOMSec is when the rotation could first not afford what it wanted to
	// cast and had nothing to convert; nil if that never happened.
	OOMSec *float64
	Events []Event
	// BySpell is the damage or healing each spell did over the fight (key 0:
	// the wand).
	BySpell map[int]float64
}

// Play runs the rotation once without dice and records what was cast when.
func Play(st *State, fight Fight, actions []Action, metric string) Playback {
	p := newPlayer(st, fight, actions, metric)
	rec := &record{bySpell: map[int]float64{}}
	value, oom := p.play(nil, rec)
	pb := Playback{Value: value, PerSec: value, Events: rec.events, BySpell: rec.bySpell}
	if oom < p.fight.Seconds {
		pb.OOMSec = &oom
	}
	if metric == "ttoom" {
		pb.Value = oom
	}
	return pb
}

// record is what a played fight notes down when asked to.
type record struct {
	events  []Event
	bySpell map[int]float64
}

// slot is one spell of the rotation with its numbers worked out.
type slot struct {
	act   Action
	shape CastShape
	cycle float64 // cooldown slots: seconds between two casts
}

// player is a rotation prepared for playing: every spell's shape is worked
// out once, the rolls stay per cast.
type player struct {
	st     *State
	fight  Fight
	metric string

	dots, cooldowns []slot
	filler, recover *slot
	channelFactor   []float64 // how much faster each DoT ticks while the filler is channeled (Wrack)
	leecher         *slot     // what is cast for health when a conversion is blocked

	conv        *converter
	netHealth   bool
	healthFloor float64
	wandDPS     float64
	regenCast   float64
	regenIdle   float64
	drainPS     float64
	fullAt      float64
	refillPS    float64
}

func newPlayer(st *State, fight Fight, actions []Action, metric string) *player {
	if fight.Seconds <= 0 {
		fight.Seconds = 60
	}
	tgt := NewTarget(st.Level, fight.TargetDelta)
	ctx := ContextOf(actions)
	p := &player{st: st, fight: fight, metric: metric}
	for _, a := range actions {
		if a.Kind == "wand" || a.Kind == "mana" || a.Spell == nil {
			continue
		}
		s := slot{act: a, shape: ShapeIn(a.Spell, st, tgt, ctx)}
		s.cycle = math.Max(s.shape.CooldownSec, a.CycleSec)
		switch a.Kind {
		case "dot", "hot":
			if s.shape.Ticks > 0 {
				p.dots = append(p.dots, s)
			}
		case "cooldown":
			if s.cycle > 0 {
				p.cooldowns = append(p.cooldowns, s)
			}
		case "filler", "heal":
			if p.filler == nil {
				f := s
				p.filler = &f
			}
		case "recover":
			if p.recover == nil {
				r := s
				p.recover = &r
			}
		}
	}
	p.channelFactor = make([]float64, len(p.dots))
	for d := range p.dots {
		p.channelFactor[d] = 1
		if p.filler != nil {
			p.channelFactor[d] += st.Mods.channelHaste(p.filler.act.Spell, p.dots[d].act.Spell) / 100
		}
	}
	leeches := func(s *slot) bool { return s != nil && (s.shape.DirectLeech > 0 || s.shape.TickLeech > 0) }
	switch {
	case leeches(p.filler):
		p.leecher = p.filler
	case leeches(p.recover):
		p.leecher = p.recover
	}
	// What converts health into mana: Life Tap, or the rotation's "mana"
	// action (Dark Sacrifice). A damage dealer may take the health down to a
	// floor that leech and hp5 refill; a healer pays the health with healing,
	// so it comes off the result, and converts only if the mana heals more
	// than the health costs.
	p.conv = converterOf(st, fight, actions)
	p.netHealth = metric != "dps"
	p.healthFloor = (1 - healthUseFraction) * st.HealthMax
	p.wandDPS = WandDPS(st, tgt)
	p.regenCast, p.regenIdle = st.Regen(fight)
	// What the full rotation drains per second beyond the regeneration. When
	// the mana will not last for the rest of the fight, the player converts
	// as soon as the health bar is full, so that no leech is wasted.
	// Full means: the next cast would return more health than fits.
	p.fullAt, p.refillPS = st.HealthMax, st.HP5/5
	if p.conv != nil {
		// The closed form of the rotation without its standby spell: the
		// player decides by the health when to cast that one.
		var plain []Action
		for _, a := range actions {
			if a.Kind != "recover" {
				plain = append(plain, a)
			}
		}
		closed := Evaluate(st, fight, plain, metric)
		p.drainPS = closed.ManaSpentPS - p.regenCast
		next := p.conv.gcd
		if p.filler != nil {
			next = p.filler.shape.ExecuteSec
		}
		p.refillPS += closed.SelfHealPS
		p.fullAt -= p.refillPS * next
		if p.netHealth && metric != "ttoom" {
			perMana := 0.0 // healing per mana of what the mana is spent on
			if p.filler != nil && p.filler.shape.ManaCost > 0 {
				perMana = p.filler.shape.SampleDirect(nil) / p.filler.shape.ManaCost
			} else {
				for _, d := range p.dots {
					if d.shape.ManaCost > 0 {
						perMana = math.Max(perMana, d.shape.Ticks*d.shape.TickValue/d.shape.ManaCost)
					}
				}
			}
			if p.conv.mana*perMana <= p.conv.health {
				p.conv = nil
			}
		}
	}
	return p
}

// lands rolls whether a cast hits; without dice it is the chance itself,
// which then weighs what the cast does.
func lands(rng *rand.Rand, chance float64) float64 {
	if rng == nil {
		return chance
	}
	if rng.Float64() < chance {
		return 1
	}
	return 0
}

// play runs one fight and returns the damage or healing per second and when
// the mana ran out (the fight's length if it never did).
func (p *player) play(rng *rand.Rand, rec *record) (perSec, oom float64) {
	st, fight, conv := p.st, p.fight, p.conv
	dots, cooldowns, filler := p.dots, p.cooldowns, p.filler
	var total float64 // damage or healing, by the metric
	mana := st.ManaMax
	health := st.HealthMax
	oom = fight.Seconds
	wentOOM := false
	lastCast := 0.0
	dotEnd := make([]float64, len(dots))
	dotRem := make([]float64, len(dots)) // seconds (at normal speed) until the next tick
	dotOn := make([]float64, len(dots))  // 1 once applied; without dice the chance that it landed
	cdReady := make([]float64, len(cooldowns))
	convReady := 0.0
	convLeft := 0.0 // seconds a conversion over time (Dark Sacrifice) still pays out
	t := 0.0

	note := func(kind string, sp *Spell) {
		if rec == nil {
			return
		}
		e := Event{T: t, Kind: kind, Mana: mana, Health: health, Total: total}
		if sp != nil {
			e.SpellID = sp.ID
		}
		rec.events = append(rec.events, e)
	}
	gain := func(sp *Spell, amount, leech float64) { // damage or healing done, and the health it returns
		if rec != nil {
			rec.bySpell[sp.ID] += amount
		}
		total += amount
		health += amount * leech
		if health > st.HealthMax {
			health = st.HealthMax
		}
	}
	// inFight is the part of a cast or channel that happens before the fight
	// ends: its effect counts for that part. An instant counts in full.
	inFight := func(sh *CastShape) float64 {
		if sh.CastSec <= 0 || t+sh.CastSec <= fight.Seconds {
			return 1
		}
		return math.Max(0, (fight.Seconds-t)/sh.CastSec)
	}
	// advance moves the clock: dots and hots tick on their own schedule
	// (faster while the filler channel says so), health regenerates.
	advance := func(dur float64, hasted bool) {
		from, to := t, t+dur
		for d := range dots {
			end := math.Min(to, math.Min(dotEnd[d], fight.Seconds))
			if end <= from {
				continue
			}
			progress := end - from
			if hasted {
				progress *= p.channelFactor[d]
			}
			for progress >= dotRem[d]-1e-9 {
				progress -= dotRem[d]
				gain(dots[d].act.Spell, dots[d].shape.TickValue*dotOn[d], dots[d].shape.TickLeech)
				dotRem[d] = dots[d].shape.PeriodSec
			}
			dotRem[d] -= progress
		}
		if convLeft > 0 { // a conversion over time: health for mana, second by second
			d := math.Min(convLeft, math.Min(to, fight.Seconds)-from)
			if d > 0 {
				part := d / conv.duration
				mana = math.Min(st.ManaMax, mana+conv.mana*part)
				if p.netHealth {
					total -= conv.health * part
				} else {
					health -= conv.health * part
				}
			}
			convLeft -= dur
		}
		health += st.HP5 / 5 * dur
		if health > st.HealthMax {
			health = st.HealthMax
		}
		t = to
	}
	spend := func(cost, dur float64, hasted bool) { // a cast: pay, regenerate, advance
		mana -= cost
		mana += p.regenCast * dur
		if mana > st.ManaMax {
			mana = st.ManaMax
		}
		lastCast = t + dur
		advance(dur, hasted)
	}
	// convert trades health for mana: at once (Life Tap), or over the
	// effect's duration (Dark Sacrifice, see advance).
	convert := func() {
		note(conv.kind, conv.spell)
		convReady = t + conv.cooldown
		if conv.duration > 0 {
			convLeft = conv.duration
		} else {
			mana += conv.mana
			if p.netHealth {
				total -= conv.health
			} else {
				health -= conv.health
			}
		}
		spend(0, conv.gcd, false)
	}
	// cast is one cast of a filler (or of the spell that returns health).
	cast := func(s *slot) {
		sh := &s.shape
		note(s.act.Kind, s.act.Spell)
		part := inFight(sh)
		gain(s.act.Spell, sh.SampleDirect(rng)*part, sh.DirectLeech)
		if sh.Ticks > 0 { // a channel: its ticks land within the cast
			if w := lands(rng, sh.HitChance); w > 0 {
				gain(s.act.Spell, sh.Ticks*sh.TickValue*w*part, sh.TickLeech)
			}
		}
		spend(sh.ManaCost, sh.ExecuteSec, s == filler && s.act.Spell.Channeled)
	}

	for t < fight.Seconds {
		acted := false
		ready := conv != nil && t >= convReady
		canTap := ready
		if ready && !p.netHealth {
			// A damage dealer keeps the health above a floor. A cost
			// spread over time may dip below it if leech and regeneration
			// bring the health back by the end of the fight.
			left := health - conv.health
			if conv.duration > 0 {
				left += p.refillPS * (fight.Seconds - t)
			}
			canTap = left >= p.healthFloor
		}
		// Too little health to convert and a spell that returns health
		// (Drain Life). As the filler it goes before everything else, or
		// mana and health both run dry. Standing by next to another filler
		// it is cast when that filler would leave too little mana for it:
		// the health it returns is the next conversion, and with that mana
		// the rotation goes on.
		if l := p.leecher; ready && !canTap && l != nil && mana >= l.shape.ManaCost && mana < p.drainPS*(fight.Seconds-t) {
			if l == filler || filler == nil || mana-filler.shape.ManaCost < l.shape.ManaCost {
				cast(l)
				continue
			}
		}
		for d := range dots { // keep every DoT/HoT up
			sh := &dots[d].shape
			if t >= dotEnd[d] && mana >= sh.ManaCost {
				note(dots[d].act.Kind, dots[d].act.Spell)
				if w := lands(rng, sh.HitChance); w > 0 {
					gain(dots[d].act.Spell, sh.SampleLanded(rng)*w*inFight(sh), sh.DirectLeech) // the direct part of Immolate, Holy Fire
					dotEnd[d] = t + sh.DurationSec
					dotRem[d] = sh.PeriodSec
					dotOn[d] = w
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
				note("cooldown", cooldowns[cd].act.Spell)
				gain(cooldowns[cd].act.Spell, sh.SampleDirect(rng)*inFight(sh), sh.DirectLeech)
				cdReady[cd] = t + cooldowns[cd].cycle
				spend(sh.ManaCost, sh.ExecuteSec, false)
				acted = true
				break
			}
		}
		if acted {
			continue
		}
		// Ahead of need a damage dealer converts when the health bar is full,
		// so that no leech is wasted on it. With a spell standing by that
		// returns health there is nothing to wait for: the lower the health,
		// the more of what that spell returns fits.
		early := p.netHealth || health >= p.fullAt || (p.recover != nil && p.leecher == p.recover)
		if canTap && early && mana+conv.mana <= st.ManaMax && mana < p.drainPS*(fight.Seconds-t) {
			convert()
			continue
		}
		if filler != nil && mana >= filler.shape.ManaCost {
			cast(filler)
			continue
		}
		// Out of mana for the filler: convert if possible, else note
		// it and take the wand. A rotation without a filler converts only
		// ahead of need (above): its mana goes to the DoTs.
		if canTap && filler != nil {
			convert()
			continue
		}
		if !wentOOM {
			wentOOM, oom = true, t
		}
		// Wand (or idle): full spirit regeneration five seconds after
		// the last cast (the five-second rule).
		note("wand", nil)
		swing := wandSwingSec
		if st.Wand.Speed > 0 {
			swing = st.Wand.Speed
		}
		if p.metric == "dps" {
			hit := p.wandDPS * math.Min(swing, fight.Seconds-t)
			total += hit
			if rec != nil {
				rec.bySpell[0] += hit
			}
		}
		if t-lastCast >= 5 {
			mana += p.regenIdle * swing
		} else {
			mana += p.regenCast * swing
		}
		if mana > st.ManaMax {
			mana = st.ManaMax
		}
		advance(swing, false)
	}
	return total / fight.Seconds, oom
}
