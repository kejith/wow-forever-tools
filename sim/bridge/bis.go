package bridge

import (
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

func init() { register("bis", bisCmd) }

// bisCmd computes the top N best-in-slot items per equipment slot filtered by item source categories
// (quest, dungeon, raid, crafted, ...) and level mode ("current" or "60").
func bisCmd(a *Args) (*pyjson.Obj, error) {
	top := a.Top
	if top <= 0 || !a.TopSet {
		top = 10
	}
	o := upgrades.BiSOptions{
		LevelMode:      a.LevelMode,
		Preview:        int64(a.Preview),
		Top:            top,
		Sources:        upgrades.ParseSources(a.Sources),
		Profile:        a.Profile,
		Engine:         a.Engine,
		FightSeconds:   a.FightSeconds,
		TargetDelta:    a.TargetDelta,
		KillsPerMinute: a.KillsPerMinute,
		Metric:         a.Metric,
		Method:         a.Method,
		Iterations:     a.Iterations,
	}
	if a.Consumables {
		o.FightBuffs = fightBuffs(a)
	}
	return upgrades.ComputeBiS(a.Wow, a.Char, nil, o)
}
