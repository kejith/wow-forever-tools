// wowtools is the one binary the Python scripts move into (notes/go-migration.md).
//
//	wowtools bridge --list            the bridge commands ported so far, as a JSON array
//	wowtools bridge <command> [args]  one JSON object, like scripts/app_bridge.py
//	wowtools --version
package main

import (
	"fmt"
	"os"

	"github.com/kejith/wow-forever-tools/sim/bridge"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/run"
)

const usage = "usage: wowtools bridge --list | wowtools bridge <command> [args] | wowtools --version"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch args[0] {
	case "--version", "-v":
		fmt.Println(run.Version)
	case "bridge":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, usage)
			os.Exit(2)
		}
		if args[1] == "--list" {
			list := []any{}
			for _, c := range bridge.Commands() {
				list = append(list, c)
			}
			emit(list)
			return
		}
		r, err := bridge.Run(args[1], args[2:])
		if err != nil { // wrong arguments: argparse stops with a message and exit code 2
			fmt.Fprintln(os.Stderr, "wowtools bridge "+args[1]+": "+err.Error())
			os.Exit(2)
		}
		emit(r)
	default:
		fmt.Fprintln(os.Stderr, "unknown command: "+args[0])
		os.Exit(2)
	}
}

func emit(v any) {
	data, err := pyjson.Encode(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(append(data, '\n'))
}
