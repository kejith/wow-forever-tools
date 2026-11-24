package engine

import (
	"math"
	"math/rand/v2"
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
// Against several enemies a DoT or a debuff is kept on each of them (the
// first DoT on every enemy, then the second), an area spell hits them all
// with one cast, and every other spell is cast at the first enemy. A channel
// that damages its caster (Hellfire) goes in front of the filler for as many
// ticks as the health above the floor pays for.
//
// With a random source every cast rolls (hit, damage range, crit): that is
// one Monte-Carlo fight. Without one every cast is worth its expectation:
// the one fight an average of many would look like, which is what the
// rotation guide shows as a timeline.

const wandSwingSec = 1.5 // swing cadence used when no speed is exported

// minBurnShare is the least part of a channel that costs health (Hellfire)
// worth paying its mana for.
const minBurnShare = 1.0 / 3

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
// out once (without static debuff/DoT-stack multipliers, which playWith
// evaluates dynamically against the live target state), the rolls stay per cast.
type player struct {
	st     *State
	fight  Fight
	metric string

	enemies         int
	dots, cooldowns []slot
	filler, recover *slot
	procCast        *slot // Nightfall instant Shadow Bolt when another filler is active
	procCastBeats   bool  // whether casting instant procCast beats the active filler
	burn            *slot // a channel that costs health (Hellfire)
	channelFactor   []float64
	leecher         *slot // what is cast for health when a conversion is blocked
	hasStackProcs   bool
	hasCritDebuffs  bool
	hasInstantProcs bool

	conv        *converter
	netHealth   bool
	healthFloor float64 // Life Tap keeps the health above it
	burnFloor   float64 // a channel that costs health stops there
	wandDPS     float64
	passiveDPS  float64
	resourceMax float64
	startRes    float64
	nonMana     bool
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
	p := &player{
		st: st, fight: fight, metric: metric, enemies: 1,
		hasStackProcs:   metric == "dps" && len(st.Mods.StackProcs) > 0,
		hasCritDebuffs:  metric == "dps" && len(st.Mods.CritDebuffs) > 0,
		hasInstantProcs: metric == "dps" && len(st.Mods.InstantProcs) > 0,
	}
	if metric == "dps" {
		p.enemies = fight.Enemies()
	}
	autoAtk, autoShot, pet := 0.0, 0.0, 0.0
	if metric == "dps" {
		autoAtk, autoShot, pet = passiveSources(st, tgt, actions)
		p.passiveDPS = autoAtk + autoShot + pet
	}
	usesRage, usesEnergy := usesNonMana(actions)
	p.nonMana = usesRage || usesEnergy
	for _, a := range actions {
		if a.Kind == "wand" || a.Kind == "mana" || a.Kind == "autoattack" || a.Kind == "autoshot" || a.Kind == "pet" || a.Spell == nil {
			continue
		}
		// Base shape without static Context: debuffs and DoT-stacks are
		// evaluated dynamically per target during playback.
		s := slot{act: a, shape: ShapeIn(a.Spell, st, tgt, nil)}
		if usesEnergy {
			s.shape.ManaCost = s.shape.EnergyCost
		} else if usesRage {
			s.shape.ManaCost = s.shape.RageCost
		}
		s.cycle = math.Max(s.shape.CooldownSec, a.CycleSec)
		switch a.Kind {
		case "dot", "hot":
			if s.shape.Ticks > 0 {
				p.dots = append(p.dots, s)
			}
		case "debuff": // kept up like a DoT, without ticks
			if s.shape.DurationSec = DebuffSec(a.Spell, st); s.shape.DurationSec > 0 {
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
		case "proc_cast":
			if p.procCast == nil {
				pc := s
				p.procCast = &pc
			}
		case "burn":
			if p.burn == nil && s.shape.Ticks > 0 && s.shape.SelfTick > 0 && p.enemies > 1 {
				b := s
				p.burn = &b
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
	if p.hasInstantProcs && p.procCast != nil && p.procCast.shape.GCDSec > 0 {
		instantDPS := p.procCast.shape.SampleDirect(nil) / p.procCast.shape.GCDSec
		fillerDPS := 0.0
		if p.filler != nil && p.filler.shape.ExecuteSec > 0 {
			fsh := &p.filler.shape
			fillerDPS = (fsh.SampleDirect(nil) + fsh.Ticks*fsh.TickValue*fsh.HitChance) / fsh.ExecuteSec
			for d := range p.dots {
				if p.channelFactor[d] > 1 && p.dots[d].shape.PeriodSec > 0 {
					fillerDPS += (p.dots[d].shape.TickValue / p.dots[d].shape.PeriodSec) * (p.channelFactor[d] - 1)
				}
			}
		}
		p.procCastBeats = instantDPS > fillerDPS
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
	p.burnFloor = (1 - burnUseFraction) * st.HealthMax
	p.wandDPS = WandDPS(st, tgt)
	p.regenCast, p.regenIdle = st.Regen(fight)
	p.resourceMax, p.startRes = st.ManaMax, st.ManaMax
	if usesEnergy {
		p.wandDPS = 0
		p.resourceMax, p.startRes = 100, 100
		p.regenCast, p.regenIdle = 10.0, 10.0
	} else if usesRage {
		p.wandDPS = 0
		p.resourceMax, p.startRes = 100, 25
		r := rageBudgetPS(st.Level, autoAtk)
		p.regenCast, p.regenIdle = r, r
	}
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

// playScratch holds per-fight mutable state so Monte-Carlo workers reuse
// slices across iterations with zero per-iteration heap allocations.
type playScratch struct {
	dotOff     []int
	dotCnt     []int
	dotEnd     []float64
	dotRem     []float64
	dotOn      []float64
	dotReady   []float64
	cdReady    []float64
	swStacks   []float64 // [procIdx * enemies + target]
	swEnd      []float64
	isbCharges []float64 // [critDebuffIdx * enemies + target]
	isbEnd     []float64
	nfEnd      []float64 // [instantProcIdx]
	nfAcc      []float64 // deterministic fractional proc accumulator
}

func (p *player) newScratch() playScratch {
	sc := playScratch{
		dotOff:   make([]int, len(p.dots)),
		dotCnt:   make([]int, len(p.dots)),
		dotReady: make([]float64, len(p.dots)),
		cdReady:  make([]float64, len(p.cooldowns)),
	}
	total := 0
	for d := range p.dots {
		n := KeptOn(p.enemies, p.dots[d].shape.DurationSec, p.dots[d].shape.CooldownSec)
		sc.dotOff[d] = total
		sc.dotCnt[d] = n
		total += n
	}
	sc.dotEnd = make([]float64, total)
	sc.dotRem = make([]float64, total)
	sc.dotOn = make([]float64, total)
	if p.hasStackProcs {
		n := len(p.st.Mods.StackProcs) * p.enemies
		sc.swStacks = make([]float64, n)
		sc.swEnd = make([]float64, n)
	}
	if p.hasCritDebuffs {
		n := len(p.st.Mods.CritDebuffs) * p.enemies
		sc.isbCharges = make([]float64, n)
		sc.isbEnd = make([]float64, n)
	}
	if p.hasInstantProcs {
		n := len(p.st.Mods.InstantProcs)
		sc.nfEnd = make([]float64, n)
		sc.nfAcc = make([]float64, n)
	}
	return sc
}

func (sc *playScratch) reset() {
	clear(sc.dotEnd)
	clear(sc.dotRem)
	clear(sc.dotOn)
	clear(sc.dotReady)
	clear(sc.cdReady)
	clear(sc.swStacks)
	clear(sc.swEnd)
	clear(sc.isbCharges)
	clear(sc.isbEnd)
	clear(sc.nfEnd)
	clear(sc.nfAcc)
}

// play runs one fight and returns the damage or healing per second and when
// the mana ran out (the fight's length if it never did).
func (p *player) play(rng *rand.Rand, rec *record) (perSec, oom float64) {
	sc := p.newScratch()
	return p.playWith(rng, rec, &sc)
}

func (p *player) playWith(rng *rand.Rand, rec *record, sc *playScratch) (perSec, oom float64) {
	sc.reset()
	st, fight, conv := p.st, p.fight, p.conv
	dots, cooldowns, filler := p.dots, p.cooldowns, p.filler
	var total float64 // damage or healing, by the metric
	if p.metric == "dps" && p.passiveDPS > 0 {
		total = p.passiveDPS * fight.Seconds
	}
	mana := p.startRes
	health := st.HealthMax
	oom = fight.Seconds
	wentOOM := false
	lastCast := 0.0
	dotOff, dotCnt := sc.dotOff, sc.dotCnt
	dotEnd, dotRem, dotOn := sc.dotEnd, sc.dotRem, sc.dotOn
	dotReady := sc.dotReady
	cdReady := sc.cdReady
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

	// Dynamic damage multiplier on target k at time now: live debuffs
	// (Curse of the Elements), live DoT-stack bonuses (Soul Siphon, Power
	// in Light), stacking vulnerability (Shadow Weaving), and crit debuffs
	// (Improved Shadow Bolt, consuming charges).
	liveMult := func(sp *Spell, scSchool School, target int, now float64, consumeCrit bool) float64 {
		if sp.IsHeal {
			return 1
		}
		mult := 1.0
		// Live debuffs on this target (or enemy 0 when debuff only tracks 1 target).
		takenPct := 0.0
		for d := range dots {
			if dots[d].act.Kind != "debuff" {
				continue
			}
			if dots[d].act.TakenMask&(1<<uint(scSchool)) == 0 {
				continue
			}
			k := target
			if k >= dotCnt[d] {
				continue
			}
			idx := dotOff[d] + k
			if dotEnd[idx] > now+1e-9 && dotOn[idx] > 0 {
				takenPct += dots[d].act.TakenPct * dotOn[idx]
			}
		}
		if takenPct != 0 {
			mult *= 1 + takenPct/100
		}
		// Live DoT-stack talents (Soul Siphon, Power in Light).
		if len(st.Mods.DotStacks) > 0 {
			for _, ds := range st.Mods.DotStacks {
				if !ds.Affects[sp.BaseID] {
					continue
				}
				n := 0.0
				for d := range dots {
					if dots[d].act.Kind != "dot" || !ds.Counts[dots[d].act.Spell.BaseID] {
						continue
					}
					if target < dotCnt[d] {
						idx := dotOff[d] + target
						if dotEnd[idx] > now+1e-9 && dotOn[idx] > 0 {
							n += dotOn[idx]
						}
					}
				}
				if ds.Max > 0 && n > float64(ds.Max) {
					n = float64(ds.Max)
				}
				mult *= 1 + ds.PctPerDot*n/100
			}
		}
		// Live stacking procs (Shadow Weaving).
		if p.hasStackProcs {
			for i, proc := range st.Mods.StackProcs {
				if proc.SchoolMask&(1<<uint(scSchool)) == 0 {
					continue
				}
				idx := i*p.enemies + target
				if sc.swEnd[idx] > now+1e-9 && sc.swStacks[idx] > 0 {
					mult *= 1 + sc.swStacks[idx]*proc.PctPerStack/100
				}
			}
		}
		// Live crit debuffs (Improved Shadow Bolt).
		if p.hasCritDebuffs {
			for i, cd := range st.Mods.CritDebuffs {
				if cd.SchoolMask&(1<<uint(scSchool)) == 0 {
					continue
				}
				idx := i*p.enemies + target
				if sc.isbEnd[idx] > now+1e-9 && sc.isbCharges[idx] > 1e-9 {
					share := math.Min(1, sc.isbCharges[idx])
					mult *= 1 + share*cd.Pct/100
					if consumeCrit {
						sc.isbCharges[idx] = math.Max(0, sc.isbCharges[idx]-share)
					}
				}
			}
		}
		return mult
	}

	// onSpellLand updates stacking procs (Shadow Weaving) on target k.
	onSpellLand := func(scSchool School, target int, now, weight float64) {
		if !p.hasStackProcs || weight <= 0 {
			return
		}
		for i, proc := range st.Mods.StackProcs {
			if proc.SchoolMask&(1<<uint(scSchool)) == 0 {
				continue
			}
			idx := i*p.enemies + target
			if sc.swEnd[idx] <= now+1e-9 {
				sc.swStacks[idx] = 0
			}
			chance := math.Min(1, proc.ChancePct/100) * weight
			if rng != nil {
				if rng.Float64() < chance {
					if sc.swStacks[idx] < float64(proc.MaxStacks) {
						sc.swStacks[idx]++
					}
					sc.swEnd[idx] = now + proc.DurationSec
				}
			} else if chance > 0 {
				sc.swStacks[idx] = math.Min(float64(proc.MaxStacks), sc.swStacks[idx]+chance)
				sc.swEnd[idx] = now + proc.DurationSec
			}
		}
	}

	// onCritDebuff refreshes Improved Shadow Bolt when TriggerBase crits.
	onCritDebuff := func(sp *Spell, target int, now, critWeight float64) {
		if !p.hasCritDebuffs || critWeight <= 0 {
			return
		}
		for i, cd := range st.Mods.CritDebuffs {
			if sp.BaseID != cd.TriggerBase {
				continue
			}
			idx := i*p.enemies + target
			if sc.isbEnd[idx] <= now+1e-9 {
				sc.isbCharges[idx] = 0
			}
			if rng != nil {
				sc.isbCharges[idx] = float64(cd.Charges)
				sc.isbEnd[idx] = now + cd.DurationSec
			} else {
				// Deterministic expectation: a crit with probability critWeight sets Charges.
				sc.isbCharges[idx] = math.Min(float64(cd.Charges), sc.isbCharges[idx]+critWeight*float64(cd.Charges))
				sc.isbEnd[idx] = now + cd.DurationSec
			}
		}
	}

	// onTickProc checks Nightfall on Corruption / Drain Life ticks.
	onTickProc := func(sp *Spell, now, weight float64) {
		if !p.hasInstantProcs || weight <= 0 {
			return
		}
		for i, ip := range st.Mods.InstantProcs {
			if !ip.FromBases[sp.BaseID] {
				continue
			}
			chance := ip.ChancePct / 100 * weight
			if rng != nil {
				if rng.Float64() < chance {
					sc.nfEnd[i] = now + ip.DurationSec
				}
			} else {
				sc.nfAcc[i] += chance
				if sc.nfAcc[i] >= 1.0-1e-9 {
					sc.nfAcc[i] -= 1.0
					sc.nfEnd[i] = now + ip.DurationSec
				}
			}
		}
	}

	// consumeInstant reports whether an instant-cast proc is ready for castBase
	// and consumes it.
	consumeInstant := func(castBase int, now float64) bool {
		if !p.hasInstantProcs {
			return false
		}
		for i, ip := range st.Mods.InstantProcs {
			if ip.CastBase == castBase && sc.nfEnd[i] > now+1e-9 {
				sc.nfEnd[i] = 0
				return true
			}
		}
		return false
	}

	// hasInstant reports whether an instant-cast proc is active for castBase.
	hasInstant := func(castBase int, now float64) bool {
		if !p.hasInstantProcs {
			return false
		}
		for i, ip := range st.Mods.InstantProcs {
			if ip.CastBase == castBase && sc.nfEnd[i] > now+1e-9 {
				return true
			}
		}
		return false
	}

	// advance moves the clock: dots and hots tick on their own schedule
	// (faster while the filler channel says so), health regenerates.
	advance := func(dur float64, hasted bool) {
		from, to := t, t+dur
		for d := range dots {
			sh := &dots[d].shape
			if sh.PeriodSec <= 0 {
				continue
			}
			off, cnt := dotOff[d], dotCnt[d]
			for k := 0; k < cnt; k++ {
				idx := off + k
				end := math.Min(to, math.Min(dotEnd[idx], fight.Seconds))
				if end <= from {
					continue
				}
				speed := 1.0
				if hasted && k == 0 { // the channel is on the first enemy
					speed = p.channelFactor[d]
				}
				progress := (end - from) * speed
				tickT := from
				for progress >= dotRem[idx]-1e-9 {
					tickT += dotRem[idx] / speed
					progress -= dotRem[idx]
					mult := liveMult(dots[d].act.Spell, sh.School, k, tickT, true)
					gain(dots[d].act.Spell, sh.TickValue*dotOn[idx]*mult, sh.TickLeech)
					onTickProc(dots[d].act.Spell, tickT, dotOn[idx])
					dotRem[idx] = sh.PeriodSec
				}
				dotRem[idx] -= progress
			}
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
	// landDirect resolves a direct hit on target k at time now, updating
	// Shadow Weaving and Improved Shadow Bolt.
	landDirect := func(sp *Spell, sh *CastShape, target int, now float64) {
		if sh.DirectAvg == 0 {
			return
		}
		if rng != nil {
			if rng.Float64() >= sh.HitChance {
				return
			}
			val := sh.DirectAvg
			if sh.DirectVariance > 0 {
				span := val * sh.DirectVariance
				val = val - span/2 + rng.Float64()*span
			}
			crit := rng.Float64() < sh.CritPct/100
			if crit {
				val *= 1 + sh.CritBonus
			}
			mult := liveMult(sp, sh.School, target, now, true)
			gain(sp, val*mult, sh.DirectLeech)
			onSpellLand(sh.School, target, now, 1)
			if crit {
				onCritDebuff(sp, target, now, 1)
			}
			return
		}
		mult := liveMult(sp, sh.School, target, now, true)
		val := sh.DirectAvg * (1 + sh.CritPct/100*sh.CritBonus) * sh.HitChance * mult
		gain(sp, val, sh.DirectLeech)
		onSpellLand(sh.School, target, now, sh.HitChance)
		onCritDebuff(sp, target, now, sh.HitChance*sh.CritPct/100)
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
	// cast is one cast of a filler (or of the spell that returns health, or
	// an instant proc_cast). Direct casts with a cast time land at cast end
	// and deal 0 if cut off before finishing; channels tick discretely across
	// their duration so leech and end-of-fight cutoffs match the real timing.
	cast := func(s *slot, instant bool) {
		sh := &s.shape
		note(s.act.Kind, s.act.Spell)
		hits := 1
		if sh.Area {
			hits = p.enemies
		}
		// Channeled spell: tick discretely across PeriodSec steps.
		if s.act.Spell.Channeled && sh.Ticks > 0 && sh.PeriodSec > 0 {
			hasted := s == filler
			// Pay mana up front; channel ticks land at each period boundary.
			mana -= sh.ManaCost
			if mana < 0 {
				mana = 0
			}
			nTicks := int(math.Round(sh.Ticks))
			var hitWeights [MaxTargets]float64
			for e := 0; e < hits; e++ {
				hitWeights[e] = lands(rng, sh.HitChance)
			}
			elapsed := 0.0
			for tick := 0; tick < nTicks && t < fight.Seconds-1e-9; tick++ {
				step := sh.PeriodSec
				if t+step > fight.Seconds+1e-9 {
					// Partial last tick does not land; advance remaining fight time so active DoTs finish.
					rem := fight.Seconds - t
					mana = math.Min(st.ManaMax, mana+p.regenCast*rem)
					lastCast = fight.Seconds
					advance(rem, hasted)
					return
				}
				mana = math.Min(st.ManaMax, mana+p.regenCast*step)
				lastCast = t + step
				advance(step, hasted)
				elapsed += step
				for e := 0; e < hits; e++ {
					if w := hitWeights[e]; w > 0 {
						mult := liveMult(s.act.Spell, sh.School, e, t, true)
						gain(s.act.Spell, sh.TickValue*w*mult, sh.TickLeech)
						if tick == 0 {
							onSpellLand(sh.School, e, t, w)
						}
						onTickProc(s.act.Spell, t, w)
					}
				}
			}
			if rem := sh.ExecuteSec - elapsed; rem > 1e-9 {
				mana = math.Min(st.ManaMax, mana+p.regenCast*rem)
				lastCast = t + rem
				advance(rem, false)
			}
			return
		}

		// Direct spell (instant or casted).
		castTime := sh.CastSec
		execTime := sh.ExecuteSec
		if instant {
			castTime = 0
			execTime = sh.GCDSec
		}
		if castTime > 0 && t+castTime > fight.Seconds+1e-9 {
			// Cast does not finish before the fight ends: 0 damage from this cast,
			// active DoTs still tick out the remaining seconds.
			rem := fight.Seconds - t
			mana = math.Min(st.ManaMax, mana+p.regenCast*rem)
			advance(rem, false)
			return
		}
		if castTime > 0 {
			// Cast time passes first (mana paid on completion, active DoTs tick).
			mana = math.Min(st.ManaMax, mana+p.regenCast*castTime)
			advance(castTime, false)
		}
		mana -= sh.ManaCost
		if mana < 0 {
			mana = 0
		}
		lastCast = t
		for e := 0; e < hits; e++ {
			landDirect(s.act.Spell, sh, e, t)
		}
		if rem := execTime - castTime; rem > 1e-9 {
			mana = math.Min(st.ManaMax, mana+p.regenCast*rem)
			lastCast = t + rem
			advance(rem, false)
		}
	}
	// burnTicks is how many ticks of the channel that costs health are worth
	// starting now: what the health above its floor pays for and the fight
	// has time for. A channel pays its mana in full at the start, so less
	// than a third of it is not started.
	burnTicks := func() float64 {
		sh := &p.burn.shape
		ticks := math.Min(sh.Ticks, math.Floor((health-p.burnFloor)/sh.SelfTick))
		ticks = math.Min(ticks, math.Floor((fight.Seconds-t+1e-9)/sh.PeriodSec))
		if ticks < math.Ceil(sh.Ticks*minBurnShare) {
			return 0
		}
		return ticks
	}
	// channelBurn channels it for that many ticks: every tick hits every
	// enemy and the caster discretely.
	channelBurn := func(ticks float64) {
		s := p.burn
		sh := &s.shape
		note("burn", s.act.Spell)
		mana -= sh.ManaCost
		if mana < 0 {
			mana = 0
		}
		var hitWeights [MaxTargets]float64
		for e := 0; e < p.enemies; e++ {
			hitWeights[e] = lands(rng, sh.HitChance)
		}
		nTicks := int(ticks)
		elapsed := 0.0
		for tick := 0; tick < nTicks && t < fight.Seconds-1e-9; tick++ {
			step := sh.PeriodSec
			if t+step > fight.Seconds+1e-9 {
				rem := fight.Seconds - t
				mana = math.Min(st.ManaMax, mana+p.regenCast*rem)
				lastCast = fight.Seconds
				advance(rem, false)
				return
			}
			mana = math.Min(st.ManaMax, mana+p.regenCast*step)
			lastCast = t + step
			advance(step, false)
			elapsed += step
			for e := 0; e < p.enemies; e++ {
				if w := hitWeights[e]; w > 0 {
					mult := liveMult(s.act.Spell, sh.School, e, t, true)
					gain(s.act.Spell, sh.TickValue*w*mult, sh.TickLeech)
					if tick == 0 {
						onSpellLand(sh.School, e, t, w)
					}
				}
			}
			health -= sh.SelfTick
		}
		if rem := sh.GCDSec - elapsed; rem > 1e-9 {
			mana = math.Min(st.ManaMax, mana+p.regenCast*rem)
			lastCast = t + rem
			advance(rem, false)
		}
	}

	for t < fight.Seconds-1e-9 {
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
				cast(l, false)
				continue
			}
		}
		// Nightfall proc when another filler is active and instant Shadow Bolt beats it.
		if p.procCastBeats && p.procCast != nil && mana >= p.procCast.shape.ManaCost &&
			hasInstant(p.procCast.act.Spell.BaseID, t) {
			consumeInstant(p.procCast.act.Spell.BaseID, t)
			cast(p.procCast, true)
			continue
		}
		for d := range dots { // keep every DoT/HoT up, on every enemy
			sh := &dots[d].shape
			off, cnt := dotOff[d], dotCnt[d]
			for k := 0; k < cnt; k++ {
				idx := off + k
				if t < dotEnd[idx]-1e-9 || mana < sh.ManaCost || (cnt > 1 && t < dotReady[d]-1e-9) {
					continue
				}
				// Casted DoT (Immolate, Holy Fire): cannot finish if castTime exceeds remaining fight.
				if sh.CastSec > 0 && t+sh.CastSec > fight.Seconds+1e-9 {
					continue
				}
				note(dots[d].act.Kind, dots[d].act.Spell)
				dotReady[d] = t + sh.CooldownSec
				if rng != nil {
					// Monte-Carlo: cast time passes before DoT/direct lands; a miss simply
					// leaves dotEnd untouched so the next GCD retries naturally.
					if sh.CastSec > 0 {
						mana = math.Min(st.ManaMax, mana+p.regenCast*sh.CastSec)
						advance(sh.CastSec, false)
					}
					mana -= sh.ManaCost
					if mana < 0 {
						mana = 0
					}
					lastCast = t
					if rng.Float64() < sh.HitChance {
						if sh.DirectAvg > 0 {
							val := sh.SampleLanded(rng) * liveMult(dots[d].act.Spell, sh.School, k, t, true)
							gain(dots[d].act.Spell, val, sh.DirectLeech)
						}
						dotEnd[idx], dotRem[idx], dotOn[idx] = t+sh.DurationSec, sh.PeriodSec, 1
						onSpellLand(sh.School, k, t, 1)
					}
					if rem := sh.ExecuteSec - sh.CastSec; rem > 1e-9 {
						mana = math.Min(st.ManaMax, mana+p.regenCast*rem)
						lastCast = t + rem
						advance(rem, false)
					}
				} else {
					// Deterministic playback: model expected recast cost (1 / HitChance) so a
					// missed DoT costs extra GCDs & mana instead of ticking weakened for 18-24s.
					tries := 1.0
					dotWeight := sh.HitChance
					if sh.HitChance > 0 && sh.HitChance < 1 {
						wantTries := 1.0 / sh.HitChance
						maxTries := wantTries
						if sh.ManaCost > 0 {
							if afford := mana / sh.ManaCost; afford < maxTries {
								maxTries = math.Max(1, afford)
							}
						}
						tries = maxTries
						// Probability of landing within `tries` attempts (1.0 when full 1/HitChance is affordable).
						if tries >= wantTries-1e-9 {
							dotWeight = 1.0
						} else {
							dotWeight = 1 - math.Pow(1-sh.HitChance, tries)
						}
					}
					if sh.CastSec > 0 {
						mana = math.Min(st.ManaMax, mana+p.regenCast*sh.CastSec)
						advance(sh.CastSec, false)
					}
					if sh.DirectAvg > 0 {
						val := sh.SampleLanded(nil) * dotWeight * liveMult(dots[d].act.Spell, sh.School, k, t, true)
						gain(dots[d].act.Spell, val, sh.DirectLeech)
					}
					dotEnd[idx], dotRem[idx], dotOn[idx] = t+sh.DurationSec, sh.PeriodSec, dotWeight
					onSpellLand(sh.School, k, t, dotWeight)
					totalCost := sh.ManaCost * tries
					remDur := sh.ExecuteSec*tries - sh.CastSec
					mana -= totalCost
					if mana < 0 {
						mana = 0
					}
					if remDur > 1e-9 {
						mana = math.Min(st.ManaMax, mana+p.regenCast*remDur)
						lastCast = t + remDur
						advance(remDur, false)
					} else {
						lastCast = t
					}
				}
				acted = true
				break
			}
			if acted {
				break
			}
		}
		if acted {
			continue
		}
		for cd := range cooldowns {
			sh := &cooldowns[cd].shape
			if t >= cdReady[cd]-1e-9 && mana >= sh.ManaCost {
				if sh.CastSec > 0 && t+sh.CastSec > fight.Seconds+1e-9 {
					continue
				}
				note("cooldown", cooldowns[cd].act.Spell)
				cdReady[cd] = t + cooldowns[cd].cycle
				if sh.CastSec > 0 {
					mana = math.Min(st.ManaMax, mana+p.regenCast*sh.CastSec)
					advance(sh.CastSec, false)
				}
				mana -= sh.ManaCost
				if mana < 0 {
					mana = 0
				}
				lastCast = t
				landDirect(cooldowns[cd].act.Spell, sh, 0, t)
				if rem := sh.ExecuteSec - sh.CastSec; rem > 1e-9 {
					mana = math.Min(p.resourceMax, mana+p.regenCast*rem)
					lastCast = t + rem
					advance(rem, false)
				}
				acted = true
				break
			}
		}
		if acted {
			continue
		}
		if p.burn != nil && mana >= p.burn.shape.ManaCost {
			if ticks := burnTicks(); ticks > 0 {
				channelBurn(ticks)
				continue
			}
		}
		// Ahead of need a damage dealer converts when the health bar is full,
		// so that no leech is wasted on it. With a spell standing by that
		// returns health there is nothing to wait for: the lower the health,
		// the more of what that spell returns fits.
		early := p.netHealth || health >= p.fullAt || (p.recover != nil && p.leecher == p.recover)
		// The mana the rest of the fight needs is an estimate (the steady
		// rate of the rotation), so a damage dealer converts ahead only what
		// is certainly missing; the last conversion waits until the mana is
		// gone.
		ahead := canTap && early && mana+conv.mana <= st.ManaMax
		if ahead {
			need := p.drainPS * (fight.Seconds - t)
			if !p.netHealth {
				need -= conv.mana
			}
			ahead = mana < need
		}
		if ahead {
			convert()
			continue
		}
		if filler != nil && mana >= filler.shape.ManaCost {
			instant := consumeInstant(filler.act.Spell.BaseID, t)
			cast(filler, instant)
			continue
		}
		// Out of mana for the filler: convert if possible, else note
		// it and take the wand. A rotation without a filler converts only
		// ahead of need (above): its mana goes to the DoTs.
		if canTap && filler != nil {
			convert()
			continue
		}
		if p.nonMana {
			// Rage/Energy class waiting for resource regeneration while auto-attacking.
			step := 0.5
			mana = math.Min(p.resourceMax, mana+p.regenIdle*step)
			advance(step, false)
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
