// Package cli holds the text commands of wowtools: `wowtools writs --char Malgruk` prints what
// `uv run scripts/writs.py --char Malgruk` printed. Each command registers itself from its own file, so that
// commands written side by side do not edit one list.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/kejith/wow-tools-forever/sim/internal/game"
	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
)

// Command is one text command.
type Command struct {
	Name    string
	Summary string // one line for `wowtools --help`
	// Run parses args (without the command name) and writes the report to out. An error is printed to
	// stderr with exit code 1, as the scripts' sys.exit(message) did; ErrUsage means the arguments were
	// wrong (exit code 2, the flag package has printed why).
	Run func(args []string, out io.Writer) error
}

// ErrUsage: the arguments could not be parsed.
var ErrUsage = errors.New("usage")

var commands = map[string]Command{}

// Register adds a command.
func Register(c Command) { commands[c.Name] = c }

// Lookup is the command of that name.
func Lookup(name string) (Command, bool) { c, ok := commands[name]; return c, ok }

// Names are the commands, sorted.
func Names() []string {
	out := make([]string, 0, len(commands))
	for n := range commands {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Flags is a flag set with the options every script had: --wow (the _forever_ folder) and --lang
// (auto|en|de).
type Flags struct {
	*flag.FlagSet
	Wow  string
	Lang string
}

// NewFlags starts the flags of a command.
func NewFlags(name string) *Flags {
	f := &Flags{FlagSet: flag.NewFlagSet("wowtools "+name, flag.ContinueOnError)}
	f.StringVar(&f.Wow, "wow", game.DefaultWow(), "the _forever_ folder of the game")
	f.StringVar(&f.Lang, "lang", "auto", "language of the output: auto (the game client's), en or de")
	return f
}

// Parse parses the arguments and selects the language, as i18n.set_language(a.lang, a.wow) did.
func (f *Flags) Parse(args []string) error {
	f.SetOutput(os.Stderr)
	if err := f.FlagSet.Parse(args); err != nil {
		return ErrUsage
	}
	if f.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "wowtools %s: unexpected argument %q\n", f.Name(), f.Arg(0))
		return ErrUsage
	}
	switch f.Lang {
	case "auto", "en", "de":
	default:
		fmt.Fprintf(os.Stderr, "wowtools %s: --lang must be auto, en or de\n", f.Name())
		return ErrUsage
	}
	i18n.SetLanguage(f.Lang, f.Wow)
	return nil
}

// JSON prints a value as json.dumps(value, ensure_ascii=False) did, with a newline.
func JSON(out io.Writer, v any) error {
	data, err := pyjson.Encode(v)
	if err != nil {
		return err
	}
	_, err = out.Write(append(data, '\n'))
	return err
}

// Main runs a command and returns the exit code.
func Main(c Command, args []string, out io.Writer) int {
	err := c.Run(args, out)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrUsage) || errors.Is(err, flag.ErrHelp):
		return 2
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}
