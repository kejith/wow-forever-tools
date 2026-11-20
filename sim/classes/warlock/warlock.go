// Package warlock is the first class module: the damage rotation of a
// leveling/dungeon Warlock. The rotation is a priority list the engine
// turns into a steady-state cycle: keep the DoTs up, fill with Shadow Bolt,
// convert health to mana with Life Tap, wand when nothing is affordable.
package warlock

import (
	"github.com/kejith/wow-forever-tools/sim/catalog"
	"github.com/kejith/wow-forever-tools/sim/engine"
)

// First ranks of the rotation spells (stable Classic ids, present in the
// Forever tables).
const (
	ShadowBolt   = 686
	Corruption   = 172
	CurseOfAgony = 980
	SiphonLife   = 18265 // talent-granted
	LifeTap      = 1454
)

// DefaultMetric is what a Warlock optimizes for.
const DefaultMetric = "dps"

// Rotation builds the priority list from what the character actually knows.
func Rotation(cat *catalog.Catalog, known map[int]bool, level int) []engine.Action {
	var actions []engine.Action
	add := func(kind string, base int) {
		if sp := cat.BestRank(base, known, level); sp != nil {
			actions = append(actions, engine.Action{Kind: kind, Spell: sp})
		}
	}
	add("dot", CurseOfAgony)
	add("dot", Corruption)
	add("dot", SiphonLife)
	add("filler", ShadowBolt)
	actions = append(actions, engine.Action{Kind: "wand"})
	return actions
}
