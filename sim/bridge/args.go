package bridge

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
)

// Args are the options of app_bridge.py, with its defaults.
type Args struct {
	Wow            string
	Char           string
	Top            int
	OwnedAtAHPrice bool
	MaxPrice       float64
	Preview        int
	Profile        string
	Sort           string
	Engine         string
	FightSeconds   float64
	TargetDelta    int
	KillsPerMinute float64
	Metric         string
	Method         string
	Iterations     int
	Consumables    bool
	Set            string  // consumables: which set a plan holds, "best" or "value"
	Budget         float64 // consumables --set value: gold per hour of fighting, 0 = no limit
	Profession     string
	IDs            string
	Write          bool
	File           string
	Lang           string
}

// choices are the values argparse accepts for an option.
var choices = map[string][]string{
	"profile": {"", "dungeon", "leveling"},
	"sort":    {"gain", "price"},
	"engine":  {"auto", "on", "off"},
	"metric":  {"", "dps", "hps", "ttoom"},
	"method":  {"", "expectation", "montecarlo"},
	"lang":    {"auto", "en", "de"},
	"set":     {"best", "value"},
}

// ParseArgs reads the options after the command. An unknown option or value is an error, as argparse
// stops with one (app_bridge.py then answers no JSON at all).
func ParseArgs(argv []string) (*Args, error) {
	a := &Args{}
	fs := flag.NewFlagSet("bridge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&a.Wow, "wow", game.DefaultWow(), "")
	fs.StringVar(&a.Char, "char", "", "")
	fs.IntVar(&a.Top, "top", 3, "")
	fs.BoolVar(&a.OwnedAtAHPrice, "owned-at-ah-price", false, "")
	fs.Float64Var(&a.MaxPrice, "max-price", 0, "")
	fs.IntVar(&a.Preview, "preview", 4, "")
	fs.StringVar(&a.Profile, "profile", "", "")
	fs.StringVar(&a.Sort, "sort", "gain", "")
	fs.StringVar(&a.Engine, "engine", "auto", "")
	fs.Float64Var(&a.FightSeconds, "fight-seconds", 60, "")
	fs.IntVar(&a.TargetDelta, "target-delta", -1, "")
	fs.Float64Var(&a.KillsPerMinute, "kills-per-minute", -1, "")
	fs.StringVar(&a.Metric, "metric", "", "")
	fs.StringVar(&a.Method, "method", "", "")
	fs.IntVar(&a.Iterations, "iterations", 0, "")
	fs.BoolVar(&a.Consumables, "consumables", false, "")
	fs.StringVar(&a.Set, "set", "best", "")
	fs.Float64Var(&a.Budget, "budget", 0, "")
	fs.StringVar(&a.Profession, "profession", "", "")
	fs.StringVar(&a.IDs, "ids", "", "")
	fs.BoolVar(&a.Write, "write", false, "")
	fs.StringVar(&a.File, "file", "", "")
	fs.StringVar(&a.Lang, "lang", "auto", "")
	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unrecognized arguments: %s", strings.Join(fs.Args(), " "))
	}
	values := map[string]string{"profile": a.Profile, "sort": a.Sort, "engine": a.Engine, "metric": a.Metric,
		"method": a.Method, "lang": a.Lang, "set": a.Set}
	for name, v := range values {
		ok := false
		for _, c := range choices[name] {
			ok = ok || c == v
		}
		if !ok {
			return nil, fmt.Errorf("argument --%s: invalid choice: %q", name, v)
		}
	}
	return a, nil
}
