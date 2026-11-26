package bridge

import (
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/enchants"
)

func init() { register("enchants", enchantsCmd) }

func enchantsCmd(a *Args) (*pyjson.Obj, error) {
	return enchants.Compute(a.Wow, a.Char, enchants.Options{
		Top:            a.Top,
		Engine:         a.Engine,
		FightSeconds:   a.FightSeconds,
		TargetDelta:    a.TargetDelta,
		KillsPerMinute: a.KillsPerMinute,
		Metric:         a.Metric,
		Profile:        a.Profile,
	})
}
