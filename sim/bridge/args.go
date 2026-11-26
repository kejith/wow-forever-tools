package bridge

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/tools/upgrades"
)

type int64ListFlag []int64

func (f *int64ListFlag) String() string {
	parts := make([]string, len(*f))
	for i, v := range *f {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(parts, ",")
}

func (f *int64ListFlag) Set(s string) error {
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil || v <= 0 {
			return fmt.Errorf("invalid item ID %q", p)
		}
		*f = append(*f, v)
	}
	return nil
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }

func (f *stringListFlag) Set(s string) error {
	for _, part := range strings.Split(s, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p != "" {
			*f = append(*f, p)
		}
	}
	return nil
}

// Args are the options of app_bridge.py, with its defaults.
type Args struct {
	Wow               string
	Char              string
	Top               int
	TopSet            bool
	OwnedAtAHPrice    bool
	MaxPrice          float64
	Preview           int
	LevelMode         string // bis: "current" or "60"
	Sources           string // bis: comma-separated source categories
	ExcludeItems      []int64
	ExcludeCategories []string
	Profile           string
	Sort              string
	Engine            string
	FightSeconds      float64
	TargetDelta       int
	KillsPerMinute    float64
	Metric            string
	Method            string
	Iterations        int
	Consumables       bool
	Set               string  // consumables: which set a plan holds, "best" or "value"
	Budget            float64 // consumables --set value: gold per hour of fighting, 0 = no limit
	Profession        string
	IDs               string
	Write             bool
	File              string
	Lang              string
}

// ItemFilter returns the configured item/category filter, or nil when no exclusions are active.
func (a *Args) ItemFilter() *upgrades.Filter {
	if len(a.ExcludeItems) == 0 && len(a.ExcludeCategories) == 0 {
		return nil
	}
	return upgrades.NewFilter(a.ExcludeItems, a.ExcludeCategories)
}

// choices are the values argparse accepts for an option.
var choices = map[string][]string{
	"profile": {"", "dungeon", "leveling"},
	"sort":    {"gain", "price"},
	"engine":  {"auto", "on", "off"},
	"metric":  {"", "dps", "hps", "ttoom"},
	"method":  {"", "expectation", "montecarlo"},
	"lang":       {"auto", "en", "de"},
	"set":        {"best", "value"},
	"level-mode": {"current", "60"},
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
	fs.StringVar(&a.LevelMode, "level-mode", "current", "")
	fs.StringVar(&a.Sources, "sources", "", "")
	fs.Var((*int64ListFlag)(&a.ExcludeItems), "exclude-item", "")
	fs.Var((*stringListFlag)(&a.ExcludeCategories), "exclude-category", "")
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
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "top" {
			a.TopSet = true
		}
	})
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unrecognized arguments: %s", strings.Join(fs.Args(), " "))
	}
	values := map[string]string{"profile": a.Profile, "sort": a.Sort, "engine": a.Engine, "metric": a.Metric,
		"method": a.Method, "lang": a.Lang, "set": a.Set, "level-mode": a.LevelMode}
	for name, v := range values {
		ok := false
		for _, c := range choices[name] {
			ok = ok || c == v
		}
		if !ok {
			return nil, fmt.Errorf("argument --%s: invalid choice: %q", name, v)
		}
	}
	for _, cat := range a.ExcludeCategories {
		if !upgrades.IsKnownCategory(cat) {
			return nil, fmt.Errorf("argument --exclude-category: invalid choice: %q", cat)
		}
	}
	return a, nil
}
