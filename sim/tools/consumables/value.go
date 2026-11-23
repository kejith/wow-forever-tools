package consumables

import (
	"math"
	"sort"
	"strconv"

	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/tools/enginebundle"
)

// ---------- the set by value: the most gain per gold ----------

// Set says how the set for a fight is built: the strongest one (the zero value), or by value.
type Set struct {
	Value  bool    // by what an item adds per gold instead of by what it adds
	Budget float64 // with Value: at most this much gold per hour of fighting (0 = no limit)
}

const (
	hour          = 3600.0
	copperPerGold = 10000.0
	maxValueSteps = 200 // every step raises the metric by eps or more, so this is never reached
)

// costPerHour is what an hour of fighting with the consumable costs, in copper: its price times the items
// an hour takes. A buff takes one item per duration (a flask of two hours half an item), a potion or a rune
// the ones a fight uses in every fight. What is in the bags costs nothing. false = no price is known.
func costPerHour(r *Row, seconds float64) (float64, bool) {
	if r.Owned != 0 {
		return 0, true
	}
	if r.price() == 0 {
		return 0, false
	}
	seconds = math.Max(1.0, seconds)
	items := float64(usesIn(seconds)) * hour / seconds
	if r.Kind != "potion" && r.Kind != "rune" {
		lasts := seconds // a buff shorter than the fight: one item per fight
		if r.item != nil {
			lasts = math.Max(seconds, float64(r.item.duration()))
		}
		items = hour / lasts
	}
	return float64(r.price()) * items, true
}

// perGold is a gain per gold an hour of fighting costs.
func perGold(gain, copper float64) *float64 {
	if copper <= 0 {
		return nil
	}
	v := pyfmt.Round(gain/(copper/copperPerGold), 2)
	return &v
}

// step is one way to go on from a set: put row in, which takes out the rows of the set that share a slot
// with it. more is what the metric gains, cost what an hour costs more (copper; less than 0 when the row is
// cheaper than what it replaces). A step that was taken also has what the set adds and costs after it.
type step struct {
	row       *Row
	keep, out []*Row
	more      float64
	cost      float64
	gained    float64
	spent     float64
}

// before: a is the better step. What costs nothing comes first, the bigger gain first; then the most gain
// per gold. Of steps that are worth the same, the item in the bags wins, then the cheaper one.
func (a *step) before(b *step) bool {
	af, bf := a.cost <= 0, b.cost <= 0
	if af != bf {
		return af
	}
	if af {
		if am, bm := pyfmt.Round(a.more, 1), pyfmt.Round(b.more, 1); am != bm {
			return am > bm
		}
	} else if ar, br := a.more/a.cost, b.more/b.cost; ar != br {
		return ar > br
	}
	if ao, bo := a.row.Owned != 0, b.row.Owned != 0; ao != bo {
		return ao
	}
	if ap, bp := a.row.price(), b.row.price(); ap != bp {
		return ap < bp
	}
	return a.row.ID < b.row.ID
}

func shareSlot(a, b *Row) bool {
	for _, s := range a.Slots {
		for _, t := range b.Slots {
			if s == t {
				return true
			}
		}
	}
	return false
}

func sumDelta(rows []*Row) *Delta {
	total := newDelta()
	for _, r := range rows {
		total = addDelta(total, r.Delta)
	}
	return total
}

// chooseValue is the set by value: the consumables that add the most per gold an hour of fighting costs
// (costPerHour), up to budget gold an hour (0 = no limit).
//
// The set grows one step at a time. A step puts one item in; if the set holds an item in its place, that one
// goes out, and the step is worth what the metric gains for what the hour costs more. So what is in the bags
// comes first, then the cheap items, and a stronger item of the same kind replaces a weaker one later, when
// the gold it costs more buys more there than anywhere else. A step must add eps or more and fit the budget;
// when none is left the set is done. With no budget it ends at (about) the strongest set.
//
// Returns the rows of the set in the order they came in, their summed delta, what they add together, and the
// steps: the buying order by value.
func chooseValue(batch batchFunc, rows []*Row, healer bool, seconds, budget float64) ([]*Row, *Delta, float64, []*step, error) {
	health := healthCost(healer)
	held, err := rateAlone(batch, rows, health)
	if err != nil {
		return nil, nil, 0, nil, err
	}
	var pool []*Row
	for _, r := range rows {
		c, ok := costPerHour(r, seconds)
		if !ok {
			r.NoPrice = r.Available // from an own recipe: nothing says what it costs
			continue
		}
		copper := pyfmt.RoundInt(c)
		r.PerHour = &copper
		if r.Delta.Len() == 0 || r.Active {
			continue
		}
		if r.Gain > 0 {
			r.PerGold = perGold(r.Gain, float64(copper))
		}
		if r.Available && !r.TooCostly && !held.any(r.Slots) {
			pool = append(pool, r)
		}
	}
	key := func(r *Row) string { return strconv.FormatInt(r.ID, 10) }
	paid := func(rows []*Row) float64 { // the health a set costs, per second
		sum := 0.0
		for _, r := range rows {
			sum += health(r)
		}
		return sum
	}

	limit := budget * copperPerGold
	var set []*Row
	var taken []*step
	gained, spent := 0.0, 0.0 // what the set adds, and what an hour with it costs
	for n := 0; n < maxValueSteps; n++ {
		in := map[*Row]bool{}
		for _, r := range set {
			in[r] = true
		}
		var steps []*step
		var evals []enginebundle.Eval
		for _, r := range pool {
			r.OverBudget = false
			if in[r] {
				continue
			}
			s := &step{row: r, cost: float64(*r.PerHour)}
			for _, x := range set {
				if shareSlot(x, r) {
					s.out = append(s.out, x)
					s.cost -= float64(*x.PerHour)
				} else {
					s.keep = append(s.keep, x)
				}
			}
			s.keep = append(s.keep, r)
			steps = append(steps, s)
			evals = append(evals, enginebundle.Eval{Key: key(r), Delta: sumDelta(s.keep).Map()})
		}
		if len(steps) == 0 {
			break
		}
		gains, err := batch(evals, true)
		if err != nil {
			return nil, nil, 0, nil, err
		}
		var best *step
		for _, s := range steps {
			s.more = gains[key(s.row)] - paid(s.keep) - gained
			if s.more < eps {
				continue
			}
			if limit > 0 && spent+s.cost > limit {
				s.row.OverBudget = true
				continue
			}
			if best == nil || s.before(best) {
				best = s
			}
		}
		if best == nil {
			break
		}
		set, gained, spent = best.keep, gained+best.more, spent+best.cost
		best.gained, best.spent = gained, spent
		best.row.Adds, best.row.AddsPerGold = pyfmt.Round(best.more, 2), perGold(best.more, best.cost)
		taken = append(taken, best)
	}

	used := held.copy()
	for i, r := range set {
		r.Chosen = i + 1
		for _, s := range r.Slots {
			used.set(s, r.Name)
		}
	}
	markTaken(rows, held, used)
	return set, sumDelta(set), gained, taken, nil
}

// byValue sorts the rows of a kind for the set by value: what is in the bags first, then the most gain per
// gold, last what has no price.
func byValue(rows []*Row) {
	rank := func(r *Row) int {
		switch {
		case r.PerHour == nil:
			return 2
		case *r.PerHour == 0:
			return 0
		}
		return 1
	}
	worth := func(r *Row) float64 {
		if r.PerGold != nil {
			return *r.PerGold
		}
		return r.Gain
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if wa, wb := worth(a), worth(b); wa != wb {
			return wa > wb
		}
		if a.Gain != b.Gain {
			return a.Gain > b.Gain
		}
		return a.ID < b.ID
	})
}
