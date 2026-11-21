package bridge

import (
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
)

func init() { register("upgrades", upgradesCmd) }

// upgradesCmd is app_bridge's default command: upgrade candidates per slot and the best package for the
// budget.
func upgradesCmd(a *Args) (*pyjson.Obj, error) {
	if a.Consumables {
		return nil, notPorted("upgrades --consumables")
	}
	return upgrades.Compute(a.Wow, a.Char, nil, upgrades.Options{Preview: int64(a.Preview), MaxPrice: a.MaxPrice,
		Top: a.Top, Profile: a.Profile, Sort: a.Sort, Engine: a.Engine, FightSeconds: a.FightSeconds,
		TargetDelta: a.TargetDelta, KillsPerMinute: a.KillsPerMinute, Metric: a.Metric, Method: a.Method,
		Iterations: a.Iterations})
}
