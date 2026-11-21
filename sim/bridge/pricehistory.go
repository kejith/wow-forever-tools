package bridge

import (
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/pricehistory"
)

// pricehistory is app_bridge's pricehistory: every item of Auctionator's database with its days; the window
// searches and draws them.
func init() {
	register("pricehistory", func(a *Args) (*pyjson.Obj, error) { return pricehistory.Compute(a.Wow, nil) })
}
