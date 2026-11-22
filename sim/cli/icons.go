package cli

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/tools/icons"
)

// icons is scripts/icons.py run by itself: `wowtools icons 9492 287004` maps item IDs to icon FileDataIDs,
// downloads the missing PNGs into the cache and prints {"icons", "fallback", "dir"}. Like the script it takes
// only item IDs (the game folder is the default one, WOW_FOREVER_DIR) and writes the JSON in ASCII.
func init() {
	Register(Command{Name: "icons", Summary: "item IDs -> icon FileDataIDs, PNGs into the cache (JSON)",
		Run: func(args []string, out io.Writer) error {
			ids := make([]int64, 0, len(args))
			for _, a := range args {
				n, err := strconv.ParseInt(strings.TrimSpace(a), 10, 64)
				if err != nil {
					return fmt.Errorf("ValueError: invalid literal for int() with base 10: '%s'", a)
				}
				ids = append(ids, n)
			}
			r, err := icons.Resolve(game.DefaultWow(), ids)
			if err != nil {
				return err
			}
			data, err := pyjson.Encode(r)
			if err != nil {
				return err
			}
			_, err = out.Write(append(asciiJSON(data), '\n'))
			return err
		}})
}

// asciiJSON is json.dumps' default ensure_ascii=True: every character outside ASCII as \uXXXX (a pair of
// surrogates above U+FFFF). Outside ASCII is only ever inside a string.
func asciiJSON(data []byte) []byte {
	var b bytes.Buffer
	for _, r := range string(data) {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r > 0xffff:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.Bytes()
}
