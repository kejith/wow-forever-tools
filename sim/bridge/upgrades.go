package bridge

import (
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

func init() { register("upgrades", upgradesCmd) }

// upgradesCmd is app_bridge's default command: upgrade candidates per slot and the best package for the
// budget. With --consumables every swap is rated on top of the best set of consumables.
func upgradesCmd(a *Args) (*pyjson.Obj, error) {
	o := upgrades.Options{Preview: int64(a.Preview), MaxPrice: a.MaxPrice,
		Top: a.Top, Profile: a.Profile, Sort: a.Sort, Engine: a.Engine, FightSeconds: a.FightSeconds,
		TargetDelta: a.TargetDelta, KillsPerMinute: a.KillsPerMinute, Metric: a.Metric, Method: a.Method,
		Iterations: a.Iterations}
	if a.Consumables {
		o.FightBuffs = fightBuffs(a)
	}
	return upgrades.Compute(a.Wow, a.Char, nil, o)
}
