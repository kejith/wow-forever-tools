package bridge

import (
	"strings"
	"unicode"

	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/icons"
)

// icons is app_bridge's icons: item ID -> icon FileDataID for --ids, with the PNGs downloaded into the cache.
func init() {
	register("icons", func(a *Args) (*pyjson.Obj, error) {
		var ids []int64
		for _, part := range strings.Split(a.IDs, ",") {
			p := strings.TrimFunc(part, unicode.IsSpace)
			if p == "" || strings.IndexFunc(p, func(r rune) bool { return !unicode.IsDigit(r) }) >= 0 {
				continue // str.isdigit()
			}
			if n, ok := pyfmt.ParseInt(part); ok {
				ids = append(ids, n)
			}
		}
		return icons.Resolve(a.Wow, ids)
	})
}
