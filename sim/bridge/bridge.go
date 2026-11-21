// Package bridge is the Go side of scripts/app_bridge.py: one command in, one JSON object out, errors as
// {"error": "…"} with exit code 0. During the migration (notes/go-migration.md) it grows one command at a
// time; a command it does not have yet is still answered by the Python bridge, and the parity check
// (cmd/parity) compares the two wherever both answer.
package bridge

import "sort"

// handlers maps a bridge command to its Go implementation. Empty until phase 1 of the migration.
var handlers = map[string]func(args []string) map[string]any{}

// Commands lists the bridge commands the Go side answers, sorted.
func Commands() []string {
	out := make([]string, 0, len(handlers))
	for name := range handlers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Run answers one bridge command. args are the arguments after the command, as app_bridge.py takes them.
func Run(command string, args []string) map[string]any {
	h, ok := handlers[command]
	if !ok {
		return map[string]any{"error": "not ported to Go yet: " + command}
	}
	return h(args)
}
