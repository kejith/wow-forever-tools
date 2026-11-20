// simforever is the combat engine of WoW Forever Tools: it reads one JSON
// input bundle on stdin and answers with one JSON result on stdout. Errors
// are part of the payload with exit code 0, like app_bridge.py; only a
// broken invocation exits non-zero.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kejith/wow-forever-tools/sim/run"
)

func main() {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" {
			fmt.Println(run.Version)
			return
		}
	}
	var in run.Input
	dec := json.NewDecoder(os.Stdin)
	if err := dec.Decode(&in); err != nil {
		emit(run.Output{Engine: run.Version, Error: "bad input: " + err.Error()})
		return
	}
	emit(run.Run(in))
}

func emit(out run.Output) {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
