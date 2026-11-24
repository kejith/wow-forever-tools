package bridge

import (
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/writs"
)

// writs is app_bridge's writs: the best craftsman's writs per tier and the trade crates.
func init() {
	register("writs", func(a *Args) (*pyjson.Obj, error) {
		return writs.Compute(a.Wow, a.Char, a.Top, a.OwnedAtAHPrice, nil)
	})
}
