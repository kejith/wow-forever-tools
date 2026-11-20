package engine

import (
	"math"
	"sort"
)

// Healing on demand. A healer does not cast at full speed all the time: the
// damage coming in says how much healing is needed, and the question is
// which spells supply it for the least mana. PlanDemand answers it for one
// level of demand: every heal is a source with healing and mana per second
// of casting, a HoT or a shield can only be kept on so many targets, and
// the plan is the mix that meets the demand in the time there is at the
// lowest mana per second. Time not spent casting regenerates mana: all of
// it outside the five-second rule counts at the full rate.

// HealSource is one spell a demand plan can use.
type HealSource struct {
	Action Action
	// Targets is on how many targets a HoT or a shield can be kept: one cast
	// per target and cycle (the effect's duration, or Action.CycleSec). 0 is
	// a heal that is cast as often as time allows.
	Targets int
	// Group ties the ranks of one such spell together: a target carries one
	// of them at a time, so they share the targets. 0 is a group of its own.
	Group int
}

// DemandLine is one spell of a demand plan.
type DemandLine struct {
	Action   Action
	PerCast  float64 // healing of one cast (crit expectation included)
	PerMin   float64 // casts per minute
	HPS      float64
	ManaPS   float64
	SharePct float64 // share of the time spent casting it
	// Targets is on how many targets a HoT or a shield is kept (a fraction:
	// on one of them part of the time); 0 for a heal.
	Targets float64
}

// DemandPlan is the cheapest way to heal a given amount per second.
type DemandPlan struct {
	Demand  float64 // healing per second asked for
	HPS     float64 // healing per second supplied (below Demand: more is not possible)
	ManaPS  float64 // mana per second the spells cost
	RegenPS float64 // mana per second regenerated at this pace
	// OOMSec is how long the mana lasts; nil when the regeneration covers
	// the cost.
	OOMSec  *float64
	CastPct float64 // share of the time spent casting
	FreePct float64 // share of the time outside the five-second rule
	Lines   []DemandLine
	// Conversions (Dark Sacrifice): how often per minute, how many until the
	// mana is gone (0 when they keep it up for good), and what one returns
	// and costs. ConversionSpell is nil when there is none or it does not pay.
	Conversions       float64
	ConversionsPerMin float64
	ConversionMana    float64
	ConversionHealth  float64
	ConversionSpell   *Spell
	Candidates        int
}

// option is a source with its numbers per second of casting.
type option struct {
	src     HealSource
	perCast float64
	exec    float64
	hps     float64 // healing per second of casting
	mps     float64 // mana per second of casting
	cost    float64 // what the plan weighs a second of casting at: mps plus the regeneration its casts hold back
	cap     float64 // most of the time it can take (1 = all)
	perAll  float64 // time it takes to keep it on all its targets (0 for a heal)
	group   int     // options of one group share their targets (0 = none)
}

// room tracks how much of each group's targets is taken.
type room map[int]float64

// take gives option o up to want of the time and returns what it got.
func (r room) take(o option, want float64) float64 {
	x := math.Min(o.cap, want)
	shared := o.group != 0 && o.perAll > 0
	if shared {
		x = math.Min(x, o.perAll*(1-r[o.group]))
	}
	if x <= 0 {
		return 0
	}
	if shared {
		r[o.group] += x / o.perAll
	}
	return x
}

func healOptionsOf(st *State, sources []HealSource) []option {
	var out []option
	for _, s := range sources {
		sp := s.Action.Spell
		if sp == nil {
			continue
		}
		sh := Shape(sp, st, Target{})
		o := option{src: s, exec: sh.ExecuteSec, cap: 1, group: s.Group}
		o.perCast = sh.SampleDirect(nil) + sh.Ticks*sh.TickValue
		if !sh.IsHeal || o.perCast <= 0 || o.exec <= 0 {
			continue
		}
		o.hps, o.mps = o.perCast/o.exec, sh.ManaCost/o.exec
		o.cost = o.mps
		if s.Targets > 0 {
			cycle := math.Max(math.Max(sh.DurationSec, sh.CooldownSec), s.Action.CycleSec)
			if cycle <= 0 {
				continue
			}
			o.perAll = float64(s.Targets) * o.exec / cycle
			o.cap = math.Min(1, o.perAll)
		}
		out = append(out, o)
	}
	return out
}

// fill meets the demand with the options in the order of their price per
// point of healing, where a second of casting costs lambda mana on top: the
// higher lambda, the more the plan prefers fast heals over cheap ones.
// It returns the time share of every option, their sum and what is supplied.
func fill(opts []option, demand, lambda float64) (x []float64, time, hps float64) {
	order := make([]int, len(opts))
	for i := range order {
		order[i] = i
	}
	price := func(i int) float64 { return (opts[i].cost + lambda) / opts[i].hps }
	if math.IsInf(lambda, 1) {
		price = func(i int) float64 { return 1 / opts[i].hps }
	}
	sort.SliceStable(order, func(a, b int) bool { return price(order[a]) < price(order[b]) })
	x = make([]float64, len(opts))
	taken := room{}
	for _, i := range order {
		left := demand - hps
		if left <= 1e-12 {
			break
		}
		x[i] = taken.take(opts[i], left/opts[i].hps)
		time += x[i]
		hps += x[i] * opts[i].hps
	}
	return x, time, hps
}

// MaxHealing is the most healing per second the sources give with all the
// time spent casting, whatever the mana.
func MaxHealing(st *State, sources []HealSource) float64 {
	opts := healOptionsOf(st, sources)
	order := append([]option(nil), opts...)
	sort.SliceStable(order, func(a, b int) bool { return order[a].hps > order[b].hps })
	time, hps := 0.0, 0.0
	taken := room{}
	for _, o := range order {
		if time >= 1 {
			break
		}
		x := taken.take(o, 1-time)
		time += x
		hps += x * o.hps
	}
	return hps
}

// cheapest is the mix of the options that supplies the demand within the
// time at the lowest mana per second. With two limits (the demand and the
// time) the cheapest mix lies between two fillings by price: the last one
// that is too slow and the first one fast enough.
func cheapest(opts []option, demand float64) []float64 {
	// A filling is good when it fits the time and supplies the demand (ranks
	// that share a target can leave it short: the cheap one takes the place
	// of the strong one).
	good := func(time, hps float64) bool { return time <= 1+1e-9 && hps >= demand*(1-1e-9) }
	x, time, hps := fill(opts, demand, 0)
	if good(time, hps) {
		return x
	}
	fast, fastTime, fastHPS := fill(opts, demand, math.Inf(1))
	if fastTime >= 1 || !good(fastTime, fastHPS) { // more than can be healed: everything at full speed
		order := make([]int, len(opts))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool { return opts[order[a]].hps > opts[order[b]].hps })
		out := make([]float64, len(opts))
		left := 1.0
		taken := room{}
		for _, i := range order {
			out[i] = taken.take(opts[i], left)
			left -= out[i]
		}
		return out
	}
	lo, hi := 0.0, 1.0
	slow, slowTime, slowHPS := x, time, hps
	for ; hi < 1e9; hi *= 2 {
		if f, t, h := fill(opts, demand, hi); good(t, h) {
			fast, fastTime = f, t
			break
		}
	}
	for i := 0; i < 60; i++ {
		mid := (lo + hi) / 2
		if f, t, h := fill(opts, demand, mid); good(t, h) {
			hi, fast, fastTime = mid, f, t
		} else {
			lo, slow, slowTime, slowHPS = mid, f, t, h
		}
	}
	w := 0.0 // share of the slow, cheap filling that still fits
	if slowTime > fastTime && slowHPS >= demand*(1-1e-9) {
		w = math.Min(1, (1-fastTime)/(slowTime-fastTime))
	}
	out := make([]float64, len(opts))
	for i := range out {
		out[i] = w*slow[i] + (1-w)*fast[i]
	}
	return out
}

// planOf turns time shares into a plan with its mana balance.
func planOf(st *State, fight Fight, opts []option, x []float64, demand float64) DemandPlan {
	p := DemandPlan{Demand: demand}
	castsPS := 0.0
	for i, o := range opts {
		if x[i] <= 1e-9 {
			continue
		}
		l := DemandLine{Action: o.src.Action, PerCast: o.perCast, PerMin: x[i] / o.exec * 60, HPS: x[i] * o.hps,
			ManaPS: x[i] * o.mps, SharePct: x[i] * 100}
		if o.src.Targets > 0 {
			l.Targets = float64(o.src.Targets) * x[i] / o.perAll
		}
		p.Lines = append(p.Lines, l)
		p.HPS += l.HPS
		p.ManaPS += l.ManaPS
		p.CastPct += l.SharePct
		castsPS += x[i] / o.exec
	}
	// Casts spread evenly: every cast is followed by five seconds of the
	// lower regeneration, what is left of the gap regenerates in full.
	free := math.Max(0, 1-fiveSecondRule*castsPS)
	casting, idle := st.Regen(fight)
	p.FreePct = free * 100
	p.RegenPS = casting*(1-free) + idle*free
	if net := p.ManaPS - p.RegenPS; net > 1e-9 {
		oom := st.ManaMax / net
		p.OOMSec = &oom
	}
	return p
}

const fiveSecondRule = 5.0

// LastsSec is from when on a plan's mana counts as lasting: no fight is
// that long, and "lasts 7 hours" says nothing a player can use.
const LastsSec = 3600.0

// better says whether plan a lasts longer than plan b; among plans that
// last forever the one with more mana to spare wins.
func (a DemandPlan) better(b DemandPlan) bool {
	if math.Abs(a.HPS-b.HPS) > 1e-6*math.Max(1, b.HPS) {
		return a.HPS > b.HPS
	}
	switch {
	case a.OOMSec == nil && b.OOMSec == nil:
		return a.ManaPS-a.RegenPS < b.ManaPS-b.RegenPS-1e-9
	case a.OOMSec == nil:
		return true
	case b.OOMSec == nil:
		return false
	}
	return *a.OOMSec > *b.OOMSec+1e-9
}

// PlanDemand finds the plan that supplies the demand (healing per second)
// for the longest time. Besides the cheapest mix of all sources it tries
// every single heal next to the HoTs and shields, each once with the mana
// alone as the price and once with the regeneration a cast holds back on
// top (fewer, bigger casts leave more time outside the five-second rule).
// convert is a spell that trades health for mana (Dark Sacrifice), or nil:
// it is used when the mana does not last and the mana it returns heals more
// than the health it costs. Mana that lasts longer than LastsSec counts as
// lasting.
func PlanDemand(st *State, fight Fight, sources []HealSource, demand float64, convert *Action) DemandPlan {
	opts := healOptionsOf(st, sources)
	if len(opts) == 0 || demand <= 0 {
		return DemandPlan{Demand: demand}
	}
	// Every cast holds the full regeneration back for five seconds. While
	// the casts leave gaps longer than that, a cast therefore costs that
	// much on top of its mana; once they do not, it costs its mana alone.
	// Which of the two holds depends on the plan, so both are tried.
	casting, idle := st.Regen(fight)
	taxed := append([]option(nil), opts...)
	for i := range taxed {
		taxed[i].cost += fiveSecondRule * (idle - casting) / taxed[i].exec
	}
	var best DemandPlan
	candidates := 0
	for _, set := range [][]option{opts, taxed} {
		try := func(subset []option) {
			p := planOf(st, fight, subset, cheapest(subset, demand), demand)
			if candidates++; candidates == 1 || p.better(best) {
				best = p
			}
		}
		try(set)
		for i, o := range set {
			if o.src.Targets > 0 {
				continue
			}
			var subset []option
			for j, other := range set {
				if other.src.Targets > 0 || j == i {
					subset = append(subset, other)
				}
			}
			try(subset)
		}
	}
	best.Candidates = candidates

	if convert != nil && best.OOMSec != nil && best.HPS > 0 {
		if c := converterOf(st, fight, []Action{*convert}); c != nil {
			// The health it costs has to be healed too, at the plan's price.
			gain := c.mana - c.health*best.ManaPS/best.HPS
			if gain > 0 {
				net := best.ManaPS - best.RegenPS
				best.ConversionMana, best.ConversionHealth, best.ConversionSpell = c.mana, c.health, c.spell
				if gain/c.cooldown >= net {
					// Cast now and then it closes the gap for good.
					best.OOMSec = nil
					best.ConversionsPerMin = net / gain * 60
				} else {
					uses, oom := 1.0, 0.0
					for i := 0; i < 50; i++ {
						oom = (st.ManaMax + uses*gain) / net
						next := 1 + math.Floor(oom/c.cooldown)
						if next <= uses {
							break
						}
						uses = next
					}
					best.OOMSec = &oom
					best.Conversions, best.ConversionsPerMin = uses, uses/oom*60
				}
			}
		}
	}
	if best.OOMSec != nil && *best.OOMSec > LastsSec {
		best.OOMSec, best.Conversions = nil, 0
	}
	return best
}
