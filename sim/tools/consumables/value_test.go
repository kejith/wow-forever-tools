package consumables

import (
	"math"
	"sort"
	"testing"

	"github.com/kejith/wow-forever-tools/sim/tools/enginebundle"
)

// A made-up engine: a point of spell damage is a point of the metric, mana per 5 seconds half a point up to
// 20 (more mana than the fight uses adds nothing).
func stubBatch(evals []enginebundle.Eval, _ bool) (map[string]float64, error) {
	out := map[string]float64{}
	for _, e := range evals {
		out[e.Key] = e.Delta["spDamage"] + math.Min(e.Delta["mp5"], 20)*0.5
	}
	return out, nil
}

// valueRow is a fight row as fightCandidates makes it: a price in copper (0 = none), the number in the bags,
// how long its buff lasts and what it adds.
func valueRow(id int64, name, kind string, price, owned, lasts int64, delta map[string]float64) *Row {
	r := &Row{ID: id, Name: name, Kind: kind, Slots: []string{kind}, Owned: owned, fight: true,
		item: &Item{Duration: &lasts}, Delta: newDelta()}
	if price != 0 {
		r.Price = &price
	}
	r.Available = owned != 0 || price != 0
	keys := make([]string, 0, len(delta))
	for k := range delta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r.Delta.set(k, delta[k])
	}
	return r
}

func sp(n float64) map[string]float64 { return map[string]float64{"spDamage": n} }

// The rows of the tests: two oils, a flask, a fish in the bags and a stew, a potion.
func valueWorld() (rows []*Row, byName map[string]*Row) {
	rows = []*Row{
		valueRow(1, "Small Oil", "weapon", 400, 0, 1800, sp(8)),
		valueRow(2, "Big Oil", "weapon", 2600, 0, 1800, sp(24)),
		valueRow(3, "Flask", "flask", 180000, 0, 7200, sp(150)),
		valueRow(4, "Fish", "food", 80, 40, 900, sp(22)),
		valueRow(5, "Stew", "food", 787, 0, 900, sp(28)),
		valueRow(6, "Potion", "potion", 12000, 0, 30, sp(15)),
	}
	byName = map[string]*Row{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	return rows, byName
}

func names(rows []*Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCostPerHour(t *testing.T) {
	for _, c := range []struct {
		name    string
		row     *Row
		seconds float64
		want    float64
		ok      bool
	}{
		{"a flask of two hours: half an item", valueRow(1, "", "flask", 180000, 0, 7200, nil), 60, 90000, true},
		{"an elixir of half an hour: two items", valueRow(1, "", "elixir", 1500, 0, 1800, nil), 60, 3000, true},
		{"a potion in every fight of a minute", valueRow(1, "", "potion", 100, 0, 30, nil), 60, 6000, true},
		{"two potions in a fight of three minutes", valueRow(1, "", "potion", 100, 0, 30, nil), 180, 4000, true},
		{"a rune like a potion", valueRow(1, "", "rune", 100, 0, 0, nil), 120, 3000, true},
		{"a buff shorter than the fight: one per fight", valueRow(1, "", "other", 100, 0, 30, nil), 60, 6000, true},
		{"in the bags: nothing", valueRow(1, "", "flask", 180000, 2, 7200, nil), 60, 0, true},
		{"no price", valueRow(1, "", "flask", 0, 0, 7200, nil), 60, 0, false},
	} {
		got, ok := costPerHour(c.row, c.seconds)
		if ok != c.ok || math.Abs(got-c.want) > 1e-6 {
			t.Errorf("%s: %v %v, want %v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// Without a budget the set ends at the strongest one, built in the order of its value: what is in the bags,
// the cheap oil, then each stronger item when the gold it costs more buys the most there.
func TestChooseValueBuyingOrder(t *testing.T) {
	rows, by := valueWorld()
	chosen, total, gained, steps, err := chooseValue(stubBatch, rows, false, 60, 0)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, s := range steps {
		order = append(order, s.row.Name)
	}
	if want := []string{"Fish", "Small Oil", "Big Oil", "Stew", "Flask", "Potion"}; !same(order, want) {
		t.Fatalf("steps %v, want %v", order, want)
	}
	if want := []string{"Big Oil", "Stew", "Flask", "Potion"}; !same(names(chosen), want) {
		t.Errorf("set %v, want %v", names(chosen), want)
	}
	if gained != 217 || total.vals["spDamage"] != 217 {
		t.Errorf("the set adds %v (delta %v), want 217", gained, total.vals)
	}
	// Big Oil comes in for Small Oil: 16 more for the 4400 copper an hour it costs more.
	swap := steps[2]
	if len(swap.out) != 1 || swap.out[0] != by["Small Oil"] || swap.more != 16 || swap.cost != 4400 {
		t.Errorf("the oil step: out %v, +%v for %v", names(swap.out), swap.more, swap.cost)
	}
	if steps[0].cost != 0 || steps[0].more != 22 {
		t.Errorf("the fish in the bags: +%v for %v", steps[0].more, steps[0].cost)
	}
	last := steps[len(steps)-1]
	if last.gained != 217 || last.spent != 5200+3148+90000+720000 {
		t.Errorf("after the last step: +%v for %v", last.gained, last.spent)
	}
	for i := 2; i < len(steps); i++ { // the paid steps come in falling value
		if a, b := steps[i-1], steps[i]; a.more/a.cost < b.more/b.cost {
			t.Errorf("step %d is worth more than step %d", i+1, i)
		}
	}
	if by["Big Oil"].Chosen != 1 || by["Small Oil"].Chosen != 0 || by["Small Oil"].BlockedBy != "Big Oil" ||
		by["Fish"].BlockedBy != "Stew" {
		t.Errorf("what the replaced rows say: %+v %+v", by["Small Oil"], by["Fish"])
	}
	if g := by["Flask"].PerGold; g == nil || *g != 16.67 {
		t.Errorf("the flask alone per gold: %v", g)
	}
	if g := by["Stew"].AddsPerGold; g == nil || *g != 19.06 {
		t.Errorf("the stew for the fish per gold: %v", g)
	}
	if by["Fish"].PerGold != nil || *by["Fish"].PerHour != 0 {
		t.Errorf("the fish in the bags costs nothing: %v %v", by["Fish"].PerGold, *by["Fish"].PerHour)
	}
}

// A budget ends the set where the next step does not fit; what would still add something says so.
func TestChooseValueBudget(t *testing.T) {
	rows, by := valueWorld()
	chosen, _, gained, steps, err := chooseValue(stubBatch, rows, false, 60, 1) // a gold an hour
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Big Oil", "Stew"}; !same(names(chosen), want) || gained != 52 {
		t.Errorf("set %v adds %v, want %v and 52", names(chosen), gained, want)
	}
	if spent := steps[len(steps)-1].spent; spent != 8348 {
		t.Errorf("an hour costs %v, want 8348", spent)
	}
	if !by["Flask"].OverBudget || !by["Potion"].OverBudget || by["Stew"].OverBudget || by["Small Oil"].OverBudget {
		t.Errorf("over the budget: flask %v, potion %v, stew %v, small oil %v", by["Flask"].OverBudget,
			by["Potion"].OverBudget, by["Stew"].OverBudget, by["Small Oil"].OverBudget)
	}
	// With less than the big oil costs the small one stays.
	rows, by = valueWorld()
	chosen, _, _, _, err = chooseValue(stubBatch, rows, false, 60, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Fish", "Small Oil"}; !same(names(chosen), want) || !by["Big Oil"].OverBudget {
		t.Errorf("set %v, want %v", names(chosen), want)
	}
}

// Gains do not add up: once the mana lasts, a second source of mana is not worth its gold.
func TestChooseValueCountsWhatIsThere(t *testing.T) {
	rows := []*Row{
		valueRow(1, "Mana Oil", "weapon", 500, 0, 1800, map[string]float64{"mp5": 20}),
		valueRow(2, "Mana Food", "food", 100, 0, 1800, map[string]float64{"mp5": 16}),
	}
	chosen, _, gained, _, err := chooseValue(stubBatch, rows, false, 60, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The food first (8 for 200 copper an hour), then the oil for the 2 that are left.
	if want := []string{"Mana Food", "Mana Oil"}; !same(names(chosen), want) || gained != 10 {
		t.Errorf("set %v adds %v", names(chosen), gained)
	}
	if rows[0].Gain != 10 || rows[0].Adds != 2 {
		t.Errorf("the oil: alone %v, in the set %v", rows[0].Gain, rows[0].Adds)
	}
}

// What has no price is never part of the set, and neither is what a buff on the character keeps out or what
// costs too much health.
func TestChooseValueLeavesOut(t *testing.T) {
	rows, by := valueWorld()
	craft := valueRow(7, "Own Elixir", "elixir", 0, 0, 3600, sp(40))
	craft.Available, craft.Craft = true, &Craft{Learned: true}
	rune := valueRow(8, "Rune", "rune", 100, 0, 0, sp(30))
	rune.TooCostly = true
	fed := valueRow(9, "Meal", "food", 50, 0, 900, nil)
	fed.OnYou, fed.onYouAt, fed.Active = "Well Fed", 1, true
	rows = append(rows, craft, rune, fed)
	chosen, _, _, _, err := chooseValue(stubBatch, rows, false, 60, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Big Oil", "Flask", "Potion"}; !same(names(chosen), want) {
		t.Errorf("set %v, want %v", names(chosen), want)
	}
	if !craft.NoPrice || craft.PerHour != nil {
		t.Errorf("the own elixir has no price: %+v", craft)
	}
	if by["Stew"].OnYou != "Well Fed" || by["Fish"].OnYou != "Well Fed" {
		t.Errorf("the food says what is on the character: %q %q", by["Stew"].OnYou, by["Fish"].OnYou)
	}
}

// For a healer the health an item costs is taken off what it adds.
func TestChooseValueHealthCost(t *testing.T) {
	cost := 6.0
	rune := valueRow(1, "Rune", "rune", 100, 0, 0, sp(10))
	rune.HealthPerSec = &cost
	rows := []*Row{rune, valueRow(2, "Oil", "weapon", 400, 0, 1800, sp(8))}
	chosen, _, gained, _, err := chooseValue(stubBatch, rows, true, 60, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Oil", "Rune"}; !same(names(chosen), want) || gained != 12 || rune.Gain != 4 {
		t.Errorf("set %v adds %v, the rune alone %v", names(chosen), gained, rune.Gain)
	}
}

func TestByValue(t *testing.T) {
	rows, _ := valueWorld()
	if _, _, _, _, err := chooseValue(stubBatch, rows, false, 60, 0); err != nil {
		t.Fatal(err)
	}
	none := valueRow(7, "Nowhere", "food", 0, 0, 900, sp(99))
	rows = append(rows, none)
	byValue(rows)
	// In the bags, then by gain per gold (100, 88.94, 46.15, 16.67, 0.21), last what has no price.
	if want := []string{"Fish", "Small Oil", "Stew", "Big Oil", "Flask", "Potion", "Nowhere"}; !same(names(rows), want) {
		t.Errorf("order %v, want %v", names(rows), want)
	}
}

// The strongest set is what it was: the same rows through the shared helpers.
func TestChooseStrongest(t *testing.T) {
	rows, by := valueWorld()
	chosen, _, gained, err := choose(stubBatch, rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Flask", "Stew", "Big Oil", "Potion"}; !same(names(chosen), want) || gained != 217 {
		t.Errorf("set %v adds %v", names(chosen), gained)
	}
	if by["Fish"].BlockedBy != "Stew" || by["Flask"].PerHour != nil {
		t.Errorf("fish %q, flask per hour %v", by["Fish"].BlockedBy, by["Flask"].PerHour)
	}
}
