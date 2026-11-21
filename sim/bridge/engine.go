package bridge

import (
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/combatlog"
	"github.com/kejith/wow-forever-tools/sim/tools/damage"
	"github.com/kejith/wow-forever-tools/sim/tools/healing"
	"github.com/kejith/wow-forever-tools/sim/tools/rotations"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
)

// rotationsCmd is app_bridge's rotations: the rotation guide. With --consumables every fight is played with
// the best set of consumables for the guide's metric.
func rotationsCmd(a *Args) (*pyjson.Obj, error) {
	var buffs upgrades.FightBuffsFunc
	if a.Consumables {
		buffs = fightBuffs(a)
	}
	return rotations.Compute(a.Wow, a.Char, a.FightSeconds, a.TargetDelta, a.KillsPerMinute, buffs)
}

// healingCmd is app_bridge's healing: every heal and shield rank with its numbers, which rank to use when,
// the regeneration and the stat weights.
func healingCmd(a *Args) (*pyjson.Obj, error) {
	return healing.Compute(a.Wow, a.Char, a.FightSeconds, a.TargetDelta)
}

// damageCmd is app_bridge's damage: every known damage spell with its numbers, the rotations to use and the
// stat weights.
func damageCmd(a *Args) (*pyjson.Obj, error) {
	return damage.Compute(a.Wow, a.Char, a.FightSeconds, a.TargetDelta, a.KillsPerMinute)
}

// combatlogCmd is app_bridge's combatlog: what the character really did in the newest combat log (or
// --file), next to the combat model's numbers.
func combatlogCmd(a *Args) (*pyjson.Obj, error) {
	return combatlog.Compute(a.Wow, a.Char, a.File, combatlog.Gap, true)
}
