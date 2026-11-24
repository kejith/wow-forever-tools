package bridge

import (
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/goldperhour"
)

// goldperhour is app_bridge's goldperhour: the GoldPerHour sessions of the account (ignores --char).
func init() {
	register("goldperhour", func(a *Args) (*pyjson.Obj, error) { return goldperhour.Compute(a.Wow) })
}
