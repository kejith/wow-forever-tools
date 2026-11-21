// wowtools is the one binary the Python scripts move into (notes/go-migration.md).
//
//	wowtools bridge --list            the bridge commands ported so far, as a JSON array
//	wowtools bridge <command> [args]  one JSON object, like scripts/app_bridge.py
//	wowtools --version
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kejith/wow-forever-tools/sim/bridge"
	"github.com/kejith/wow-forever-tools/sim/run"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: wowtools bridge --list | wowtools bridge <command> [args] | wowtools --version")
		os.Exit(2)
	}
	switch args[0] {
	case "--version", "-v":
		fmt.Println(run.Version)
	case "bridge":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: wowtools bridge --list | wowtools bridge <command> [args]")
			os.Exit(2)
		}
		if args[1] == "--list" {
			emit(bridge.Commands())
			return
		}
		emit(bridge.Run(args[1], args[2:]))
	default:
		fmt.Fprintln(os.Stderr, "unknown command: "+args[0])
		os.Exit(2)
	}
}

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
