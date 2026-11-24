package bridge

import (
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/ingame"
)

// ingame is app_bridge's ingame: what the addon shows in the game, and with --write the file it loads.
func init() {
	register("ingame", func(a *Args) (*pyjson.Obj, error) { return ingame.Sync(a.Wow, a.Char, a.Write) })
}
