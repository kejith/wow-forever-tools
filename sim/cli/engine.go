package cli

import (
	"encoding/json"
	"io"
	"os"

	"github.com/kejith/wow-tools-forever/sim/run"
)

// engine is what simforever was: one JSON input bundle on stdin, one JSON result on stdout. Errors are part
// of the payload; only a broken invocation exits non-zero. For looking at the combat engine by hand.
func init() {
	Register(Command{Name: "engine", Summary: "the combat engine: a JSON bundle on stdin, the result on stdout",
		Run: func(args []string, out io.Writer) error {
			f := NewFlags("engine")
			if err := f.Parse(args); err != nil {
				return err
			}
			var in run.Input
			result := run.Output{Engine: run.Version}
			if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
				result.Error = "bad input: " + err.Error()
			} else {
				result = run.Run(in)
			}
			return json.NewEncoder(out).Encode(result)
		}})
}
