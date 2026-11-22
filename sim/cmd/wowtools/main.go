// wowtools is the one binary of WoW Forever Tools (notes/go-migration.md).
//
//	wowtools bridge --list            the bridge commands, as a JSON array
//	wowtools bridge <command> [args]  one JSON object for the desktop app (scripts/app_bridge.py before)
//	wowtools <command> [args]         a report as text: writs, upgrades, recipes, … (the scripts' CLIs before)
//	wowtools --version
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/bridge"
	"github.com/kejith/wow-forever-tools/sim/cli"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/run"
)

func usage() string {
	var b strings.Builder
	b.WriteString("usage: wowtools <command> [options]   (wowtools <command> -h for its options)\n\ncommands:\n")
	for _, name := range cli.Names() {
		c, _ := cli.Lookup(name)
		fmt.Fprintf(&b, "  %-13s %s\n", name, c.Summary)
	}
	b.WriteString("\n  bridge        JSON for the desktop app: wowtools bridge --list | wowtools bridge <command> [args]\n")
	b.WriteString("  --version     the version of the combat engine\n")
	return b.String()
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage())
		os.Exit(2)
	}
	switch args[0] {
	case "--version", "-v":
		fmt.Println(run.Version)
	case "--help", "-h", "help":
		fmt.Print(usage())
	case "bridge":
		if len(args) < 2 {
			fmt.Fprint(os.Stderr, usage())
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
		c, ok := cli.Lookup(args[0])
		if !ok {
			fmt.Fprintln(os.Stderr, "unknown command: "+args[0])
			fmt.Fprint(os.Stderr, usage())
			os.Exit(2)
		}
		out := bufio.NewWriter(os.Stdout)
		code := cli.Main(c, args[1:], out)
		out.Flush()
		os.Exit(code)
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
