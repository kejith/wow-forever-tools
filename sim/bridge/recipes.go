package bridge

import (
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/recipes"
)

// recipes is app_bridge.py's "recipes": the professions with their learned and missing recipes, all of
// them (no top), filtered by --profession.
func init() {
	register("recipes", func(a *Args) (*pyjson.Obj, error) {
		return recipes.Compute(a.Wow, a.Char, a.Profession, 0, nil)
	})
}
