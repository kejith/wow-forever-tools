package bridge

import (
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/combatlog"
	"github.com/kejith/wow-forever-tools/sim/tools/damage"
	"github.com/kejith/wow-forever-tools/sim/tools/healing"
	"github.com/kejith/wow-forever-tools/sim/tools/rotations"
)

// rotationsCmd is app_bridge's rotations: the rotation guide. With --consumables it needs the consumables
// of a fight, which only the Python side has so far (phase 3 of the migration).
func rotationsCmd(a *Args) (*pyjson.Obj, error) {
	if a.Consumables {
		return nil, notPorted("rotations --consumables")
	}
	return rotations.Compute(a.Wow, a.Char, a.FightSeconds, a.TargetDelta, a.KillsPerMinute)
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
