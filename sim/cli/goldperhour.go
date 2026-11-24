package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/tools/goldperhour"
)

// goldperhour is scripts/goldperhour.py run by itself: the GoldPerHour sessions of the account as indented
// JSON. The script had only --wow (its texts are numbers and names from the game, nothing to translate).
func init() {
	Register(Command{Name: "goldperhour", Summary: "the GoldPerHour sessions of the account, as JSON",
		Run: func(args []string, out io.Writer) error {
			f := flag.NewFlagSet("wowtools goldperhour", flag.ContinueOnError)
			f.SetOutput(os.Stderr)
			wow := f.String("wow", game.DefaultWow(), "the _forever_ folder of the game")
			if err := f.Parse(args); err != nil {
				return ErrUsage
			}
			if f.NArg() > 0 {
				fmt.Fprintf(os.Stderr, "wowtools goldperhour: unexpected argument %q\n", f.Arg(0))
				return ErrUsage
			}
			r, err := goldperhour.Compute(*wow)
			if err != nil {
				return err
			}
			data, err := pyjson.Encode(r)
			if err != nil {
				return err
			}
			_, err = out.Write(append(indentJSON(data, 2), '\n'))
			return err
		}})
}

// indentJSON turns pyjson's compact text (", " and ": " as separators) into what json.dumps(indent=n) writes:
// one entry per line, "," at the end of the line, empty lists and objects as [] and {}.
func indentJSON(data []byte, n int) []byte {
	var b bytes.Buffer
	level := 0
	newline := func() {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(" ", level*n))
	}
	inString, escaped := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			b.WriteByte(c)
		case '{', '[':
			if i+1 < len(data) && (data[i+1] == '}' || data[i+1] == ']') {
				b.WriteByte(c)
				b.WriteByte(data[i+1])
				i++
				continue
			}
			b.WriteByte(c)
			level++
			newline()
		case '}', ']':
			level--
			newline()
			b.WriteByte(c)
		case ',':
			b.WriteByte(c)
			newline()
			if i+1 < len(data) && data[i+1] == ' ' {
				i++
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.Bytes()
}
