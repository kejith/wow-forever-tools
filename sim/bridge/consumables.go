package bridge

import (
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/consumables"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
)

// consumables is app_bridge.py's "consumables": every buff group (the window hides the ones that do not help
// the class) and, with the combat engine, the consumables for a fight rated by what they add ("plans"). With
// --set value the set of a plan is the one by value: the most gain per gold an hour of fighting costs, up to
// --budget gold an hour.
func init() {
	register("consumables", func(a *Args) (*pyjson.Obj, error) {
		set := consumables.Set{Value: a.Set == "value", Budget: a.Budget}
		return consumables.Compute(a.Wow, a.Char, a.Top, true, a.Engine, a.FightSeconds, a.TargetDelta, nil, set)
	})
}

// fightBuffs is consumables.fight_buffs for the character of a call, as upgrades and rotations take it with
// --consumables. An error Python does not raise as a LookupError is marked upgrades.Fatal, so that upgrades
// returns it whatever --engine says.
func fightBuffs(a *Args) upgrades.FightBuffsFunc {
	return func(metric string, seconds float64, targetDelta int) (*upgrades.Consumed, error) {
		c, err := consumables.FightBuffs(a.Wow, a.Char, metric, seconds, targetDelta)
		if err != nil && !consumables.IsLookup(err) {
			return nil, &upgrades.Fatal{Err: err}
		}
		return c, err
	}
}
